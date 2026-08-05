package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"golang.org/x/crypto/bcrypt"
)

// Тесты входа по email и паролю.

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(hash)
}

// passwordUser — аккаунт, заведённый регистрацией по почте
func passwordUser(t *testing.T, id int, email, password string) api.User {
	t.Helper()
	return api.User{ID: id, DisplayName: "Почтовый", LoginEmail: email, PasswordHash: mustHash(t, password)}
}

func parseAuthResponse(t *testing.T, rec *httptest.ResponseRecorder) authResponseDto {
	t.Helper()
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("не разобран ответ %q: %v", rec.Body.String(), err)
	}
	return resp
}

func postJSON(t *testing.T, s *Server, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodPost, path, token, body)
}

func TestRegisterCreatesAccount(t *testing.T) {
	users := newFakeUserRepo()
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	rec := postJSON(t, s, "/api/v1/auth/register", "",
		`{"email":"New@Example.com","password":"secret123","displayName":"Новый"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseAuthResponse(t, rec)
	if resp.Token == "" {
		t.Fatal("токен не выдан")
	}
	// номер из аллокатора: telegram id у такого аккаунта нет
	if resp.User.ID < 1_000_000_000_000 {
		t.Fatalf("_id = %d, ожидался синтетический номер (>= 10^12)", resp.User.ID)
	}
	if resp.User.DisplayName != "Новый" {
		t.Fatalf("имя не сохранено: %+v", resp.User)
	}
	assertProviders(t, resp.User.LinkedProviders, []string{providerPassword})

	stored := users.users[resp.User.ID]
	if stored.LoginEmail != "new@example.com" {
		t.Fatalf("адрес сохранён ненормализованным: %q", stored.LoginEmail)
	}
	if stored.PasswordHash == "" || stored.PasswordHash == "secret123" {
		t.Fatalf("пароль сохранён не хешем: %q", stored.PasswordHash)
	}
	// api.User.Email — best-effort поле профиля, адрес входа живёт отдельно
	if stored.Email != "" {
		t.Fatalf("адрес входа записан в api.User.Email: %q", stored.Email)
	}
	// ни пароль, ни хеш, ни адрес входа наружу не отдаются
	body := rec.Body.String()
	for _, secret := range []string{"secret123", stored.PasswordHash, "new@example.com"} {
		if strings.Contains(body, secret) {
			t.Fatalf("ответ содержит %q: %s", secret, body)
		}
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	users := newFakeUserRepo(passwordUser(t, 1_000_000_000_005, "taken@example.com", "secret123"))
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	// регистр адреса значения не имеет — это тот же аккаунт
	rec := postJSON(t, s, "/api/v1/auth/register", "",
		`{"email":"TAKEN@example.com","password":"secret123","displayName":"Второй"}`)
	assertErrorCode(t, rec, http.StatusConflict, "email_taken")
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"короткий пароль", `{"email":"a@b.com","password":"short12","displayName":"Имя"}`},
		{"невалидный email", `{"email":"not-an-email","password":"secret123","displayName":"Имя"}`},
		{"пустой email", `{"email":"","password":"secret123","displayName":"Имя"}`},
		{"пустое имя", `{"email":"a@b.com","password":"secret123","displayName":"  "}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			users := newFakeUserRepo()
			s := newTestServer(Config{}, users, newFakeRoomRepo())
			rec := postJSON(t, s, "/api/v1/auth/register", "", tc.body)
			assertErrorCode(t, rec, http.StatusBadRequest, "validation")
			if len(users.users) != 0 {
				t.Fatalf("аккаунт создан несмотря на ошибку валидации: %v", users.users)
			}
		})
	}
}

func TestPasswordLoginSuccess(t *testing.T) {
	users := newFakeUserRepo(passwordUser(t, 1_000_000_000_006, "user@example.com", "secret123"))
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	rec := postJSON(t, s, "/api/v1/auth/login", "", `{"email":" USER@Example.com ","password":"secret123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	resp := parseAuthResponse(t, rec)
	if resp.User.ID != 1_000_000_000_006 {
		t.Fatalf("вошли не в тот аккаунт: %+v", resp.User)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != 1_000_000_000_006 {
		t.Fatalf("токен невалиден: userId=%d err=%v", userId, err)
	}
}

// ГЛАВНОЕ требование входа: «нет такого пользователя» и «неверный пароль»
// обязаны быть неотличимы — иначе эндпоинт становится оракулом регистрации
func TestPasswordLoginFailuresIndistinguishable(t *testing.T) {
	users := newFakeUserRepo(passwordUser(t, 1_000_000_000_007, "known@example.com", "secret123"))
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	wrongPassword := postJSON(t, s, "/api/v1/auth/login", "",
		`{"email":"known@example.com","password":"wrong-password"}`)
	unknownEmail := postJSON(t, s, "/api/v1/auth/login", "",
		`{"email":"nobody@example.com","password":"secret123"}`)

	if wrongPassword.Code != unknownEmail.Code {
		t.Fatalf("статусы различаются: %d и %d", wrongPassword.Code, unknownEmail.Code)
	}
	if wrongPassword.Body.String() != unknownEmail.Body.String() {
		t.Fatalf("тела ответов различаются:\n%s\n%s", wrongPassword.Body.String(), unknownEmail.Body.String())
	}
	assertErrorCode(t, wrongPassword, http.StatusUnauthorized, "invalid_credentials")
}

// Аккаунт с адресом, но без хеша (пароль отвязан) войти не даёт — и отвечает так
// же, как на несуществующий адрес
func TestPasswordLoginWithoutHash(t *testing.T) {
	users := newFakeUserRepo(api.User{ID: 1_000_000_000_008, LoginEmail: "unlinked@example.com", GoogleSub: "g"})
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	rec := postJSON(t, s, "/api/v1/auth/login", "", `{"email":"unlinked@example.com","password":"secret123"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_credentials")
}

// Удалённый аккаунт по адресу не находится: вход отвечает как на неизвестный
func TestPasswordLoginDeletedAccount(t *testing.T) {
	deleted := passwordUser(t, 1_000_000_000_009, "gone@example.com", "secret123")
	users := newFakeUserRepo(deleted)
	s := newTestServer(Config{}, users, newFakeRoomRepo())
	if err := users.SoftDeleteUser(context.Background(), deleted.ID); err != nil {
		t.Fatalf("SoftDeleteUser: %v", err)
	}

	rec := postJSON(t, s, "/api/v1/auth/login", "", `{"email":"gone@example.com","password":"secret123"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_credentials")
}

func TestSetPasswordRequiresCurrentWhenSet(t *testing.T) {
	user := passwordUser(t, 1_000_000_000_010, "user@example.com", "old-secret")
	users := newFakeUserRepo(user)
	s := newTestServer(Config{}, users, newFakeRoomRepo())
	token := mustToken(t, s, user.ID)

	rec := postJSON(t, s, "/api/v1/me/password", token, `{"currentPassword":"not-the-one","newPassword":"new-secret"}`)
	assertErrorCode(t, rec, http.StatusForbidden, "invalid_password")

	rec = postJSON(t, s, "/api/v1/me/password", token, `{"newPassword":"new-secret"}`)
	assertErrorCode(t, rec, http.StatusForbidden, "invalid_password")

	rec = postJSON(t, s, "/api/v1/me/password", token, `{"currentPassword":"old-secret","newPassword":"new-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("смена пароля: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// старый пароль больше не пускает, новый пускает
	rec = postJSON(t, s, "/api/v1/auth/login", "", `{"email":"user@example.com","password":"old-secret"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_credentials")
	rec = postJSON(t, s, "/api/v1/auth/login", "", `{"email":"user@example.com","password":"new-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("вход с новым паролем: status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// Путь восстановления: пароль забыт, но есть другой способ входа. Пользователь
// отвязывает пароль и задаёт новый — адрес входа остаётся прежним
func TestSetPasswordWithoutCurrentAfterUnlink(t *testing.T) {
	user := passwordUser(t, 1_000_000_000_011, "user@example.com", "forgotten")
	user.GoogleSub = "gsub"
	users := newFakeUserRepo(user)
	s := newTestServer(Config{}, users, newFakeRoomRepo())
	token := mustToken(t, s, user.ID)

	rec := deleteLink(t, s, providerPassword, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("отвязка пароля: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerGoogle})

	// текущий пароль больше не требуется — его нет
	rec = postJSON(t, s, "/api/v1/me/password", token, `{"newPassword":"brand-new-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("установка пароля: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerGoogle, providerPassword})

	// вход по тому же адресу с новым паролем
	rec = postJSON(t, s, "/api/v1/auth/login", "", `{"email":"user@example.com","password":"brand-new-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("вход после восстановления: status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestSetPasswordValidation(t *testing.T) {
	user := withTelegram(api.User{ID: 1, Username: "tg"}, 42)
	users := newFakeUserRepo(user)
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	rec := postJSON(t, s, "/api/v1/me/password", mustToken(t, s, user.ID), `{"newPassword":"short12"}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	if users.users[user.ID].PasswordHash != "" {
		t.Fatal("короткий пароль сохранён")
	}
}

func TestSetPasswordUnauthorized(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(), newFakeRoomRepo())
	rec := postJSON(t, s, "/api/v1/me/password", "", `{"newPassword":"secret123"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// Хеш без адреса войти не даёт, поэтому способом входа не считается: иначе
// telegram-пользователь, задавший пароль, смог бы отвязать telegram и потерять
// доступ навсегда
func TestPasswordWithoutEmailIsNotLoginMethod(t *testing.T) {
	user := withTelegram(api.User{ID: 1, Username: "tg", DisplayName: "Телеграмный"}, 42)
	users := newFakeUserRepo(user)
	s := newTestServer(Config{}, users, newFakeRoomRepo())
	token := mustToken(t, s, user.ID)

	rec := postJSON(t, s, "/api/v1/me/password", token, `{"newPassword":"secret123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("установка пароля: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerTelegram})

	// telegram остаётся единственным способом входа — отвязать его нельзя
	assertErrorCode(t, deleteLink(t, s, providerTelegram, token), http.StatusConflict, "last_identity")
}

// Пароль как единственный способ входа отвязать нельзя: аккаунт остался бы без
// единой двери, а восстановить его неоткуда
func TestUnlinkOnlyPasswordRefused(t *testing.T) {
	user := passwordUser(t, 1_000_000_000_012, "solo@example.com", "secret123")
	users := newFakeUserRepo(user)
	s := newTestServer(Config{}, users, newFakeRoomRepo())

	rec := deleteLink(t, s, providerPassword, mustToken(t, s, user.ID))
	assertErrorCode(t, rec, http.StatusConflict, "last_identity")
	if users.users[user.ID].PasswordHash == "" {
		t.Fatal("хеш вычищен несмотря на отказ")
	}
}

// linkedProviders отражает реальность: пароль появляется, только когда войти им
// действительно можно
func TestLinkedProvidersReflectPassword(t *testing.T) {
	tests := []struct {
		name string
		user api.User
		want []string
	}{
		{"почта и хеш", passwordUser(t, 1_000_000_000_013, "a@b.com", "secret123"), []string{providerPassword}},
		{
			"почта без хеша",
			api.User{ID: 1_000_000_000_014, LoginEmail: "a@b.com", GoogleSub: "g"},
			[]string{providerGoogle},
		},
		{
			"хеш без почты",
			api.User{ID: 1_000_000_000_015, PasswordHash: mustHash(t, "secret123"), GoogleSub: "g"},
			[]string{providerGoogle},
		},
		{
			"пароль вместе с другими",
			withTelegram(api.User{
				ID: 1_000_000_000_016, LoginEmail: "a@b.com", PasswordHash: mustHash(t, "secret123"), GoogleSub: "g",
			}, 7),
			[]string{providerTelegram, providerGoogle, providerPassword},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(Config{}, newFakeUserRepo(tc.user), newFakeRoomRepo())
			rec := doRequest(t, s, http.MethodGet, "/api/v1/me", mustToken(t, s, tc.user.ID), "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /me: status = %d, body: %s", rec.Code, rec.Body.String())
			}
			var me meDto
			if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
				t.Fatalf("не разобран ответ: %v", err)
			}
			assertProviders(t, me.LinkedProviders, tc.want)
		})
	}
}

// После удаления аккаунта адрес свободен: tombstone его не держит
func TestRegisterAfterDeleteMe(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	user := passwordUser(t, 1_000_000_000_017, "reuse@example.com", "secret123")
	d.users.users[user.ID] = &user

	if rec := d.deleteMe(t, user.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	rec := postJSON(t, d.s, "/api/v1/auth/register", "",
		`{"email":"reuse@example.com","password":"secret123","displayName":"Заново"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("повторная регистрация: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if resp := parseAuthResponse(t, rec); resp.User.ID == user.ID {
		t.Fatal("повторная регистрация воскресила удалённый аккаунт")
	}
}

// POST /me/link/password не существует: пароль задаётся через /me/password
func TestLinkPasswordProviderNotFound(t *testing.T) {
	user := withTelegram(api.User{ID: 1}, 42)
	s := newTestServer(Config{}, newFakeUserRepo(user), newFakeRoomRepo())

	rec := postJSON(t, s, "/api/v1/me/link/password", mustToken(t, s, user.ID), `{}`)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}
