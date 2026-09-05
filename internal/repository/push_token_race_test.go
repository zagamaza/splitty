package repository

import (
	"sync"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
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
