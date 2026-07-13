package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/golang-jwt/jwt/v5"
)

const testTgToken = "12345:test-telegram-token"

// signTelegramFields считает подпись Telegram Login Widget так, как это делает виджет
func signTelegramFields(fields map[string]string, tgToken string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+fields[k])
	}
	secret := sha256.Sum256([]byte(tgToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(pairs, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

// (е) валидация telegram-hash
func TestAuthTelegramHashValidation(t *testing.T) {
	userRepo := newFakeUserRepo()
	s := newTestServer(Config{TgToken: testTgToken}, userRepo, newFakeRoomRepo())

	authDate := time.Now().Unix()
	hash := signTelegramFields(map[string]string{
		"auth_date":  fmt.Sprintf("%d", authDate),
		"first_name": "Загир",
		"id":         "42",
		"username":   "zagir",
	}, testTgToken)

	body := fmt.Sprintf(`{"id": 42, "firstName": "Загир", "username": "zagir", "authDate": %d, "hash": "%s"}`, authDate, hash)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.Token == "" {
		t.Error("token is empty")
	}
	if resp.User.ID != 42 || resp.User.DisplayName != "Загир" {
		t.Errorf("unexpected user: %+v", resp.User)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != 42 {
		t.Errorf("parseToken = (%d, %v), want (42, nil)", userId, err)
	}
	if _, ok := userRepo.users[42]; !ok {
		t.Error("user was not upserted")
	}

	// токен работает для защищённых эндпоинтов
	rec = doRequest(t, s, http.MethodGet, "/api/v1/me", resp.Token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me with issued token status = %d, want 200", rec.Code)
	}
}

func TestAuthTelegramInvalidHash(t *testing.T) {
	s := newTestServer(Config{TgToken: testTgToken}, newFakeUserRepo(), newFakeRoomRepo())

	body := fmt.Sprintf(`{"id": 42, "firstName": "Загир", "authDate": %d, "hash": "%s"}`,
		time.Now().Unix(), strings.Repeat("ab", 32))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAuthTelegramStaleAuthDate(t *testing.T) {
	s := newTestServer(Config{TgToken: testTgToken}, newFakeUserRepo(), newFakeRoomRepo())

	// окно maxAuthAge — 10 минут, payload старше отвергается (защита от replay)
	authDate := time.Now().Add(-11 * time.Minute).Unix()
	hash := signTelegramFields(map[string]string{
		"auth_date":  fmt.Sprintf("%d", authDate),
		"first_name": "Загир",
		"id":         "42",
	}, testTgToken)
	body := fmt.Sprintf(`{"id": 42, "firstName": "Загир", "authDate": %d, "hash": "%s"}`, authDate, hash)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAuthTelegramWithoutToken(t *testing.T) {
	s := newTestServer(Config{TgToken: ""}, newFakeUserRepo(), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", `{"id": 42, "authDate": 1, "hash": "x"}`)
	assertErrorCode(t, rec, http.StatusServiceUnavailable, "unavailable")
}

// токен без exp отвергается: parseToken требует exp (jwt.WithExpirationRequired)
func TestTokenWithoutExpRejected(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())

	claims := jwt.RegisteredClaims{Subject: "1", IssuedAt: jwt.NewNumericDate(time.Now())}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JwtSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	rec := doRequest(t, s, http.MethodGet, "/api/v1/me", token, "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// тело больше 1 МБ → 413 (лимит действует и на неаутентифицированный /auth/telegram)
func TestRequestBodyTooLarge(t *testing.T) {
	s := newTestServer(Config{TgToken: testTgToken}, newFakeUserRepo(), newFakeRoomRepo())

	body := fmt.Sprintf(`{"id": 42, "authDate": 1, "hash": "%s"}`, strings.Repeat("a", maxRequestBodyBytes+1))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	assertErrorCode(t, rec, http.StatusRequestEntityTooLarge, "too_large")
}

// validLoginCode живой одноразовый код для userId
func validLoginCode(code string, userId int) api.LoginCode {
	return api.LoginCode{Code: code, UserId: userId, ExpiresAt: time.Now().Add(5 * time.Minute)}
}

// happy path: код из /login → токен в формате /auth/dev → /me работает;
// повторное использование того же кода → 401 invalid_code
func TestAuthCodeHappyPath(t *testing.T) {
	codeRepo := newFakeLoginCodeRepo(validLoginCode("ABCD2345", testUser1.ID))
	s := newTestServerWithLoginCodes(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(), codeRepo)

	// код регистронезависим: клиент шлёт в нижнем регистре
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "abcd2345"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.Token == "" {
		t.Error("token is empty")
	}
	if resp.User.ID != testUser1.ID || resp.User.DisplayName != testUser1.DisplayName {
		t.Errorf("unexpected user: %+v", resp.User)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != testUser1.ID {
		t.Errorf("parseToken = (%d, %v), want (%d, nil)", userId, err, testUser1.ID)
	}

	// токен работает для защищённых эндпоинтов
	rec = doRequest(t, s, http.MethodGet, "/api/v1/me", resp.Token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me with issued token status = %d, want 200", rec.Code)
	}

	// код одноразовый: повторное использование отвергается
	rec = doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "ABCD2345"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_code")
}

func TestAuthCodeInvalid(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "NOSUCH99"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_code")
}

func TestAuthCodeExpired(t *testing.T) {
	expired := api.LoginCode{Code: "ABCD2345", UserId: testUser1.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	s := newTestServerWithLoginCodes(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(), newFakeLoginCodeRepo(expired))

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "ABCD2345"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_code")
}

// код валиден, но пользователя нет в users → 401 invalid_code (без утечки деталей)
func TestAuthCodeUnknownUser(t *testing.T) {
	codeRepo := newFakeLoginCodeRepo(validLoginCode("ABCD2345", 777))
	s := newTestServerWithLoginCodes(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(), codeRepo)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "ABCD2345"}`)
	assertErrorCode(t, rec, http.StatusUnauthorized, "invalid_code")
}

func TestAuthCodeValidation(t *testing.T) {
	codeRepo := newFakeLoginCodeRepo(validLoginCode("ABCD2345", testUser1.ID))
	s := newTestServerWithLoginCodes(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(), codeRepo)

	// пустое тело → 400
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", "")
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	// пустой код → 400
	rec = doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": ""}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

func TestAuthDevDisabledNotFound(t *testing.T) {
	s := newTestServer(Config{DevAuth: false}, newFakeUserRepo(), newFakeRoomRepo())
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/dev", "", `{"userId": 7}`)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

func TestAuthDevEnabled(t *testing.T) {
	s := newTestServer(Config{DevAuth: true}, newFakeUserRepo(), newFakeRoomRepo())
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/dev", "", `{"userId": 7, "displayName": "Дев", "username": "dev"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID != 7 || resp.Token == "" {
		t.Errorf("unexpected auth response: %+v", resp)
	}
}


// Многоразовый код ревьюеров App Store: логинит в демо-аккаунт, не гаснет
// после использования; при пустом конфиге — выключен.
func TestAuthCodeReviewLogin(t *testing.T) {
	userRepo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{ReviewLoginCode: "APPLEREVIEW", ReviewUserId: testUser1.ID}, userRepo, newFakeRoomRepo())

	for i := 0; i < 2; i++ { // многоразовость
		rec := doRequest(t, s, "POST", "/api/v1/auth/code", "", `{"code":"applereview"}`)
		if rec.Code != 200 {
			t.Fatalf("попытка %d: status = %d, body %s", i, rec.Code, rec.Body.String())
		}
	}

	// без конфига тот же код невалиден
	s2 := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	rec := doRequest(t, s2, "POST", "/api/v1/auth/code", "", `{"code":"applereview"}`)
	if rec.Code != 401 {
		t.Fatalf("выключенный механизм должен давать 401, got %d", rec.Code)
	}
}
