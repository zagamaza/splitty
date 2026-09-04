package repository

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

// Дата создания ставится обеими вставками — и той, что заводит личность
// (telegram/google/apple/пароль/бот), и upsert'ом.
//
// «Там, где выдаётся номер» было бы неверным местом: NextUserID зовут только
// google/apple, регистрация по паролю и telegram с занятым _id. Обычная
// telegram-регистрация, бот и dev-вход прошли бы мимо и остались без даты —
// молча, потому что поле необязательное.
func TestCreatedAtSetOnIdentityInsert(t *testing.T) {
	repo := NewUserRepository(testDB(t))
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 9001, Username: "identity"}); err != nil {
		t.Fatal(err)
	}

	user, err := repo.FindById(ctx, 9001)
	if err != nil {
		t.Fatal(err)
	}
	if user.CreatedAt == nil {
		t.Fatal("дата создания не проставилась")
	}
	if user.CreatedAt.Before(before) {
		t.Errorf("дата создания %s раньше начала теста", user.CreatedAt)
	}
}

func TestCreatedAtSetOnUpsertInsert(t *testing.T) {
	repo := NewUserRepository(testDB(t))
	ctx := context.Background()

	if _, err := repo.UpsertUser(ctx, api.User{ID: 9002, Username: "upsert", DisplayName: "Апсерт"}); err != nil {
		t.Fatal(err)
	}
	user, err := repo.FindById(ctx, 9002)
	if err != nil {
		t.Fatal(err)
	}
	if user.CreatedAt == nil {
		t.Fatal("дата создания не проставилась при upsert-вставке")
	}
}

// Сохранение профиля дату НЕ переписывает.
//
// UpsertUser зовётся на каждой смене имени, и положи мы created_at в $set —
// «зарегистрировался» стало бы означать «последний раз правил профиль». Ошибка
// такого рода не видна ничем: когорты просто медленно поехали бы вперёд.
func TestCreatedAtSurvivesProfileSave(t *testing.T) {
	repo := NewUserRepository(testDB(t))
	ctx := context.Background()

	if _, err := repo.UpsertUser(ctx, api.User{ID: 9003, Username: "stable", DisplayName: "Было"}); err != nil {
		t.Fatal(err)
	}
	first, err := repo.FindById(ctx, 9003)
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt == nil {
		t.Fatal("дата не проставилась")
	}
	created := *first.CreatedAt

	time.Sleep(10 * time.Millisecond)
	if _, err := repo.UpsertUser(ctx, api.User{ID: 9003, Username: "stable", DisplayName: "Стало"}); err != nil {
		t.Fatal(err)
	}

	again, err := repo.FindById(ctx, 9003)
	if err != nil {
		t.Fatal(err)
	}
	if again.CreatedAt == nil || !again.CreatedAt.Equal(created) {
		t.Errorf("дата уехала: было %s, стало %v", created, again.CreatedAt)
	}
	if again.DisplayName != "Стало" {
		t.Errorf("имя не сохранилось: %q", again.DisplayName)
	}
}

// У аккаунтов, заведённых до этой правки, даты нет — и это отдельная когорта
// «до аналитики», а не 1 января 1970-го. Поле необязательное именно поэтому.
func TestCreatedAtAbsentForLegacyUsers(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	if _, err := db.Collection("user").InsertOne(ctx, map[string]any{"_id": 9004, "user_name": "legacy"}); err != nil {
		t.Fatal(err)
	}
	user, err := repo.FindById(ctx, 9004)
	if err != nil {
		t.Fatal(err)
	}
	if user.CreatedAt != nil {
		t.Errorf("у старого аккаунта появилась дата %s — когорта «до аналитики» размылась", user.CreatedAt)
	}
}
