package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

// Интеграционные тесты SetNotificationsSeenAt против ЖИВОГО mongo.
//
// Фейком их не заменить: вся суть метода — в двух нетривиальных условиях
// ФИЛЬТРА (tombstone и «только вперёд»), а фейк в rest-тестах повторяет то же
// правило кодом на Go. Инвертируй там $lt или выкини условие по deleted_at —
// и все тесты пакета rest остались бы зелёными.

const seenUserID = 9101

func seenRepo(t *testing.T, users ...api.User) *MongoUserRepository {
	t.Helper()
	db := testDB(t)
	seedUsers(t, db, users...)
	return NewUserRepository(db)
}

func seenAt(t *testing.T, repo *MongoUserRepository, id int) *time.Time {
	t.Helper()
	user, err := repo.FindById(testCtx(t), id)
	if err != nil {
		t.Fatalf("не удалось прочитать пользователя: %v", err)
	}
	return user.NotificationsSeenAt
}

func TestSetNotificationsSeenAtMovesForward(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)

	first := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, first); err != nil {
		t.Fatalf("первая отметка: %v", err)
	}
	if got := seenAt(t, repo, seenUserID); got == nil || !got.Equal(first) {
		t.Fatalf("отметка не записалась: %v, want %v", got, first)
	}

	second := first.Add(30 * time.Minute)
	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, second); err != nil {
		t.Fatalf("вторая отметка: %v", err)
	}
	if got := seenAt(t, repo, seenUserID); got == nil || !got.Equal(second) {
		t.Fatalf("отметка не сдвинулась вперёд: %v, want %v", got, second)
	}
}

// TestSetNotificationsSeenAtNeverGoesBackwards — запоздавший ретрай со старым
// временем не должен откатывать отметку: уже прочитанное всплыло бы снова.
func TestSetNotificationsSeenAtNeverGoesBackwards(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)

	current := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, current); err != nil {
		t.Fatalf("отметка: %v", err)
	}

	stale := current.Add(-time.Hour)
	// Ошибки быть не должно: для клиента это идемпотентный повтор, а не сбой.
	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, stale); err != nil {
		t.Fatalf("запоздавшая отметка вернула ошибку: %v", err)
	}
	if got := seenAt(t, repo, seenUserID); got == nil || !got.Equal(current) {
		t.Fatalf("отметку откатили назад: %v, want %v", got, current)
	}
}

// TestSetNotificationsSeenAtSkipsTombstone — на удалённый аккаунт метод не
// пишет ничего и, главное, НЕ создаёт документ: фильтр без upsert.
func TestSetNotificationsSeenAtSkipsTombstone(t *testing.T) {
	deletedAt := time.Now().UTC()
	repo := seenRepo(t, api.User{
		ID: seenUserID, Username: "zagir", DisplayName: DeletedUserPlaceholder,
		DeletedAt: &deletedAt,
	})
	ctx := testCtx(t)

	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, time.Now().UTC()); err != nil {
		t.Fatalf("отметка на tombstone вернула ошибку: %v", err)
	}
	if got := seenAt(t, repo, seenUserID); got != nil {
		t.Fatalf("отметка записана на tombstone: %v", got)
	}
}

// TestSetNotificationsSeenAtDoesNotCreateUser — несуществующий id не должен
// порождать документ: upsert здесь означал бы пользователя из ниоткуда.
func TestSetNotificationsSeenAtDoesNotCreateUser(t *testing.T) {
	repo := seenRepo(t)
	ctx := testCtx(t)

	if err := repo.SetNotificationsSeenAt(ctx, seenUserID, time.Now().UTC()); err != nil {
		t.Fatalf("отметка по несуществующему id вернула ошибку: %v", err)
	}
	if _, err := repo.FindById(ctx, seenUserID); err == nil {
		t.Fatal("отметка создала пользователя, которого не было")
	}
}
