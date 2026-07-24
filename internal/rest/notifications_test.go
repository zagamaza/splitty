package rest

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Дефолты без явных настроек: оба канала (telegram и push) включены для обеих
// категорий — push доедет только на устройство с зарегистрированным токеном.
func TestGetNotificationsDefaults(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, userRepo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/me/notifications", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var dto notifySettingsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if !dto.Operations.Telegram || !dto.Debts.Telegram {
		t.Fatalf("telegram-каналы по умолчанию включены, got %+v", dto)
	}
	if !dto.Operations.Push || !dto.Debts.Push {
		t.Fatalf("push по умолчанию включён, got %+v", dto)
	}
}

// PATCH меняет только присланные поля; ответ и последующий GET согласованы.
func TestPatchNotificationsPartial(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, userRepo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "PATCH", "/api/v1/me/notifications", token,
		`{"operations":{"telegram":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var dto notifySettingsDto
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Operations.Telegram {
		t.Fatalf("operations.telegram должен выключиться, got %+v", dto)
	}
	if !dto.Debts.Telegram {
		t.Fatalf("debts.telegram не трогали — должен остаться включённым, got %+v", dto)
	}

	rec = doRequest(t, s, "GET", "/api/v1/me/notifications", token, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Operations.Telegram || !dto.Debts.Telegram {
		t.Fatalf("GET после PATCH не согласован: %+v", dto)
	}
}

// Настройки уважаются хелперами, которыми пользуются бот и notifier.
func TestNotifySettingsRespectedByHelpers(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, userRepo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	doRequest(t, s, "PATCH", "/api/v1/me/notifications", token,
		`{"debts":{"telegram":false},"operations":{"push":true}}`)

	u := userRepo.users[testUser1.ID]
	if u.AllowsTelegram("debts") {
		t.Fatal("debts.telegram выключен — AllowsTelegram должен вернуть false")
	}
	if !u.AllowsTelegram("operations") {
		t.Fatal("operations.telegram не трогали — должен остаться true")
	}
	if !u.WantsPush("operations") {
		t.Fatal("operations.push включён — WantsPush должен вернуть true")
	}
}
