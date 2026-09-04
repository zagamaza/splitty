package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
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

// TestPatchNotificationsOldClientKeepsInvites — старая сборка присылает тело
// БЕЗ поля invites (категория появилась позже). Такой запрос обязан сохранить
// категорию, а не обнулить: иначе после выкатки бэкенда любой человек, тронувший
// настройки из старого клиента, молча перестал бы получать приглашения.
func TestPatchNotificationsOldClientKeepsInvites(t *testing.T) {
	users := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, users, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	// Человек выключил приглашения из новой сборки.
	rec := doRequest(t, s, http.MethodPatch, "/api/v1/me/notifications", token,
		`{"invites":{"telegram":false,"push":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("подготовка: ожидался 200, получен %d", rec.Code)
	}

	// Затем старая сборка меняет только расходы.
	rec = doRequest(t, s, http.MethodPatch, "/api/v1/me/notifications", token,
		`{"operations":{"push":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("старый клиент: ожидался 200, получен %d", rec.Code)
	}

	var got notifySettingsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}
	if got.Invites.Telegram || got.Invites.Push {
		t.Fatalf("старый клиент включил обратно выключенные приглашения: %+v", got.Invites)
	}
	if got.Operations.Push {
		t.Fatal("изменение из старого клиента не применилось")
	}
}

// TestNotifyInvitesDefaultsOn — приглашения по умолчанию включены: человек,
// никогда не открывавший настройки, обязан узнать, что его позвали.
func TestNotifyInvitesDefaultsOn(t *testing.T) {
	u := testUser1
	if !u.AllowsTelegram(api.NotifyInvites) || !u.WantsPush(api.NotifyInvites) {
		t.Fatal("приглашения должны быть включены по умолчанию")
	}

	off := false
	u.NotificationOn = &off
	if u.AllowsTelegram(api.NotifyInvites) || u.WantsPush(api.NotifyInvites) {
		t.Fatal("мастер-выключатель обязан гасить и приглашения")
	}
}

// Категория «правки» приезжает выключенной по push и включённой по telegram.
// Проверяем через HTTP, а не только на модели: стартовая матрица в
// handlePatchNotifications фиксирует ЭФФЕКТИВНЫЕ значения, и если бы она брала
// общий дефолт «включено», первое же сохранение настроек из приложения
// незаметно включило бы человеку пуши на каждое переименование.
func TestEditsCategoryDefaults(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, userRepo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/me/notifications", token, "")
	var dto notifySettingsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if dto.Edits.Push {
		t.Errorf("edits.push должен быть выключен по умолчанию, got %+v", dto.Edits)
	}
	if !dto.Edits.Telegram {
		t.Errorf("edits.telegram должен остаться включённым, got %+v", dto.Edits)
	}

	// Правка соседней категории не включает пуши правок задним числом.
	rec = doRequest(t, s, "PATCH", "/api/v1/me/notifications", token, `{"debts":{"push":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Edits.Push {
		t.Errorf("сохранение других настроек не должно включать edits.push, got %+v", dto.Edits)
	}
}

// Включение пушей правок сохраняется и не задевает соседей.
func TestPatchEditsPush(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, userRepo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "PATCH", "/api/v1/me/notifications", token, `{"edits":{"push":true}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var dto notifySettingsDto
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if !dto.Edits.Push {
		t.Fatalf("edits.push должен включиться, got %+v", dto)
	}
	if !dto.Operations.Push || !dto.Debts.Push {
		t.Fatalf("соседние категории не трогали, got %+v", dto)
	}

	rec = doRequest(t, s, "GET", "/api/v1/me/notifications", token, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if !dto.Edits.Push {
		t.Fatalf("GET после PATCH не согласован: %+v", dto)
	}
}
