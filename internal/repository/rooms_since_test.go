package repository

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// seedRoomCreatedAt вставляет комнату с заданной датой создания.
func seedRoomCreatedAt(t *testing.T, repo *MongoRoomRepository, name string, createdAt time.Time) string {
	t.Helper()
	room := api.Room{
		Name:     name,
		Members:  &[]api.User{{ID: 1, DisplayName: "Загир"}},
		CreateAt: createdAt,
		Currency: "RUB",
	}
	res, err := repo.col.InsertOne(context.Background(), room)
	if err != nil {
		t.Fatalf("не удалось засеять комнату: %v", err)
	}
	return res.InsertedID.(primitive.ObjectID).Hex()
}

// Джоб напоминаний берёт только свежие комнаты: в базе лежит гора старых
// ботовых, где долги давно закрыты мимо приложения.
func TestEachRoomCreatedAfterFiltersByDate(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	seedRoomCreatedAt(t, repo, "древняя", now.AddDate(0, 0, -200))
	seedRoomCreatedAt(t, repo, "свежая", now.AddDate(0, 0, -10))
	seedRoomCreatedAt(t, repo, "вчерашняя", now.AddDate(0, 0, -1))

	var seen []string
	err := repo.EachRoomCreatedAfter(ctx, now.AddDate(0, 0, -60), 10, func(rooms []api.Room) error {
		for _, r := range rooms {
			seen = append(seen, r.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("получили %v, ожидались только свежие", seen)
	}
	for _, name := range seen {
		if name == "древняя" {
			t.Error("старая комната попала в выборку — напоминание там было бы ложью")
		}
	}
}

// Порции не должны терять комнаты: хвост меньше размера батча — обычный случай.
func TestEachRoomCreatedAfterBatchesLoseNothing(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	const total = 7
	for i := 0; i < total; i++ {
		seedRoomCreatedAt(t, repo, "комната", now.AddDate(0, 0, -i-1))
	}

	var got, batches int
	err := repo.EachRoomCreatedAfter(ctx, now.AddDate(0, 0, -60), 3, func(rooms []api.Room) error {
		batches++
		got += len(rooms)
		return nil
	})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if got != total {
		t.Errorf("обошли %d комнат из %d", got, total)
	}
	// 7 комнат по 3 — три порции: две полные и хвост.
	if batches != 3 {
		t.Errorf("порций %d, ожидалось 3", batches)
	}
}

// Ошибка обработчика останавливает обход: джоб не должен молча продолжать
// рассылку после сбоя.
func TestEachRoomCreatedAfterStopsOnError(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		seedRoomCreatedAt(t, repo, "комната", now.AddDate(0, 0, -i-1))
	}

	calls := 0
	err := repo.EachRoomCreatedAfter(ctx, now.AddDate(0, 0, -60), 2, func([]api.Room) error {
		calls++
		return context.Canceled
	})
	if err == nil {
		t.Fatal("ошибка обработчика не вернулась наверх")
	}
	if calls != 1 {
		t.Errorf("обработчик вызван %d раз после ошибки", calls)
	}
}
