package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestFindByLoginEmail — поиск по адресу входа находит живого пользователя,
// чужой и несуществующий адрес дают mongo.ErrNoDocuments
func TestFindByLoginEmail(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db,
		api.User{ID: 1_000_000_000_001, DisplayName: "Email User", LoginEmail: "user@example.com", PasswordHash: "hash"},
		api.User{ID: 111, DisplayName: "Telegram User", TelegramID: intPtr(111)},
	)

	got, err := repo.FindByLoginEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("пользователь не найден по login_email: %v", err)
	}
	if got.ID != 1_000_000_000_001 || got.PasswordHash != "hash" {
		t.Fatalf("найден не тот пользователь: %+v", got)
	}

	if _, err = repo.FindByLoginEmail(ctx, "nobody@example.com"); err != mongo.ErrNoDocuments {
		t.Fatalf("ожидался mongo.ErrNoDocuments, получено: %v", err)
	}
	// пустой адрес не должен матчить документы, где поля просто нет
	if _, err = repo.FindByLoginEmail(ctx, ""); err != mongo.ErrNoDocuments {
		t.Fatalf("пустой адрес нашёл пользователя: %v", err)
	}
}

// TestFindByLoginEmailNormalizes — регистр и пробелы не создают второго
// аккаунта: A@B.com и a@b.com это один и тот же адрес
func TestFindByLoginEmailNormalizes(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.CreateIdentityUser(ctx, api.User{
		ID: 1_000_000_000_002, LoginEmail: NormalizeLoginEmail("  A@B.com "), PasswordHash: "hash",
	}); err != nil {
		t.Fatalf("создание упало: %v", err)
	}

	var doc bson.M
	if err := db.Collection("user").FindOne(ctx, bson.M{"_id": 1_000_000_000_002}).Decode(&doc); err != nil {
		t.Fatalf("документ не прочитан: %v", err)
	}
	if doc["login_email"] != "a@b.com" {
		t.Fatalf("адрес записан ненормализованным: %v", doc["login_email"])
	}

	for _, variant := range []string{"a@b.com", "A@B.com", " A@b.COM "} {
		got, err := repo.FindByLoginEmail(ctx, variant)
		if err != nil {
			t.Fatalf("вариант %q не нашёл пользователя: %v", variant, err)
		}
		if got.ID != 1_000_000_000_002 {
			t.Fatalf("вариант %q нашёл не того: %d", variant, got.ID)
		}
	}
}

// TestFindByLoginEmailSkipsDeleted — tombstone по адресу не находится: иначе
// удалённый аккаунт блокировал бы повторную регистрацию с той же почтой
func TestFindByLoginEmailSkipsDeleted(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	deletedAt := time.Now().UTC()
	seedUsers(t, db, api.User{
		ID: 600, LoginEmail: "dead@example.com", PasswordHash: "hash", DeletedAt: &deletedAt,
	})

	if _, err := repo.FindByLoginEmail(ctx, "dead@example.com"); err != mongo.ErrNoDocuments {
		t.Fatalf("удалённый найден по login_email: %v", err)
	}
}

// TestLoginEmailIndexUniqueSparse — второй аккаунт с тем же адресом отвергается,
// а аккаунты вовсе без адреса сосуществуют (sparse)
func TestLoginEmailIndexUniqueSparse(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes упал: %v", err)
	}

	if err := repo.CreateIdentityUser(ctx, api.User{ID: 1, LoginEmail: "same@example.com", PasswordHash: "h1"}); err != nil {
		t.Fatalf("первый пользователь не создан: %v", err)
	}
	err := repo.CreateIdentityUser(ctx, api.User{ID: 2, LoginEmail: "same@example.com", PasswordHash: "h2"})
	if !IsDuplicateKey(err) {
		t.Fatalf("ожидался duplicate key по login_email, получено: %v", err)
	}

	// sparse: у обоих login_email отсутствует
	if err = repo.CreateIdentityUser(ctx, api.User{ID: 3, TelegramID: intPtr(3)}); err != nil {
		t.Fatalf("пользователь без login_email не создан: %v", err)
	}
	if err = repo.CreateIdentityUser(ctx, api.User{ID: 4, TelegramID: intPtr(4)}); err != nil {
		t.Fatalf("второй пользователь без login_email не создан (sparse не работает): %v", err)
	}
}

// TestSetPasswordHash — хеш пишется живому пользователю и не воскрешает
// tombstone: удалённому аккаунту способ входа возвращать нельзя
func TestSetPasswordHash(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	deletedAt := time.Now().UTC()
	seedUsers(t, db,
		api.User{ID: 700, DisplayName: "Live", LoginEmail: "live@example.com"},
		api.User{ID: 701, DisplayName: "Dead", DeletedAt: &deletedAt},
	)

	if err := repo.SetPasswordHash(ctx, 700, "new-hash"); err != nil {
		t.Fatalf("SetPasswordHash упал: %v", err)
	}
	got, err := repo.FindByLoginEmail(ctx, "live@example.com")
	if err != nil {
		t.Fatalf("пользователь не найден: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Fatalf("хеш не записан: %+v", got)
	}

	if err = repo.SetPasswordHash(ctx, 701, "resurrect"); err != mongo.ErrNoDocuments {
		t.Fatalf("запись в tombstone должна давать ErrNoDocuments, получено: %v", err)
	}
	dead, err := repo.FindById(ctx, 701)
	if err != nil {
		t.Fatalf("tombstone не прочитан: %v", err)
	}
	if dead.PasswordHash != "" {
		t.Fatalf("tombstone получил password_hash: %+v", dead)
	}

	if err = repo.SetPasswordHash(ctx, 999, "ghost"); err != mongo.ErrNoDocuments {
		t.Fatalf("запись несуществующему должна давать ErrNoDocuments, получено: %v", err)
	}
	if n, cErr := db.Collection("user").CountDocuments(ctx, bson.M{"_id": 999}); cErr != nil || n != 0 {
		t.Fatalf("SetPasswordHash создал документ (upsert): n=%d err=%v", n, cErr)
	}
}

// TestSoftDeleteFreesLoginEmail — удаление аккаунта убирает и адрес, и хеш,
// после чего та же почта регистрируется заново
func TestSoftDeleteFreesLoginEmail(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes упал: %v", err)
	}
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 800, LoginEmail: "reuse@example.com", PasswordHash: "hash"}); err != nil {
		t.Fatalf("создание упало: %v", err)
	}
	if err := repo.SoftDeleteUser(ctx, 800); err != nil {
		t.Fatalf("SoftDeleteUser упал: %v", err)
	}

	tombstone, err := repo.FindById(ctx, 800)
	if err != nil {
		t.Fatalf("tombstone не прочитан: %v", err)
	}
	if tombstone.LoginEmail != "" || tombstone.PasswordHash != "" {
		t.Fatalf("удаление не вычистило вход по паролю: %+v", tombstone)
	}

	// адрес свободен: unique sparse индекс отсутствующего поля не видит
	if err = repo.CreateIdentityUser(ctx, api.User{ID: 801, LoginEmail: "reuse@example.com", PasswordHash: "hash2"}); err != nil {
		t.Fatalf("повторная регистрация того же адреса отвергнута: %v", err)
	}
	got, err := repo.FindByLoginEmail(ctx, "reuse@example.com")
	if err != nil || got.ID != 801 {
		t.Fatalf("после повторной регистрации найден не тот пользователь: %+v, err=%v", got, err)
	}
}

// TestClearPasswordIdentityKeepsEmail — отвязка пароля убирает хеш, но оставляет
// адрес за аккаунтом: иначе «войти другим способом и задать новый пароль»
// вернуло бы пароль без почты, то есть неработающий вход
func TestClearPasswordIdentityKeepsEmail(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db, api.User{ID: 900, LoginEmail: "keep@example.com", PasswordHash: "hash", GoogleSub: "gsub"})

	if err := repo.ClearIdentity(ctx, 900, IdentityPassword); err != nil {
		t.Fatalf("ClearIdentity упал: %v", err)
	}
	got, err := repo.FindById(ctx, 900)
	if err != nil {
		t.Fatalf("пользователь не прочитан: %v", err)
	}
	if got.PasswordHash != "" {
		t.Fatalf("хеш не убран: %+v", got)
	}
	if got.LoginEmail != "keep@example.com" {
		t.Fatalf("адрес входа потерян: %+v", got)
	}
}
