package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Интеграционные тесты инварианта удаления аккаунта (Task 13, Apple Guideline
// 5.1.1(v) / GDPR): TOMBSTONE НИКОГДА НЕ ПОЛУЧАЕТ PII ОБРАТНО.
//
// Проверяется против ЖИВОГО mongo, а не фейка: половина проблемы была именно в
// поведении драйвера — upsert по фильтру, который не совпал с tombstone, не
// «ничего не делает», а ПЫТАЕТСЯ ВСТАВИТЬ документ. Фейк такого не покажет.
//
// Каждый мутатор проверяется дважды: на tombstone он обязан не записать ничего,
// на живом пользователе — работать ровно как раньше.

const (
	twUserID = 8801
	twTgID   = 990088
)

// twSeedUser — «богатый» пользователь: заполнено каждое поле из
// snapshotPIIFields, чтобы возврат любого из них на tombstone был виден
func twSeedUser() api.User {
	tg := twTgID
	return api.User{
		ID: twUserID, TelegramID: &tg,
		Username: "zagir", DisplayName: "Загир", UserLang: "ru",
		Email: "zagir@example.com", GoogleSub: "g-tw", AppleSub: "a-tw",
		AppleRefreshToken: "apple-refresh-tw",
		BankDetails:       "4276 0000 1111 2222",
		Aliases:           []string{"заги"},
		PushTokens:        []api.PushToken{{Token: "fcm-tw", Platform: "ios"}},
	}
}

// twRepo поднимает базу с одним живым пользователем twSeedUser
func twRepo(t *testing.T) (*MongoUserRepository, *mongo.Database) {
	t.Helper()
	db := testDB(t)
	seedUsers(t, db, twSeedUser())
	return NewUserRepository(db), db
}

// twTombstoned — та же база, но пользователь уже удалён
func twTombstoned(t *testing.T) (*MongoUserRepository, *mongo.Database) {
	t.Helper()
	repo, db := twRepo(t)
	if err := repo.SoftDeleteUser(testCtx(t), twUserID); err != nil {
		t.Fatalf("SoftDeleteUser: %v", err)
	}
	return repo, db
}

// twRaw читает СЫРОЙ документ: проверять надо наличие полей в базе, а не то,
// что вернул декодер модели (у пустой строки и отсутствующего поля разница
// именно в базе)
func twRaw(t *testing.T, db *mongo.Database, id int) bson.M {
	t.Helper()
	var doc bson.M
	if err := db.Collection("user").FindOne(testCtx(t), bson.M{"_id": id}).Decode(&doc); err != nil {
		t.Fatalf("не удалось прочитать документ %d: %v", id, err)
	}
	return doc
}

// twAssertClean — на tombstone не вернулось ни одного поля PII, он не воскрес,
// не размножился и вообще НИ ОДНО его поле не изменилось. wantDocs — сколько
// документов должно остаться в коллекции: один (отказ) или два (мутатор имел
// право завести НОВЫЙ аккаунт рядом).
//
// before — сырой документ, снятый ДО вызова мутатора; сравнивается целиком, а не
// по списку полей. Перечислением тут не обойтись: настройки профиля
// (selected_lang, count_in_page, notification_on, notify) в snapshotPIIFields не
// входят — сами по себе это не PII, — но запись любой из них означает, что
// фильтр по deleted_at не сработал. Проверкой «поля нет» они тоже не ловятся:
// пустые значения лежат в документе с самой вставки, значение имеет только
// НЕИЗМЕННОСТЬ
func twAssertClean(t *testing.T, db *mongo.Database, where string, before bson.M, wantDocs int64) {
	t.Helper()

	n, err := db.Collection("user").CountDocuments(testCtx(t), bson.M{})
	if err != nil {
		t.Fatalf("%s: count: %v", where, err)
	}
	if n != wantDocs {
		t.Fatalf("%s: в коллекции %d документов, want %d (вставка вместо отказа?)", where, n, wantDocs)
	}

	doc := twRaw(t, db, twUserID)
	if _, ok := doc["deleted_at"]; !ok {
		t.Fatalf("%s: с документа пропал deleted_at — аккаунт воскрес", where)
	}
	if got := doc["display_name"]; got != DeletedUserPlaceholder {
		t.Errorf("%s: display_name = %v, want %q", where, got, DeletedUserPlaceholder)
	}
	for _, field := range append(append([]string{}, snapshotPIIFields...), tombstoneExtraFields...) {
		if v, ok := doc[field]; ok {
			t.Errorf("%s: на tombstone вернулось поле %s = %v", where, field, v)
		}
	}
	for field, want := range before {
		got, ok := doc[field]
		if !ok {
			t.Errorf("%s: с tombstone пропало поле %s (было %v)", where, field, want)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: на tombstone изменилось поле %s: %v -> %v", where, field, want, got)
		}
	}
	for field, got := range doc {
		if _, ok := before[field]; !ok {
			t.Errorf("%s: на tombstone появилось поле %s = %v", where, field, got)
		}
	}
}

// twWriter — один мутатор коллекции user
type twWriter struct {
	name string
	// call пишет от имени внешнего запроса, впущенного за миг до DELETE /me
	call func(ctx context.Context, repo *MongoUserRepository) error
	// wantErrOnTombstone — что метод обязан вернуть, встретив tombstone.
	// nil означает «тихий no-op»: настройки удалённого аккаунта менять некому,
	// и 500 в ответ на гонку с собственным DELETE /me ничего не объясняет
	wantErrOnTombstone error
}

// twWriters — ВСЕ мутаторы user, кроме SoftDeleteUser (он обязан работать по
// tombstone: удаление повторяемо) и RemovePushToken (он только УБИРАЕТ данные,
// и отбраковка мёртвого токена приходит уже после удаления аккаунта).
//
// Новый мутатор профиля добавляется сюда же: список — исполняемая версия
// правила «фильтр по _id + отсутствие deleted_at, без upsert» из плана Task 12
func twWriters() []twWriter {
	return []twWriter{
		{
			name: "UpsertUser",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				_, err := repo.UpsertUser(ctx, api.User{
					ID: twUserID, Username: "zagir", DisplayName: "Загир", UserLang: "ru",
				})
				return err
			},
			wantErrOnTombstone: ErrUserDeleted,
		},
		{
			name: "SetUserLang",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetUserLang(ctx, twUserID, "en")
			},
		},
		{
			name: "SetCountInPage",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetCountInPage(ctx, twUserID, 15)
			},
		},
		{
			name: "SetNotificationUser",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetNotificationUser(ctx, twUserID, false)
			},
		},
		{
			name: "SetNotifySettings",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetNotifySettings(ctx, twUserID, api.NotifySettings{})
			},
		},
		{
			// bank_details входит в snapshotPIIFields
			name: "SetUserBankDetails",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetUserBankDetails(ctx, twUserID, "5555 4444 3333 2222")
			},
		},
		{
			// aliases входит в snapshotPIIFields, а писать сюда может ЛЮБОЙ
			// участник общей комнаты — не только сам пользователь
			name: "AddAlias",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.AddAlias(ctx, twUserID, "заги")
			},
			wantErrOnTombstone: mongo.ErrNoDocuments,
		},
		{
			// push_tokens входит в snapshotPIIFields: токен на tombstone вернул
			// бы удалённому аккаунту адрес доставки пушей
			name: "AddPushToken",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.AddPushToken(ctx, twUserID, api.PushToken{Token: "fcm-tw", Platform: "ios"})
			},
			wantErrOnTombstone: mongo.ErrNoDocuments,
		},
		{
			name: "UpdateAppleProfile",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.UpdateAppleProfile(ctx, twUserID, "zagir@example.com", "Загир", "apple-refresh-tw")
			},
		},
		{
			name: "SetIdentity",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				return repo.SetIdentity(ctx, twUserID, IdentityGoogle, "g-tw")
			},
			wantErrOnTombstone: mongo.ErrNoDocuments,
		},
		{
			// refreshTelegramProfile — самый коварный: апдейт из Telegram нашёл
			// ЖИВОГО пользователя, а пишет уже после чистки PII. Именно поэтому
			// сюда передаётся снимок, прочитанный ДО удаления, и ИЗМЕНИВШИЙСЯ
			// профиль — совпадающий метод не пишет вовсе и гонку не покажет
			name: "refreshTelegramProfile",
			call: func(ctx context.Context, repo *MongoUserRepository) error {
				stale := twSeedUser()
				_, err := repo.refreshTelegramProfile(ctx, &stale, "zagir_new", "Загир Новый", "en")
				return err
			},
			wantErrOnTombstone: mongo.ErrNoDocuments,
		},
	}
}

// ⚠️ Главный тест инварианта: ни один мутатор не возвращает PII на tombstone.
//
// Гонка, которую он воспроизводит: запрос прошёл auth-middleware, пока аккаунт
// был жив (middleware держит вердикт в кеше до accountTTL), параллельный
// DELETE /me поставил tombstone и вычистил PII — и запись, дошедшая до базы
// после этого, вернула бы человеку в базу его настоящее имя, username, язык,
// банковские реквизиты, прозвища или FCM-токен
func TestUserWritersNeverResurrectTombstone(t *testing.T) {
	for _, w := range twWriters() {
		w := w
		t.Run(w.name, func(t *testing.T) {
			repo, db := twTombstoned(t)
			before := twRaw(t, db, twUserID)

			err := w.call(testCtx(t), repo)
			switch {
			case w.wantErrOnTombstone == nil && err != nil:
				t.Fatalf("%s на tombstone: err = %v, want nil (тихий no-op)", w.name, err)
			case w.wantErrOnTombstone != nil && !errors.Is(err, w.wantErrOnTombstone):
				t.Fatalf("%s на tombstone: err = %v, want %v", w.name, err, w.wantErrOnTombstone)
			}

			twAssertClean(t, db, w.name, before, 1)
		})
	}
}

// Обратная половина: на ЖИВОМ пользователе те же вызовы обязаны отработать без
// ошибки. Иначе «починка» превратилась бы в поломку профиля всем остальным
func TestUserWritersStillWriteToLiveUser(t *testing.T) {
	for _, w := range twWriters() {
		w := w
		t.Run(w.name, func(t *testing.T) {
			repo, db := twRepo(t)

			if err := w.call(testCtx(t), repo); err != nil {
				t.Fatalf("%s на живом пользователе: %v", w.name, err)
			}
			if _, ok := twRaw(t, db, twUserID)["deleted_at"]; ok {
				t.Fatalf("%s пометил живого пользователя удалённым", w.name)
			}
		})
	}
}

// Значения на живом пользователе действительно долетают до базы: предыдущий
// тест ловит только ошибки, а тихий no-op ошибки не даёт — без этой проверки
// «фильтр по живому» можно было бы случайно сделать фильтром «по никому»
func TestLiveProfileMutatorsWriteValues(t *testing.T) {
	repo, db := twRepo(t)
	ctx := testCtx(t)

	if err := repo.SetUserLang(ctx, twUserID, "en"); err != nil {
		t.Fatalf("SetUserLang: %v", err)
	}
	if err := repo.SetCountInPage(ctx, twUserID, 15); err != nil {
		t.Fatalf("SetCountInPage: %v", err)
	}
	if err := repo.SetNotificationUser(ctx, twUserID, false); err != nil {
		t.Fatalf("SetNotificationUser: %v", err)
	}
	if err := repo.SetUserBankDetails(ctx, twUserID, "5555"); err != nil {
		t.Fatalf("SetUserBankDetails: %v", err)
	}
	if err := repo.AddAlias(ctx, twUserID, "новое"); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	if err := repo.AddPushToken(ctx, twUserID, api.PushToken{Token: "fcm-2", Platform: "android"}); err != nil {
		t.Fatalf("AddPushToken: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, api.User{ID: twUserID, Username: "new-name", DisplayName: "Новое имя", UserLang: "en"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	doc := twRaw(t, db, twUserID)
	for field, want := range map[string]interface{}{
		"selected_lang":   "en",
		"count_in_page":   int32(15),
		"notification_on": false,
		"bank_details":    "5555",
		"user_name":       "new-name",
		"display_name":    "Новое имя",
	} {
		if got := doc[field]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", field, got, got, want, want)
		}
	}

	user, err := repo.FindById(ctx, twUserID)
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if len(user.Aliases) != 2 {
		t.Errorf("aliases = %v, want два прозвища", user.Aliases)
	}
	if len(user.PushTokens) != 2 {
		t.Errorf("push_tokens = %v, want два токена", user.PushTokens)
	}
}

// UpsertUser обязан по-прежнему СОЗДАВАТЬ пользователя: на нём держится первый
// dev-вход (POST /auth/dev), а upsert из метода убран
func TestUpsertUserCreatesMissingUser(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)

	created, err := repo.UpsertUser(testCtx(t), api.User{
		ID: twUserID, Username: "newbie", DisplayName: "Новичок", UserLang: "ru", DevAuth: true,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if created == nil || created.Username != "newbie" || created.DisplayName != "Новичок" {
		t.Fatalf("создан не тот пользователь: %+v", created)
	}
	if got := twRaw(t, db, twUserID)["dev_auth"]; got != true {
		t.Errorf("dev_auth = %v, want true", got)
	}

	// повторный вызов обновляет, а не плодит второй документ
	if _, err = repo.UpsertUser(testCtx(t), api.User{ID: twUserID, Username: "renamed", DisplayName: "Переименован"}); err != nil {
		t.Fatalf("повторный UpsertUser: %v", err)
	}
	n, err := db.Collection("user").CountDocuments(testCtx(t), bson.M{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("в коллекции %d документов, want 1", n)
	}
	if got := twRaw(t, db, twUserID)["user_name"]; got != "renamed" {
		t.Errorf("user_name = %v, want renamed", got)
	}
}

// Апдейт из Telegram, стартовавший до удаления, не возвращает имя и username на
// tombstone, а UpsertTelegramUser доводит человека до НОВОГО аккаунта: tombstone
// освободил telegram_id, и следующий /start обязан просто завести новый профиль
func TestUpsertTelegramUserAfterDeleteCreatesFreshAccount(t *testing.T) {
	repo, db := twTombstoned(t)
	ctx := testCtx(t)
	before := twRaw(t, db, twUserID)

	fresh, err := repo.UpsertTelegramUser(ctx, twTgID, "zagir", "Загир", "ru")
	if err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if fresh.ID == twUserID {
		t.Fatalf("бот подобрал удалённый аккаунт %d вместо нового", fresh.ID)
	}
	if fresh.DisplayName != "Загир" || fresh.Username != "zagir" {
		t.Errorf("новый профиль заполнен неверно: %+v", fresh)
	}

	// два документа: tombstone остался на месте, рядом появился новый аккаунт
	twAssertClean(t, db, "UpsertTelegramUser", before, 2)
}
