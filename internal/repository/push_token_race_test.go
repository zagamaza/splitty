package repository

import (
	"reflect"
	"sync"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// pushTokens читает токены пользователя напрямую из базы.
func pushTokens(t *testing.T, db *mongo.Database, userID int) []api.PushToken {
	t.Helper()
	var user api.User
	if err := db.Collection("user").FindOne(testCtx(t), map[string]interface{}{"_id": userID}).Decode(&user); err != nil {
		t.Fatalf("не прочитать пользователя: %v", err)
	}
	return user.PushTokens
}

// Один токен — одно устройство — одна запись, сколько бы регистраций ни пришло
// разом. Вход и колбэк FCM дёргают регистрацию одновременно, клиентский дедуп
// пропускает обе (ни одна ещё не ответила), и прежняя пара «$pull, потом
// $push» успевала сделать pull-pull-push-push: токен оставался дважды. С
// языками это стало видно — один телефон получал два пуша на разных языках.
func TestAddPushTokenIsRaceSafe(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	seedUsers(t, db, api.User{ID: 700, Username: "device", DisplayName: "Device"})

	const parallel = 8
	var wg sync.WaitGroup
	errs := make([]error, parallel)
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			locale := "ru"
			if i%2 == 1 {
				locale = "en"
			}
			errs[i] = repo.AddPushToken(testCtx(t), 700, api.PushToken{
				Token: "fcm-one", Platform: "ios", Locale: locale,
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("регистрация %d: %v", i, err)
		}
	}

	tokens := pushTokens(t, db, 700)
	if len(tokens) != 1 {
		t.Fatalf("токенов %d, ожидался один: %+v", len(tokens), tokens)
	}
	if tokens[0].Locale != "ru" && tokens[0].Locale != "en" {
		t.Errorf("язык токена %q — не от одной из регистраций", tokens[0].Locale)
	}
}

// Повторная регистрация того же токена меняет язык, а не плодит запись: так
// человек, сменивший язык устройства, получает пуши на новом.
func TestAddPushTokenUpdatesLocaleInPlace(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	seedUsers(t, db, api.User{ID: 701, Username: "device", DisplayName: "Device"})

	if err := repo.AddPushToken(ctx, 701, api.PushToken{Token: "fcm-a", Platform: "ios", Locale: "ru"}); err != nil {
		t.Fatalf("первая регистрация: %v", err)
	}
	if err := repo.AddPushToken(ctx, 701, api.PushToken{Token: "fcm-a", Platform: "ios", Locale: "ja"}); err != nil {
		t.Fatalf("повторная регистрация: %v", err)
	}

	tokens := pushTokens(t, db, 701)
	if len(tokens) != 1 {
		t.Fatalf("токенов %d, ожидался один: %+v", len(tokens), tokens)
	}
	if tokens[0].Locale != "ja" {
		t.Errorf("язык %q, ожидался ja: смена языка устройства не доехала", tokens[0].Locale)
	}
}

// Разные токены живут рядом: у человека может быть телефон и планшет.
func TestAddPushTokenKeepsOtherDevices(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	seedUsers(t, db, api.User{ID: 702, Username: "device", DisplayName: "Device"})

	for _, tok := range []api.PushToken{
		{Token: "phone", Platform: "ios", Locale: "ru"},
		{Token: "tablet", Platform: "ios", Locale: "en"},
	} {
		if err := repo.AddPushToken(ctx, 702, tok); err != nil {
			t.Fatalf("регистрация %s: %v", tok.Token, err)
		}
	}
	if got := pushTokens(t, db, 702); len(got) != 2 {
		t.Fatalf("токенов %d, ожидалось два: %+v", len(got), got)
	}
}

// Дубли, накопленные прежней неатомарной регистрацией, чинит миграция:
// сам по себе AddPushToken их не тронет — он правит совпавшие записи на месте,
// а их семь. Устройство получало по пушу на каждую копию.
func TestDedupePushTokensCollapsesExistingDuplicates(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	seedUsers(t, db,
		api.User{ID: 710, Username: "dup", DisplayName: "Dup", PushTokens: []api.PushToken{
			{Token: "phone", Platform: "ios", Locale: "ru"},
			{Token: "tablet", Platform: "ios", Locale: "en"},
			{Token: "phone", Platform: "ios", Locale: "ru"},
			{Token: "phone", Platform: "ios", Locale: "ja"}, // последняя — самая свежая
		}},
		api.User{ID: 711, Username: "clean", DisplayName: "Clean", PushTokens: []api.PushToken{
			{Token: "one", Platform: "android", Locale: "de"},
			{Token: "two", Platform: "android", Locale: "de"},
		}},
	)

	if _, err := DedupePushTokens(ctx, db); err != nil {
		t.Fatalf("миграция: %v", err)
	}

	got := pushTokens(t, db, 710)
	if len(got) != 2 {
		t.Fatalf("токенов %d, ожидалось два: %+v", len(got), got)
	}
	byToken := map[string]api.PushToken{}
	for _, tok := range got {
		byToken[tok.Token] = tok
	}
	if byToken["phone"].Locale != "ja" {
		t.Errorf("у phone язык %q, ожидался ja: оставаться должна ПОСЛЕДНЯЯ копия", byToken["phone"].Locale)
	}
	if _, ok := byToken["tablet"]; !ok {
		t.Error("второе устройство потеряно")
	}

	// Документ без дублей миграция не трогает. Сверяем массив целиком, а не
	// длину: фильтр берёт ЛЮБОГО пользователя с двумя токенами, и порядок с
	// содержимым обязаны остаться прежними.
	wantClean := []api.PushToken{
		{Token: "one", Platform: "android", Locale: "de"},
		{Token: "two", Platform: "android", Locale: "de"},
	}
	if clean := pushTokens(t, db, 711); !reflect.DeepEqual(clean, wantClean) {
		t.Errorf("чистый пользователь изменился: %+v, было %+v", clean, wantClean)
	}

	// Маркер записан: второй запуск к коллекции user не идёт.
	modified, err := DedupePushTokens(ctx, db)
	if err != nil {
		t.Fatalf("повторный запуск: %v", err)
	}
	if modified != 0 {
		t.Errorf("повторный запуск тронул %d документов — маркер не сработал", modified)
	}

	// А теперь без маркера: восстановление базы из старого дампа сносит
	// коллекцию migration, и pipeline проходит по уже вычищенным документам.
	// Он обязан быть идемпотентным сам по себе, а не только под маркером.
	before710, before711 := pushTokens(t, db, 710), pushTokens(t, db, 711)
	if _, err := db.Collection(migrationCollection).DeleteOne(ctx, bson.M{"_id": dedupePushTokensMarker}); err != nil {
		t.Fatalf("не удалить маркер: %v", err)
	}
	if _, err := DedupePushTokens(ctx, db); err != nil {
		t.Fatalf("прогон без маркера: %v", err)
	}
	if after := pushTokens(t, db, 710); !reflect.DeepEqual(after, before710) {
		t.Errorf("повтор без маркера изменил массив: %+v, было %+v", after, before710)
	}
	if after := pushTokens(t, db, 711); !reflect.DeepEqual(after, before711) {
		t.Errorf("повтор без маркера тронул чистого: %+v, было %+v", after, before711)
	}
}

// Телефон сменил владельца: Маша вышла, но запрос на отвязку не дошёл, и её
// запись осталась. Регистрация Пети обязана забрать токен себе — иначе Маша
// продолжит слать названия своих групп на чужой экран, и само это не пройдёт:
// телефон живой, доставка успешна, отбраковки не будет.
func TestAddPushTokenTakesDeviceFromPreviousOwner(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)
	seedUsers(t, db,
		api.User{ID: 720, Username: "masha", DisplayName: "Маша", PushTokens: []api.PushToken{
			{Token: "shared-phone", Platform: "ios", Locale: "ru"},
			{Token: "her-tablet", Platform: "ios", Locale: "ru"},
		}},
		api.User{ID: 721, Username: "petya", DisplayName: "Петя"},
	)

	if err := repo.AddPushToken(ctx, 721, api.PushToken{Token: "shared-phone", Platform: "ios", Locale: "en"}); err != nil {
		t.Fatalf("регистрация нового владельца: %v", err)
	}

	previous := pushTokens(t, db, 720)
	for _, tok := range previous {
		if tok.Token == "shared-phone" {
			t.Error("токен остался у прежнего аккаунта — его пуши уйдут чужому человеку")
		}
	}
	if len(previous) != 1 || previous[0].Token != "her-tablet" {
		t.Errorf("у прежнего владельца пострадали другие устройства: %+v", previous)
	}

	current := pushTokens(t, db, 721)
	if len(current) != 1 || current[0].Token != "shared-phone" || current[0].Locale != "en" {
		t.Errorf("новый владелец не получил устройство: %+v", current)
	}
}
