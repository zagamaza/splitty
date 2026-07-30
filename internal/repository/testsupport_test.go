package repository

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Инфраструктура интеграционных тестов репозитория.
//
// Тесты работают против ЖИВОГО mongo, а не мока: проверять надо ровно то, что
// мок не умеет — unique/sparse-индексы, атомарность $inc, поведение upsert.
// URI берётся из MONGO_TEST_URI (по умолчанию mongodb://localhost:27017).
// Если mongo недоступен — тест скипается, а не падает: `go test ./...` на
// машине без docker обязан оставаться зелёным.

const (
	testMongoURIEnv     = "MONGO_TEST_URI"
	testMongoURIDefault = "mongodb://localhost:27017"
	testMongoTimeout    = 3 * time.Second
)

// testDBCounter добавляется к имени базы: nanotime сам по себе не гарантирует
// уникальность, если два testDB вызваны в одну наносекунду (разрешение таймера
// на некоторых платформах грубее наносекунды).
var testDBCounter int64

// testDB подключается к тестовому mongo и возвращает свежую пустую базу с
// уникальным именем. База удаляется, а соединение закрывается в t.Cleanup,
// поэтому вызывающему убирать за собой не нужно.
//
// Если mongo недоступен — t.Skip с подсказкой, как его поднять.
func testDB(t *testing.T) *mongo.Database {
	t.Helper()

	uri := os.Getenv(testMongoURIEnv)
	if uri == "" {
		uri = testMongoURIDefault
	}

	ctx, cancel := context.WithTimeout(context.Background(), testMongoTimeout)
	defer cancel()

	client, err := mongo.NewClient(options.Client().ApplyURI(uri).SetServerSelectionTimeout(testMongoTimeout))
	if err != nil {
		t.Skipf("mongo недоступен (%s=%q): %v; задайте MONGO_TEST_URI или поднимите docker compose up -d mongo", testMongoURIEnv, uri, err)
	}
	if err = client.Connect(ctx); err != nil {
		t.Skipf("mongo недоступен (%s=%q): %v; задайте MONGO_TEST_URI или поднимите docker compose up -d mongo", testMongoURIEnv, uri, err)
	}
	if err = client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Skipf("mongo недоступен (%s=%q): %v; задайте MONGO_TEST_URI или поднимите docker compose up -d mongo", testMongoURIEnv, uri, err)
	}

	name := testDBName()
	db := client.Database(name)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), testMongoTimeout)
		defer cleanupCancel()
		if dropErr := db.Drop(cleanupCtx); dropErr != nil {
			t.Errorf("не удалось удалить тестовую базу %s: %v", name, dropErr)
		}
		if discErr := client.Disconnect(cleanupCtx); discErr != nil {
			t.Errorf("не удалось отключиться от mongo: %v", discErr)
		}
	})

	return db
}

// testDBName генерирует уникальное имя тестовой базы.
func testDBName() string {
	return fmt.Sprintf("splitty_test_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&testDBCounter, 1))
}

// seedUsers вставляет пользователей в коллекцию user напрямую, минуя
// репозиторий: подготовка состояния не должна зависеть от кода, который тест и
// проверяет (иначе сломанный UpsertUser «починит» тест на FindBy*).
func seedUsers(t *testing.T, db *mongo.Database, users ...api.User) {
	t.Helper()
	if len(users) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), testMongoTimeout)
	defer cancel()

	docs := make([]interface{}, 0, len(users))
	for _, u := range users {
		docs = append(docs, u)
	}
	if _, err := db.Collection("user").InsertMany(ctx, docs); err != nil {
		t.Fatalf("не удалось засеять пользователей: %v", err)
	}
}

// testCtx — контекст с таймаутом для запросов внутри теста.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestTestDBLifecycle — самопроверка инфраструктуры: база создаётся, документ
// пишется и читается, а после t.Cleanup базы больше не существует.
func TestTestDBLifecycle(t *testing.T) {
	var dbName string

	t.Run("write and read", func(t *testing.T) {
		db := testDB(t)
		dbName = db.Name()

		seedUsers(t, db,
			api.User{ID: 42, Username: "alice", DisplayName: "Alice"},
			api.User{ID: 43, Username: "bob", DisplayName: "Bob"},
		)

		ctx := testCtx(t)
		var got api.User
		if err := db.Collection("user").FindOne(ctx, bson.M{"_id": 42}).Decode(&got); err != nil {
			t.Fatalf("не удалось прочитать засеянного пользователя: %v", err)
		}
		if got.ID != 42 || got.Username != "alice" || got.DisplayName != "Alice" {
			t.Fatalf("прочитан не тот пользователь: %+v", got)
		}

		n, err := db.Collection("user").CountDocuments(ctx, bson.M{})
		if err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if n != 2 {
			t.Fatalf("ожидалось 2 документа, получено %d", n)
		}
	})

	if dbName == "" {
		t.Skip("вложенный тест скипнулся: mongo недоступен")
	}

	// Cleanup вложенного теста уже отработал — базы быть не должно.
	uri := os.Getenv(testMongoURIEnv)
	if uri == "" {
		uri = testMongoURIDefault
	}
	ctx, cancel := context.WithTimeout(context.Background(), testMongoTimeout)
	defer cancel()

	client, err := mongo.NewClient(options.Client().ApplyURI(uri).SetServerSelectionTimeout(testMongoTimeout))
	if err != nil {
		t.Fatalf("не удалось создать клиента для проверки: %v", err)
	}
	if err = client.Connect(ctx); err != nil {
		t.Fatalf("не удалось подключиться для проверки: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	names, err := client.ListDatabaseNames(ctx, bson.M{"name": dbName})
	if err != nil {
		t.Fatalf("не удалось получить список баз: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("тестовая база %s не удалена после Cleanup: %v", dbName, names)
	}
}
