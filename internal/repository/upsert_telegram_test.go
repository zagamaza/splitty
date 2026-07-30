package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestUpsertTelegramUserCreatesWithTelegramID — новый telegram-пользователь
// получает _id, равный telegram id (так заведены все исторические аккаунты), и
// telegram_id тем же значением
func TestUpsertTelegramUserCreatesWithTelegramID(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	u, err := repo.UpsertTelegramUser(ctx, 555, "zagir", "Загир", "ru")
	if err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if u.ID != 555 {
		t.Fatalf("_id = %d, want 555 (telegram id)", u.ID)
	}
	if u.TelegramID == nil || *u.TelegramID != 555 {
		t.Fatalf("telegram_id = %v, want 555", u.TelegramID)
	}
	if u.Username != "zagir" || u.DisplayName != "Загир" || u.UserLang != "ru" {
		t.Fatalf("профиль записан неверно: %+v", u)
	}
}

// TestUpsertTelegramUserOccupiedID — _id, равный telegram id, занят ДРУГИМ
// пользователем (например, google-аккаунтом или tombstone). Новый получает
// синтетический номер, но telegram_id остаётся telegram-овским: иначе резолв по
// личности при следующем апдейте не нашёл бы его
func TestUpsertTelegramUserOccupiedID(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	// чужой аккаунт занял номер 777 и никакого отношения к telegram не имеет
	seedUsers(t, db, api.User{ID: 777, Username: "stranger", DisplayName: "Чужой", GoogleSub: "sub-777"})

	u, err := repo.UpsertTelegramUser(ctx, 777, "zagir", "Загир", "ru")
	if err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if u.ID < firstSyntheticUserID {
		t.Fatalf("_id = %d, want синтетический (>= %d)", u.ID, firstSyntheticUserID)
	}
	if u.TelegramID == nil || *u.TelegramID != 777 {
		t.Fatalf("telegram_id = %v, want 777", u.TelegramID)
	}
	if u.GoogleSub != "" {
		t.Fatalf("чужой google_sub протёк в нового пользователя: %+v", u)
	}

	// чужой аккаунт не тронут
	stranger, err := repo.FindById(ctx, 777)
	if err != nil {
		t.Fatalf("FindById(777): %v", err)
	}
	if stranger.GoogleSub != "sub-777" || stranger.TelegramID != nil {
		t.Fatalf("чужой аккаунт изменён: %+v", stranger)
	}

	// повторный апдейт находит созданного по личности, а не заводит третьего
	again, err := repo.UpsertTelegramUser(ctx, 777, "zagir", "Загир", "ru")
	if err != nil {
		t.Fatalf("повторный UpsertTelegramUser: %v", err)
	}
	if again.ID != u.ID {
		t.Fatalf("повторный вход дал другой _id: %d != %d", again.ID, u.ID)
	}
	assertUserCount(t, db, 2)
}

// TestUpsertTelegramUserFindsExisting — существующий находится по telegram_id, а
// не по _id. Проверяется на историческом аккаунте (_id == telegram id) и на
// аккаунте с синтетическим номером — второй ломался бы при поиске по _id
func TestUpsertTelegramUserFindsExisting(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	seedUsers(t, db,
		api.User{ID: 111, Username: "old", DisplayName: "Исторический", TelegramID: intPtr(111), UserLang: "ru"},
		api.User{ID: 1_000_000_000_500, Username: "goo", DisplayName: "Гугловый", GoogleSub: "sub-g", TelegramID: intPtr(222), UserLang: "en"},
	)

	tests := []struct {
		name   string
		tgID   int
		wantID int
	}{
		{"исторический аккаунт", 111, 111},
		{"google-аккаунт с привязанным telegram", 222, 1_000_000_000_500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := repo.UpsertTelegramUser(ctx, tc.tgID, "new-name", "Новое Имя", "de")
			if err != nil {
				t.Fatalf("UpsertTelegramUser: %v", err)
			}
			if u.ID != tc.wantID {
				t.Fatalf("_id = %d, want %d — найденному пользователю _id менять нельзя", u.ID, tc.wantID)
			}
		})
	}
	assertUserCount(t, db, 2)
}

// TestUpsertTelegramUserProfileUpdates — смена username долетает до базы, а
// user_lang не затирается: язык принадлежит пользователю (он мог выбрать его
// руками), апдейты бота его переписывать не должны
func TestUpsertTelegramUserProfileUpdates(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	// у пользователя языка нет — первый же апдейт его проставляет
	if _, err := repo.UpsertTelegramUser(ctx, 900, "old", "Старое Имя", ""); err != nil {
		t.Fatalf("создание: %v", err)
	}
	u, err := repo.UpsertTelegramUser(ctx, 900, "old", "Старое Имя", "ru")
	if err != nil {
		t.Fatalf("проставление языка: %v", err)
	}
	if u.UserLang != "ru" {
		t.Fatalf("user_lang = %q, want ru — пустой язык обязан заполняться", u.UserLang)
	}

	// заполненный язык не трогаем, а username и display_name обновляем
	u, err = repo.UpsertTelegramUser(ctx, 900, "new", "Новое Имя", "en")
	if err != nil {
		t.Fatalf("апдейт профиля: %v", err)
	}
	if u.UserLang != "ru" {
		t.Fatalf("user_lang = %q, want ru — заполненный язык затирать нельзя", u.UserLang)
	}
	if u.Username != "new" || u.DisplayName != "Новое Имя" {
		t.Fatalf("профиль не обновлён: %+v", u)
	}

	// пустое имя не затирает известное боту
	u, err = repo.UpsertTelegramUser(ctx, 900, "new", "  ", "en")
	if err != nil {
		t.Fatalf("апдейт с пустым именем: %v", err)
	}
	if u.DisplayName != "Новое Имя" {
		t.Fatalf("display_name = %q — пустое имя не должно затирать известное", u.DisplayName)
	}
	assertUserCount(t, db, 1)
}

// TestUpsertTelegramUserPreservesIdentityFields — резолв telegram не трогает
// поля личности других провайдеров: у google-первого пользователя, привязавшего
// telegram, каждый апдейт бота проходит через UpsertTelegramUser
func TestUpsertTelegramUserPreservesIdentityFields(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	seedUsers(t, db, api.User{
		ID: 1_000_000_000_700, Username: "goo", DisplayName: "Гугловый",
		GoogleSub: "sub-keep", Email: "g@example.com", TelegramID: intPtr(333),
	})

	if _, err := repo.UpsertTelegramUser(ctx, 333, "goo2", "Гугловый 2", "ru"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	got, err := repo.FindById(ctx, 1_000_000_000_700)
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if got.GoogleSub != "sub-keep" || got.Email != "g@example.com" {
		t.Fatalf("поля личности затёрты: %+v", got)
	}
}

// TestUpsertTelegramUserDeletedNotResurrected — tombstone по личности не
// находится, поэтому человек регистрируется заново; его _id занят трупом, значит
// новый аккаунт получает синтетический номер
func TestUpsertTelegramUserDeletedNotResurrected(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	deletedAt := time.Now().Add(-time.Hour)
	// личность у tombstone вычищена (см. удаление аккаунта), остался только _id
	seedUsers(t, db, api.User{ID: 444, DisplayName: "Удалённый", DeletedAt: &deletedAt})

	u, err := repo.UpsertTelegramUser(ctx, 444, "zagir", "Загир", "ru")
	if err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if u.ID < firstSyntheticUserID {
		t.Fatalf("_id = %d — новый аккаунт не должен занимать номер tombstone", u.ID)
	}
	if u.IsDeleted() {
		t.Fatalf("воскрешён tombstone: %+v", u)
	}
}

// TestUpsertTelegramUserRace — гонка двух апдейтов одного нового пользователя.
// Оба вызова обязаны завершиться успешно и вернуть ОДИН И ТОТ ЖЕ _id: проигравший
// на duplicate key переспрашивает базу по telegram_id и подбирает документ,
// созданный победителем. Ветка «занят → сразу аллокатор» здесь потеряла бы
// апдейт, упершись в unique-индекс по telegram_id
func TestUpsertTelegramUserRace(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	const goroutines = 8
	ids := make([]int, goroutines)
	errs := make([]error, goroutines)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			u, err := repo.UpsertTelegramUser(ctx, 12345, "zagir", "Загир", "ru")
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = u.ID
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("горутина %d получила ошибку: %v", i, err)
		}
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("горутина %d вернула _id=%d, а горутина 0 — %d: создано два аккаунта на одну личность", i, id, ids[0])
		}
	}
	assertUserCount(t, db, 1)
}

// assertUserCount — сколько документов в коллекции user. Дубли аккаунтов ловятся
// только счётом: возвращаемый вызовами _id может совпасть и при живом дубле
func assertUserCount(t *testing.T, db *mongo.Database, want int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testMongoTimeout)
	defer cancel()
	n, err := db.Collection("user").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count user: %v", err)
	}
	if n != want {
		t.Fatalf("в коллекции user %d документов, ожидалось %d — заведён лишний аккаунт", n, want)
	}
}
