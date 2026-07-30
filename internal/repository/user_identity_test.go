package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func intPtr(v int) *int { return &v }

// TestFindByIdentity — поиск по каждой из трёх личностей находит нужного
// пользователя, чужая личность и несуществующая — mongo.ErrNoDocuments
func TestFindByIdentity(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db,
		api.User{ID: 111, Username: "tg", DisplayName: "Telegram User", TelegramID: intPtr(111)},
		api.User{ID: 1_000_000_000_001, Username: "g", DisplayName: "Google User", GoogleSub: "google-sub-1", Email: "g@example.com"},
		api.User{ID: 1_000_000_000_002, Username: "a", DisplayName: "Apple User", AppleSub: "apple-sub-1"},
	)

	tests := []struct {
		name   string
		find   func() (*api.User, error)
		wantID int
	}{
		{"telegram", func() (*api.User, error) { return repo.FindByTelegramID(ctx, 111) }, 111},
		{"google", func() (*api.User, error) { return repo.FindByGoogleSub(ctx, "google-sub-1") }, 1_000_000_000_001},
		{"apple", func() (*api.User, error) { return repo.FindByAppleSub(ctx, "apple-sub-1") }, 1_000_000_000_002},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := tc.find()
			if err != nil {
				t.Fatalf("поиск по личности %s не нашёл пользователя: %v", tc.name, err)
			}
			if u.ID != tc.wantID {
				t.Fatalf("найден не тот пользователь: got _id=%d, want %d", u.ID, tc.wantID)
			}
			// дефолты FindById обязаны применяться и здесь
			if u.CountInPage != 5 || u.NotificationOn == nil || !*u.NotificationOn {
				t.Fatalf("дефолты не применены: count_in_page=%d notification_on=%v", u.CountInPage, u.NotificationOn)
			}
		})
	}

	notFound := []struct {
		name string
		find func() (*api.User, error)
	}{
		{"telegram", func() (*api.User, error) { return repo.FindByTelegramID(ctx, 999) }},
		{"google", func() (*api.User, error) { return repo.FindByGoogleSub(ctx, "nope") }},
		{"apple", func() (*api.User, error) { return repo.FindByAppleSub(ctx, "nope") }},
		// пустая строка не должна матчить документы, где поля просто нет
		{"empty google sub", func() (*api.User, error) { return repo.FindByGoogleSub(ctx, "") }},
	}
	for _, tc := range notFound {
		t.Run("не найден: "+tc.name, func(t *testing.T) {
			if _, err := tc.find(); err != mongo.ErrNoDocuments {
				t.Fatalf("ожидался mongo.ErrNoDocuments, получено: %v", err)
			}
		})
	}
}

// TestFindByIdentitySkipsDeleted — tombstone не находится ни по одной личности:
// иначе удалённый аккаунт блокировал бы повторную регистрацию с теми же
// google_sub/apple_sub/telegram_id
func TestFindByIdentitySkipsDeleted(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	deletedAt := time.Now().UTC()
	seedUsers(t, db, api.User{
		ID:         500,
		TelegramID: intPtr(500),
		GoogleSub:  "dead-google",
		AppleSub:   "dead-apple",
		DeletedAt:  &deletedAt,
	})

	if _, err := repo.FindByTelegramID(ctx, 500); err != mongo.ErrNoDocuments {
		t.Fatalf("удалённый найден по telegram_id: %v", err)
	}
	if _, err := repo.FindByGoogleSub(ctx, "dead-google"); err != mongo.ErrNoDocuments {
		t.Fatalf("удалённый найден по google_sub: %v", err)
	}
	if _, err := repo.FindByAppleSub(ctx, "dead-apple"); err != mongo.ErrNoDocuments {
		t.Fatalf("удалённый найден по apple_sub: %v", err)
	}
	// FindById удалённых по-прежнему отдаёт: он нужен админским/служебным путям
	if _, err := repo.FindById(ctx, 500); err != nil {
		t.Fatalf("FindById не нашёл tombstone: %v", err)
	}
}

// TestEnsureIndexesUniqueSparse — unique-индекс отвергает вторую личность, а
// sparse позволяет сосуществовать сколь угодно многим пользователям без неё
func TestEnsureIndexesUniqueSparse(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes упал: %v", err)
	}
	// идемпотентность: повторный вызов на старте не должен падать
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("повторный EnsureIndexes упал: %v", err)
	}

	if err := repo.CreateIdentityUser(ctx, api.User{ID: 1, GoogleSub: "same-sub"}); err != nil {
		t.Fatalf("первый пользователь не создан: %v", err)
	}
	err := repo.CreateIdentityUser(ctx, api.User{ID: 2, GoogleSub: "same-sub"})
	if !IsDuplicateKey(err) {
		t.Fatalf("ожидался duplicate key по google_sub, получено: %v", err)
	}

	// sparse: у обоих google_sub отсутствует — конфликта быть не должно
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 3, TelegramID: intPtr(3)}); err != nil {
		t.Fatalf("третий пользователь без google_sub не создан: %v", err)
	}
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 4, TelegramID: intPtr(4)}); err != nil {
		t.Fatalf("четвёртый пользователь без google_sub не создан (sparse не работает): %v", err)
	}

	// unique по telegram_id и apple_sub
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 5, TelegramID: intPtr(4)}); !IsDuplicateKey(err) {
		t.Fatalf("ожидался duplicate key по telegram_id, получено: %v", err)
	}
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 6, AppleSub: "apple"}); err != nil {
		t.Fatalf("пользователь с apple_sub не создан: %v", err)
	}
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 7, AppleSub: "apple"}); !IsDuplicateKey(err) {
		t.Fatalf("ожидался duplicate key по apple_sub, получено: %v", err)
	}
}

// TestCreateIdentityUserDuplicateId — занятый _id даёт duplicate key, а не
// молчаливую перезапись: на этой ошибке строится retry входа в Tasks 6/10/11
func TestCreateIdentityUserDuplicateId(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.CreateIdentityUser(ctx, api.User{ID: 42, DisplayName: "First", GoogleSub: "sub-a"}); err != nil {
		t.Fatalf("первый пользователь не создан: %v", err)
	}
	err := repo.CreateIdentityUser(ctx, api.User{ID: 42, DisplayName: "Second", GoogleSub: "sub-b"})
	if !IsDuplicateKey(err) {
		t.Fatalf("ожидался duplicate key по _id, получено: %v", err)
	}

	got, err := repo.FindById(ctx, 42)
	if err != nil {
		t.Fatalf("FindById упал: %v", err)
	}
	if got.DisplayName != "First" || got.GoogleSub != "sub-a" {
		t.Fatalf("существующий документ перезаписан: %+v", got)
	}
}

// TestCreateIdentityUserWritesIdentityFields — CreateIdentityUser пишет поля
// личности целиком (ради этого он и заведён: UpsertUser их не касается)
func TestCreateIdentityUserWritesIdentityFields(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	u := api.User{
		ID:          1_000_000_000_007,
		DisplayName: "Google User",
		UserLang:    "ru",
		GoogleSub:   "gsub",
		Email:       "user@example.com",
	}
	if err := repo.CreateIdentityUser(ctx, u); err != nil {
		t.Fatalf("создание упало: %v", err)
	}

	got, err := repo.FindByGoogleSub(ctx, "gsub")
	if err != nil {
		t.Fatalf("созданный не найден по google_sub: %v", err)
	}
	if got.ID != u.ID || got.Email != "user@example.com" || got.UserLang != "ru" {
		t.Fatalf("поля записаны неверно: %+v", got)
	}
	if got.TelegramID != nil {
		t.Fatalf("telegram_id не должен появляться сам: %v", *got.TelegramID)
	}
}

// TestUpsertUserKeepsIdentityFields — частичный $set в UpsertUser не затирает
// личности: вход по /auth/code и апдейты бота идут именно через него
func TestUpsertUserKeepsIdentityFields(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db, api.User{
		ID:          1_000_000_000_003,
		Username:    "old",
		DisplayName: "Old Name",
		UserLang:    "ru",
		GoogleSub:   "keep-me",
		AppleSub:    "keep-me-too",
		TelegramID:  intPtr(777),
		Email:       "keep@example.com",
	})

	updated, err := repo.UpsertUser(ctx, api.User{
		ID:          1_000_000_000_003,
		Username:    "new",
		DisplayName: "New Name",
		UserLang:    "en",
	})
	if err != nil {
		t.Fatalf("UpsertUser упал: %v", err)
	}
	if updated.Username != "new" || updated.DisplayName != "New Name" || updated.UserLang != "en" {
		t.Fatalf("профильные поля не обновились: %+v", updated)
	}
	if updated.GoogleSub != "keep-me" || updated.AppleSub != "keep-me-too" || updated.Email != "keep@example.com" {
		t.Fatalf("поля личности затёрты: %+v", updated)
	}
	if updated.TelegramID == nil || *updated.TelegramID != 777 {
		t.Fatalf("telegram_id затёрт: %v", updated.TelegramID)
	}
}

// identityKeys — поля, которых во встроенных снимках комнаты быть не должно
// никогда (см. api.User.Snapshot)
var identityKeys = []string{"telegram_id", "google_sub", "apple_sub", "email", "apple_refresh_token", "push_tokens", "deleted_at", "bank_details"}

// assertNoIdentityFields рекурсивно обходит документ комнаты и падает, если
// встречает поле личности на любой глубине
func assertNoIdentityFields(t *testing.T, path string, v interface{}) {
	t.Helper()
	switch val := v.(type) {
	case bson.M:
		for k, sub := range val {
			for _, forbidden := range identityKeys {
				if k == forbidden {
					t.Errorf("поле личности %q просочилось в документ комнаты: %s.%s = %v", k, path, k, sub)
				}
			}
			assertNoIdentityFields(t, path+"."+k, sub)
		}
	case bson.D:
		assertNoIdentityFields(t, path, val.Map())
	case bson.A:
		for _, sub := range val {
			assertNoIdentityFields(t, path, sub)
		}
	case []interface{}:
		for _, sub := range val {
			assertNoIdentityFields(t, path, sub)
		}
	}
}

// userWithIdentity — пользователь со всеми полями личности разом: любой путь
// записи в комнату обязан их срезать
func userWithIdentity(id int) api.User {
	deletedAt := time.Now().UTC()
	return api.User{
		ID:                id,
		Username:          "user",
		DisplayName:       "User",
		UserLang:          "ru",
		TelegramID:        intPtr(id),
		GoogleSub:         "gsub",
		AppleSub:          "asub",
		Email:             "leak@example.com",
		AppleRefreshToken: "refresh",
		BankDetails:       "секретный счёт",
		DeletedAt:         &deletedAt,
		PushTokens:        []api.PushToken{{Token: "fcm", Platform: "ios"}},
		Aliases:           []string{"саня"},
	}
}

// TestRoomSnapshotsSanitized — санитайз на границе репозитория: ни SaveRoom, ни
// JoinToRoom, ни операции не переносят поля личности в документ room
func TestRoomSnapshotsSanitized(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	rooms := NewRoomRepository(db)

	creator := userWithIdentity(1_000_000_000_010)
	room := &api.Room{
		Name:       "Test Room",
		Members:    &[]api.User{creator},
		Operations: &[]api.Operation{},
		CreateAt:   time.Now(),
	}
	id, err := rooms.SaveRoom(ctx, room)
	if err != nil {
		t.Fatalf("SaveRoom упал: %v", err)
	}
	roomId := id.Hex()

	joiner := userWithIdentity(1_000_000_000_011)
	if err := rooms.JoinToRoom(ctx, joiner, roomId); err != nil {
		t.Fatalf("JoinToRoom упал: %v", err)
	}

	op := &api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "обед",
		Donor:             &creator,
		Recipients:        &[]api.User{joiner},
		RecipientsWithSum: []api.RecipientWithSum{{User: joiner, Sum: 100}},
		Sum:               100,
		CreateAt:          time.Now(),
	}
	if err := rooms.CreateOperation(ctx, op, roomId); err != nil {
		t.Fatalf("CreateOperation упал: %v", err)
	}

	idempotent := &api.Operation{
		ID:                primitive.NewObjectID(),
		ClientOpId:        "client-op-1",
		Description:       "ужин",
		Donor:             &joiner,
		RecipientsWithSum: []api.RecipientWithSum{{User: creator, Sum: 50}},
		Sum:               50,
		CreateAt:          time.Now(),
	}
	inserted, err := rooms.CreateOperationIfAbsent(ctx, idempotent, roomId)
	if err != nil || !inserted {
		t.Fatalf("CreateOperationIfAbsent: inserted=%v err=%v", inserted, err)
	}

	op.Description = "обед (правка)"
	if err := rooms.UpdateOperation(ctx, op, roomId); err != nil {
		t.Fatalf("UpdateOperation упал: %v", err)
	}

	var raw bson.M
	if err := db.Collection("room").FindOne(ctx, bson.M{"_id": id}).Decode(&raw); err != nil {
		t.Fatalf("не удалось прочитать документ комнаты: %v", err)
	}
	assertNoIdentityFields(t, "room", raw)

	// содержательные поля снимка остаются на месте — санитайз не должен
	// превращать участников в пустышки
	got, err := rooms.FindById(ctx, roomId)
	if err != nil {
		t.Fatalf("FindById упал: %v", err)
	}
	if got.Members == nil || len(*got.Members) != 2 {
		t.Fatalf("ожидалось 2 участника, получено %+v", got.Members)
	}
	for _, m := range *got.Members {
		if m.DisplayName != "User" || m.UserLang != "ru" || m.ID == 0 {
			t.Fatalf("снимок участника испорчен: %+v", m)
		}
	}
	if got.Operations == nil || len(*got.Operations) != 2 {
		t.Fatalf("ожидалось 2 операции, получено %+v", got.Operations)
	}
	for _, o := range *got.Operations {
		if o.Donor == nil || o.Donor.ID == 0 || o.Donor.DisplayName != "User" {
			t.Fatalf("снимок донора испорчен: %+v", o.Donor)
		}
		if len(o.RecipientsWithSum) != 1 || o.RecipientsWithSum[0].User.ID == 0 || o.RecipientsWithSum[0].Sum == 0 {
			t.Fatalf("доли получателей испорчены: %+v", o.RecipientsWithSum)
		}
	}
	if (*got.Operations)[0].Description != "обед (правка)" {
		t.Fatalf("UpdateOperation не применился: %+v", (*got.Operations)[0])
	}
}

// TestSanitizeDoesNotMutateArgument — санитайз работает по копии: вызывающий код
// после записи продолжает пользоваться полным объектом (например, шлёт по нему
// уведомления и берёт telegram_id)
func TestSanitizeDoesNotMutateArgument(t *testing.T) {
	u := userWithIdentity(7)
	op := &api.Operation{
		Donor:             &u,
		Recipients:        &[]api.User{u},
		RecipientsWithSum: []api.RecipientWithSum{{User: u, Sum: 10}},
	}
	room := &api.Room{Members: &[]api.User{u}, Operations: &[]api.Operation{*op}}

	sanitizedOp := sanitizeOperation(op)
	sanitizedRoom := sanitizeRoom(room)

	if op.Donor.Email == "" || !op.Donor.HasTelegram() {
		t.Fatalf("аргумент операции испорчен: %+v", op.Donor)
	}
	if (*op.Recipients)[0].Email == "" || op.RecipientsWithSum[0].User.Email == "" {
		t.Fatalf("аргумент получателей испорчен")
	}
	if (*room.Members)[0].Email == "" {
		t.Fatalf("аргумент комнаты испорчен")
	}
	if sanitizedOp.Donor.Email != "" || sanitizedOp.Donor.HasTelegram() {
		t.Fatalf("донор не санитайзнут: %+v", sanitizedOp.Donor)
	}
	if (*sanitizedRoom.Members)[0].Email != "" || (*sanitizedRoom.Operations)[0].Donor.Email != "" {
		t.Fatalf("комната не санитайзнута")
	}
	if sanitizeOperation(nil) != nil || sanitizeRoom(nil) != nil || sanitizeUsers(nil) != nil {
		t.Fatalf("nil должен оставаться nil")
	}
}

// TestUpdateAppleProfile — дозаполнение данных, которые Apple присылает только
// при первом входе. Пустые аргументы не пишутся, tombstone не трогается
func TestUpdateAppleProfile(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db, api.User{ID: 1_000_000_000_010, AppleSub: "asub", DisplayName: "Загир", Email: "keep@example.com"})

	// пустые значения не затирают сохранённое, refresh token записывается
	if err := repo.UpdateAppleProfile(ctx, 1_000_000_000_010, "", "", "refresh-1"); err != nil {
		t.Fatalf("UpdateAppleProfile упал: %v", err)
	}
	got, err := repo.FindByAppleSub(ctx, "asub")
	if err != nil {
		t.Fatalf("пользователь не найден: %v", err)
	}
	if got.Email != "keep@example.com" || got.DisplayName != "Загир" {
		t.Fatalf("сохранённый профиль затёрт: %+v", got)
	}
	if got.AppleRefreshToken != "refresh-1" {
		t.Fatalf("apple_refresh_token = %q, want refresh-1", got.AppleRefreshToken)
	}

	// непустые значения пишутся, refresh token обновляется на новый
	if err := repo.UpdateAppleProfile(ctx, 1_000_000_000_010, "new@example.com", "Zagir", "refresh-2"); err != nil {
		t.Fatalf("UpdateAppleProfile упал: %v", err)
	}
	if got, err = repo.FindByAppleSub(ctx, "asub"); err != nil {
		t.Fatalf("пользователь не найден: %v", err)
	}
	if got.Email != "new@example.com" || got.DisplayName != "Zagir" || got.AppleRefreshToken != "refresh-2" {
		t.Fatalf("обновление не применилось: %+v", got)
	}
}

// TestUpdateAppleProfileSkipsDeleted — гонка «медленный вход через Apple ↔
// удаление аккаунта»: дописывать refresh token в tombstone нельзя, иначе
// удалённый аккаунт снова обзаводится живым токеном Apple
func TestUpdateAppleProfileSkipsDeleted(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	deletedAt := time.Now()
	seedUsers(t, db, api.User{ID: 1_000_000_000_011, DisplayName: "Удалённый", DeletedAt: &deletedAt})

	if err := repo.UpdateAppleProfile(ctx, 1_000_000_000_011, "leak@example.com", "Загир", "refresh-1"); err != nil {
		t.Fatalf("UpdateAppleProfile упал: %v", err)
	}
	got, err := repo.FindById(ctx, 1_000_000_000_011)
	if err != nil {
		t.Fatalf("tombstone не найден: %v", err)
	}
	if got.Email != "" || got.AppleRefreshToken != "" || got.DisplayName != "Удалённый" {
		t.Fatalf("tombstone дополнен данными Apple: %+v", got)
	}
}
