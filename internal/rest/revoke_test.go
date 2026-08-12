package rest

import (
	"net/http"
	"testing"
	"time"
)

// Отзыв токенов: «выйти на всех устройствах».
//
// Токен живёт 90 дней и до сих пор не отзывался ничем, кроме смены общего
// секрета — то есть разлогина ВСЕХ. Украденный телефон означал три месяца
// доступа к чужим расходам, и обновление приложения этого не отменяло.

func TestRevokeTokensInvalidatesOldTokens(t *testing.T) {
	users := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, users, newFakeRoomRepo(newTestRoom()))
	old := mustTokenIssuedAt(t, s, testUser1.ID, time.Now().Add(-time.Hour))

	// До отзыва токен работает
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/me", old, ""); rec.Code != http.StatusOK {
		t.Fatalf("подготовка: status = %d, want 200", rec.Code)
	}

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/me/revoke-tokens", old, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("отзыв: status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, s, http.MethodGet, "/api/v1/me", old, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — украденный токен продолжает открывать чужие расходы", rec.Code)
	}
}

// Новый вход после отзыва обязан работать: иначе «выйти на всех устройствах»
// означало бы потерю аккаунта.
func TestTokenIssuedAfterRevokeWorks(t *testing.T) {
	users := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, users, newFakeRoomRepo(newTestRoom()))
	old := mustTokenIssuedAt(t, s, testUser1.ID, time.Now().Add(-time.Hour))

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/me/revoke-tokens", old, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("отзыв: status = %d", rec.Code)
	}

	fresh := mustTokenIssuedAt(t, s, testUser1.ID, time.Now().Add(time.Minute))
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/me", fresh, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — после отзыва нельзя войти заново; body: %s", rec.Code, rec.Body.String())
	}
}

// Пуши на потерянное устройство уходить не должны: токены устройств снимаются
// той же записью.
func TestRevokeTokensDropsPushTokens(t *testing.T) {
	users := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, users, newFakeRoomRepo(newTestRoom()))
	token := mustTokenIssuedAt(t, s, testUser1.ID, time.Now().Add(-time.Hour))

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/me/devices", token, `{"token":"fcm-1","platform":"ios"}`); rec.Code >= 400 {
		t.Fatalf("подготовка: регистрация устройства дала %d", rec.Code)
	}
	if got := users.users[testUser1.ID].PushTokens; len(got) == 0 {
		t.Fatal("подготовка не удалась: push-токен не записан")
	}

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/me/revoke-tokens", token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("отзыв: status = %d", rec.Code)
	}
	if got := users.users[testUser1.ID].PushTokens; len(got) != 0 {
		t.Fatalf("push-токенов осталось %d — уведомления продолжат приходить на потерянный телефон", len(got))
	}
}

// Пользователь без отсечки работает как раньше: установленные сборки не
// должны разлогиниться из-за появления поля.
func TestUserWithoutRevokeMarkerKeepsWorking(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	old := mustTokenIssuedAt(t, s, testUser1.ID, time.Now().Add(-90*24*time.Hour))

	if rec := doRequest(t, s, http.MethodGet, "/api/v1/me", old, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — токен без отзыва перестал работать", rec.Code)
	}
}
