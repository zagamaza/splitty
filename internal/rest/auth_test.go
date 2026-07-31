package rest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

// TestAuthTelegramResolvesByTelegramID: вход через Login Widget обязан резолвить
// личность по telegram_id, а sub токена — брать из _id НАЙДЕННОГО пользователя.
// У аккаунта, пришедшего через Google и привязавшего telegram, _id синтетический,
// а req.Id — telegram id; резолв по _id завёл бы ему второй профиль
func TestAuthTelegramResolvesByTelegramID(t *testing.T) {
	const (
		tgID       = 42
		canonical  = 1_000_000_000_900
		wantedName = "Загир"
	)
	tg := tgID
	userRepo := newFakeUserRepo(api.User{
		ID: canonical, Username: "zagir", DisplayName: wantedName,
		GoogleSub: "sub-1", TelegramID: &tg, UserLang: "ru",
	})
	s := newTestServer(Config{TgToken: testTgToken}, userRepo, newFakeRoomRepo())

	authDate := time.Now().Unix()
	hash := signTelegramFields(map[string]string{
		"auth_date":  fmt.Sprintf("%d", authDate),
		"first_name": wantedName,
		"id":         fmt.Sprintf("%d", tgID),
		"username":   "zagir",
	}, testTgToken)
	body := fmt.Sprintf(`{"id": %d, "firstName": %q, "username": "zagir", "authDate": %d, "hash": "%s"}`,
		tgID, wantedName, authDate, hash)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID != canonical {
		t.Errorf("resp.User.ID = %d, want %d (номер Splitty, а не telegram id)", resp.User.ID, canonical)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != canonical {
		t.Errorf("parseToken = (%d, %v), want (%d, nil)", userId, err, canonical)
	}
	if _, ok := userRepo.users[tgID]; ok {
		t.Errorf("заведён второй профиль под номером telegram id %d", tgID)
	}
	if len(userRepo.users) != 1 {
		t.Errorf("в базе %d пользователей, ожидался 1", len(userRepo.users))
	}
	// язык, выбранный пользователем, вход через виджет не затирает
	if userRepo.users[canonical].UserLang != "ru" {
		t.Errorf("user_lang = %q, want ru", userRepo.users[canonical].UserLang)
	}
}

// TestAuthTelegramCreatesUserWithTelegramID: новому пользователю проставляется
// telegram_id — без него ни бот, ни аватары его больше не найдут
func TestAuthTelegramCreatesUserWithTelegramID(t *testing.T) {
	userRepo := newFakeUserRepo()
	s := newTestServer(Config{TgToken: testTgToken}, userRepo, newFakeRoomRepo())

	authDate := time.Now().Unix()
	hash := signTelegramFields(map[string]string{
		"auth_date": fmt.Sprintf("%d", authDate),
		"id":        "77",
	}, testTgToken)
	body := fmt.Sprintf(`{"id": 77, "authDate": %d, "hash": "%s"}`, authDate, hash)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/telegram", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	created, ok := userRepo.users[77]
	if !ok {
		t.Fatal("пользователь не создан")
	}
	if created.TelegramID == nil || *created.TelegramID != 77 {
		t.Fatalf("telegram_id = %v, want 77", created.TelegramID)
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

// TestAuthCodeTelegramUserSeesOwnRooms — приёмочный критерий Task 22:
// telegram-пользователь входит по коду из бота и по выданному токену получает
// ИМЕННО свои комнаты. TestAuthCodeHappyPath доходит только до /me, а здесь
// важно, что `sub` токена — номер Splitty, по которому ищутся комнаты: после
// Task 6 у telegram-пользователя `_id` и `telegram_id` уже разные величины, и
// подстановка не того числа дала бы пустой список молча, без ошибки.
func TestAuthCodeTelegramUserSeesOwnRooms(t *testing.T) {
	tgID := 555_666_777
	tgUser := api.User{
		ID: 1_000_000_000_100, TelegramID: &tgID,
		Username: "tg", DisplayName: "Телеграмщик", UserLang: "ru",
	}

	myRoom := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Моя комната",
		Members:    &[]api.User{tgUser, testUser2},
		Operations: &[]api.Operation{},
		CreateAt:   time.Now(),
	}
	foreignRoom := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Чужая комната",
		Members:    &[]api.User{testUser2, testUser3},
		Operations: &[]api.Operation{},
		CreateAt:   time.Now(),
	}

	codeRepo := newFakeLoginCodeRepo(validLoginCode("TGCODE12", tgUser.ID))
	s := newTestServerWithLoginCodes(Config{}, newFakeUserRepo(tgUser, testUser2, testUser3),
		newFakeRoomRepo(myRoom, foreignRoom), codeRepo)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code": "TGCODE12"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/code status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var auth authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &auth); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if auth.User.ID != tgUser.ID {
		t.Fatalf("auth вернул пользователя %d, want %d", auth.User.ID, tgUser.ID)
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms", auth.Token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /rooms status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var rooms []roomSummaryDto
	if err := json.Unmarshal(rec.Body.Bytes(), &rooms); err != nil {
		t.Fatalf("cannot parse rooms %q: %v", rec.Body.String(), err)
	}
	if len(rooms) != 1 {
		t.Fatalf("получено %d комнат, want 1: %s", len(rooms), rec.Body.String())
	}
	if rooms[0].ID != myRoom.ID.Hex() {
		t.Fatalf("отдана комната %q (%s), want %q", rooms[0].Name, rooms[0].ID, myRoom.ID.Hex())
	}
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

// --- вход через Google: POST /api/v1/auth/google ---

const testGoogleToken = "valid-google-id-token"

// newGoogleServer сервер с фейковым верификатором, знающим testGoogleToken
func newGoogleServer(userRepo *fakeUserRepo, sub, email, name string) (*Server, *fakeOIDCVerifier) {
	v := newFakeVerifier().with(testGoogleToken, sub, email, name)
	return newTestServer(Config{GoogleVerifier: v}, userRepo, newFakeRoomRepo()), v
}

func postGoogle(t *testing.T, s *Server, idToken string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodPost, "/api/v1/auth/google", "", fmt.Sprintf(`{"idToken": %q}`, idToken))
}

// Новый пользователь получает СИНТЕТИЧЕСКИЙ номер из аллокатора и не имеет
// telegram_id: номер telegram ему взять неоткуда, а _id, равный чему-то
// маленькому, столкнулся бы с историческими telegram-аккаунтами
func TestAuthGoogleCreatesUserWithSyntheticID(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newGoogleServer(userRepo, "google-sub-1", "user@example.com", "Загир Нурмухаметов")

	rec := postGoogle(t, s, testGoogleToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID < 1_000_000_000_000 {
		t.Fatalf("_id = %d, ожидался синтетический номер ≥ 10^12", resp.User.ID)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != resp.User.ID {
		t.Errorf("parseToken = (%d, %v), want (%d, nil)", userId, err, resp.User.ID)
	}
	created, ok := userRepo.users[resp.User.ID]
	if !ok {
		t.Fatalf("пользователь %d не создан", resp.User.ID)
	}
	if created.GoogleSub != "google-sub-1" {
		t.Errorf("google_sub = %q, want google-sub-1", created.GoogleSub)
	}
	if created.Email != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", created.Email)
	}
	if created.DisplayName != "Загир Нурмухаметов" {
		t.Errorf("display_name = %q, want имя из токена", created.DisplayName)
	}
	if created.TelegramID != nil {
		t.Errorf("telegram_id = %v, у google-пользователя его быть не должно", *created.TelegramID)
	}
	// поля личности наружу не отдаются
	if strings.Contains(rec.Body.String(), "google-sub-1") || strings.Contains(rec.Body.String(), "user@example.com") {
		t.Errorf("ответ содержит поля личности: %s", rec.Body.String())
	}
}

// Повторный вход находит того же пользователя по google_sub — дубля нет,
// новый номер у аллокатора не берётся
func TestAuthGoogleSecondLoginReusesAccount(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newGoogleServer(userRepo, "google-sub-1", "user@example.com", "Загир")

	first := postGoogle(t, s, testGoogleToken)
	second := postGoogle(t, s, testGoogleToken)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("статусы = %d и %d, want 200 и 200", first.Code, second.Code)
	}
	var r1, r2 authResponseDto
	if err := json.Unmarshal(first.Body.Bytes(), &r1); err != nil {
		t.Fatalf("cannot parse first response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &r2); err != nil {
		t.Fatalf("cannot parse second response: %v", err)
	}
	if r1.User.ID != r2.User.ID {
		t.Errorf("повторный вход дал другой _id: %d и %d", r1.User.ID, r2.User.ID)
	}
	if len(userRepo.users) != 1 {
		t.Errorf("в базе %d пользователей, ожидался 1", len(userRepo.users))
	}
}

// Аккаунты по email НЕ склеиваются: почта не идентификатор (её меняют, Apple
// отдаёт relay-адрес). Совпадение email с существующим аккаунтом обязано дать
// НОВЫЙ профиль, а не вход в чужой
func TestAuthGoogleDoesNotMergeByEmail(t *testing.T) {
	existing := api.User{ID: 500, Username: "zagir", DisplayName: "Загир", Email: "user@example.com"}
	userRepo := newFakeUserRepo(existing)
	s, _ := newGoogleServer(userRepo, "google-sub-1", "user@example.com", "Загир")

	rec := postGoogle(t, s, testGoogleToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID == existing.ID {
		t.Fatalf("вход по совпавшему email пустил в чужой аккаунт %d", existing.ID)
	}
	if userRepo.users[existing.ID].GoogleSub != "" {
		t.Errorf("существующему аккаунту молча привязали google_sub")
	}
}

// Невалидный токен — 401 без объяснения причины: подпись, издатель, aud и срок
// это подсказки тому, кто подбирает токен
func TestAuthGoogleInvalidToken(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newGoogleServer(userRepo, "google-sub-1", "user@example.com", "Загир")

	rec := postGoogle(t, s, "forged-token")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	if strings.Contains(rec.Body.String(), "подпись не проверена") {
		t.Errorf("причина отказа утекла клиенту: %s", rec.Body.String())
	}
	if len(userRepo.users) != 0 {
		t.Errorf("по невалидному токену создан пользователь")
	}
}

// Без GOOGLE_CLIENT_IDS верификатор nil: честный 503, а не 401 — клиенту важно
// отличить «не настроено» от «вас не пускают»
func TestAuthGoogleNotConfigured(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(), newFakeRoomRepo())
	rec := postGoogle(t, s, testGoogleToken)
	assertErrorCode(t, rec, http.StatusServiceUnavailable, "unavailable")
}

func TestAuthGoogleValidation(t *testing.T) {
	s, v := newGoogleServer(newFakeUserRepo(), "google-sub-1", "", "")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/google", "", `{}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	rec = doRequest(t, s, http.MethodPost, "/api/v1/auth/google", "", `{"idToken": "   "}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	if v.calls != 0 {
		t.Errorf("пустой токен ушёл в верификатор %d раз(а)", v.calls)
	}
}

// Троттлинг per-IP работает и имеет СВОЙ префикс ключа: бюджет входа через
// Google не должен пересекаться с бюджетом /auth/code с того же адреса
func TestAuthGoogleThrottled(t *testing.T) {
	s, _ := newGoogleServer(newFakeUserRepo(), "google-sub-1", "", "")

	for i := 0; i < oauthAttemptsPerMin; i++ {
		if rec := postGoogle(t, s, "forged-token"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: status = %d, want 401", i, rec.Code)
		}
	}
	rec := postGoogle(t, s, "forged-token")
	assertErrorCode(t, rec, http.StatusTooManyRequests, "rate_limited")

	// бюджет входа по коду с того же адреса не тронут
	codeRec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code":"ABCDEF"}`)
	assertErrorCode(t, codeRec, http.StatusUnauthorized, "invalid_code")
}

// racingUserRepo имитирует проигрыш гонки двух ПЕРВЫХ входов одного человека:
// пока проигравший ходил в аллокатор, победитель вставил документ с тем же
// google_sub, и вставка проигравшего отбивается duplicate key
type racingUserRepo struct {
	*fakeUserRepo
	winner  api.User
	raced   bool
	creates int
}

func (r *racingUserRepo) CreateIdentityUser(ctx context.Context, u api.User) error {
	r.creates++
	if !r.raced {
		r.raced = true
		winner := r.winner
		r.fakeUserRepo.users[winner.ID] = &winner
		return errDuplicateKey
	}
	return r.fakeUserRepo.CreateIdentityUser(ctx, u)
}

// Retry на duplicate key ОБЯЗАН начинаться с повторного FindByGoogleSub:
// проигравший гонку подбирает документ победителя. Слепой retry «взять новый
// номер и вставить снова» упёрся бы в unique-индекс по google_sub все три раза
func TestAuthGoogleDuplicateKeyPicksUpWinner(t *testing.T) {
	const winnerID = 1_000_000_000_777
	base := newFakeUserRepo()
	repo := &racingUserRepo{
		fakeUserRepo: base,
		winner:       api.User{ID: winnerID, GoogleSub: "google-sub-1", DisplayName: "Загир"},
	}
	v := newFakeVerifier().with(testGoogleToken, "google-sub-1", "user@example.com", "Загир")
	roomRepo := newFakeRoomRepo()
	s := NewServer(Config{JwtSecret: "test-secret", GoogleVerifier: v}, repo, roomRepo, newFakeLoginCodeRepo(),
		service.NewRoomService(roomRepo), service.NewOperationService(roomRepo), newFakeUserIDAllocator())

	rec := postGoogle(t, s, testGoogleToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID != winnerID {
		t.Errorf("resp.User.ID = %d, want %d (документ победителя гонки)", resp.User.ID, winnerID)
	}
	if repo.creates != 1 {
		t.Errorf("CreateIdentityUser вызван %d раз(а), ожидался 1: повторная вставка упёрлась бы в unique-индекс", repo.creates)
	}
	if len(base.users) != 1 {
		t.Errorf("в базе %d пользователей, ожидался 1", len(base.users))
	}
}

// --- вход через Apple: POST /api/v1/auth/apple ---

const (
	testAppleToken = "valid-apple-id-token"
	testAppleNonce = "raw-nonce-from-client"
)

// appleNonceHash — то, что Apple кладёт в claim nonce: hex(sha256(сырой nonce))
func appleNonceHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newAppleServer сервер с фейковым верификатором, знающим testAppleToken с
// правильным хешем nonce
func newAppleServer(userRepo *fakeUserRepo, sub, email string) (*Server, *fakeOIDCVerifier) {
	v := newFakeVerifier().with(testAppleToken, sub, email, "").withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	return newTestServer(Config{AppleVerifier: v}, userRepo, newFakeRoomRepo()), v
}

// postApple шлёт вход через Apple. displayName приходит отдельным полем: в
// токене имени нет
func postApple(t *testing.T, s *Server, idToken, displayName, nonce, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"idToken": %q, "displayName": %q, "nonce": %q, "authorizationCode": %q}`,
		idToken, displayName, nonce, code)
	return doRequest(t, s, http.MethodPost, "/api/v1/auth/apple", "", body)
}

func appleLogin(t *testing.T, s *Server, displayName string) *httptest.ResponseRecorder {
	t.Helper()
	return postApple(t, s, testAppleToken, displayName, testAppleNonce, "")
}

// Первый вход: синтетический номер, apple_sub, email из токена и имя из тела
// запроса. Больше Apple ни email, ни имя не пришлёт — сохранить их можно только
// сейчас
func TestAuthAppleCreatesUserWithEmailAndName(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newAppleServer(userRepo, "apple-sub-1", "abc123@privaterelay.appleid.com")

	rec := appleLogin(t, s, "Загир Нурмухаметов")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID < 1_000_000_000_000 {
		t.Fatalf("_id = %d, ожидался синтетический номер ≥ 10^12", resp.User.ID)
	}
	if userId, err := s.parseToken(resp.Token); err != nil || userId != resp.User.ID {
		t.Errorf("parseToken = (%d, %v), want (%d, nil)", userId, err, resp.User.ID)
	}
	created, ok := userRepo.users[resp.User.ID]
	if !ok {
		t.Fatalf("пользователь %d не создан", resp.User.ID)
	}
	if created.AppleSub != "apple-sub-1" {
		t.Errorf("apple_sub = %q, want apple-sub-1", created.AppleSub)
	}
	// relay-адрес валиден и письма доходят, но склеивать по нему аккаунты нельзя
	if created.Email != "abc123@privaterelay.appleid.com" {
		t.Errorf("email = %q, want relay-адрес из токена", created.Email)
	}
	if created.DisplayName != "Загир Нурмухаметов" {
		t.Errorf("display_name = %q, want имя из тела запроса", created.DisplayName)
	}
	if created.TelegramID != nil {
		t.Errorf("telegram_id = %v, у apple-пользователя его быть не должно", *created.TelegramID)
	}
	if strings.Contains(rec.Body.String(), "apple-sub-1") || strings.Contains(rec.Body.String(), "privaterelay") {
		t.Errorf("ответ содержит поля личности: %s", rec.Body.String())
	}
}

// Повторный вход приходит БЕЗ email и без имени (Apple отдаёт их только один
// раз) — сохранённое затирать нельзя
func TestAuthAppleSecondLoginKeepsEmailAndName(t *testing.T) {
	userRepo := newFakeUserRepo()
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "abc123@privaterelay.appleid.com", "").
		withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	s := newTestServer(Config{AppleVerifier: v}, userRepo, newFakeRoomRepo())

	first := appleLogin(t, s, "Загир")
	if first.Code != http.StatusOK {
		t.Fatalf("первый вход: status = %d, body: %s", first.Code, first.Body.String())
	}

	// второй вход: токен без email, имя пустое
	v.with(testAppleToken, "apple-sub-1", "", "").withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	second := appleLogin(t, s, "")
	if second.Code != http.StatusOK {
		t.Fatalf("второй вход: status = %d, body: %s", second.Code, second.Body.String())
	}

	var r1, r2 authResponseDto
	if err := json.Unmarshal(first.Body.Bytes(), &r1); err != nil {
		t.Fatalf("cannot parse first response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &r2); err != nil {
		t.Fatalf("cannot parse second response: %v", err)
	}
	if r1.User.ID != r2.User.ID {
		t.Errorf("повторный вход дал другой _id: %d и %d", r1.User.ID, r2.User.ID)
	}
	if len(userRepo.users) != 1 {
		t.Errorf("в базе %d пользователей, ожидался 1", len(userRepo.users))
	}
	stored := userRepo.users[r1.User.ID]
	if stored.Email != "abc123@privaterelay.appleid.com" {
		t.Errorf("email = %q, повторный вход затёр сохранённый адрес", stored.Email)
	}
	if stored.DisplayName != "Загир" {
		t.Errorf("display_name = %q, повторный вход затёр имя", stored.DisplayName)
	}
	if r2.User.DisplayName != "Загир" {
		t.Errorf("в ответе display_name = %q, want Загир", r2.User.DisplayName)
	}
}

// Обратный случай: аккаунт есть, но профиль пуст (первый вход прошёл до того,
// как клиент научился присылать имя) — непустое значение обязано дозаполниться
func TestAuthAppleFillsEmptyProfile(t *testing.T) {
	existing := api.User{ID: 1_000_000_000_005, AppleSub: "apple-sub-1"}
	userRepo := newFakeUserRepo(existing)
	s, _ := newAppleServer(userRepo, "apple-sub-1", "abc123@privaterelay.appleid.com")

	rec := appleLogin(t, s, "Загир")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	stored := userRepo.users[existing.ID]
	if stored.Email != "abc123@privaterelay.appleid.com" {
		t.Errorf("email = %q, пустое поле не дозаполнено", stored.Email)
	}
	if stored.DisplayName != "Загир" {
		t.Errorf("display_name = %q, пустое поле не дозаполнено", stored.DisplayName)
	}
}

// Провайдер не вправе переименовать человека, который уже назвался в Splitty сам
func TestAuthAppleDoesNotRenameExistingUser(t *testing.T) {
	existing := api.User{ID: 1_000_000_000_005, AppleSub: "apple-sub-1", DisplayName: "Загир", Email: "me@example.com"}
	userRepo := newFakeUserRepo(existing)
	s, _ := newAppleServer(userRepo, "apple-sub-1", "abc123@privaterelay.appleid.com")

	if rec := appleLogin(t, s, "Zagir From Apple"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	stored := userRepo.users[existing.ID]
	if stored.DisplayName != "Загир" {
		t.Errorf("display_name = %q, провайдер переименовал существующего пользователя", stored.DisplayName)
	}
	if stored.Email != "me@example.com" {
		t.Errorf("email = %q, провайдер переписал сохранённый адрес", stored.Email)
	}
}

// Несовпадение nonce — 401. Без этой проверки nonce на клиенте бутафория:
// перехваченный или переигранный токен пускали бы как свежий вход
func TestAuthAppleNonceMismatch(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newAppleServer(userRepo, "apple-sub-1", "user@example.com")

	rec := postApple(t, s, testAppleToken, "Загир", "another-nonce", "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	if len(userRepo.users) != 0 {
		t.Errorf("по токену с чужим nonce создан пользователь")
	}

	// хеш вместо сырого значения тоже не проходит: сравнивается hex(sha256(raw))
	rec = postApple(t, s, testAppleToken, "Загир", appleNonceHash(testAppleNonce), "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// Токен без claim nonce (провайдер его не вернул) тоже отвергается
func TestAuthAppleRejectsTokenWithoutNonce(t *testing.T) {
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "", "")
	s := newTestServer(Config{AppleVerifier: v}, newFakeUserRepo(), newFakeRoomRepo())

	rec := appleLogin(t, s, "Загир")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAuthAppleInvalidToken(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newAppleServer(userRepo, "apple-sub-1", "user@example.com")

	rec := postApple(t, s, "forged-token", "Загир", testAppleNonce, "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	if strings.Contains(rec.Body.String(), "подпись не проверена") {
		t.Errorf("причина отказа утекла клиенту: %s", rec.Body.String())
	}
	if len(userRepo.users) != 0 {
		t.Errorf("по невалидному токену создан пользователь")
	}
}

// Без APPLE_CLIENT_IDS — честный 503, а не 401
func TestAuthAppleNotConfigured(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(), newFakeRoomRepo())
	rec := appleLogin(t, s, "Загир")
	assertErrorCode(t, rec, http.StatusServiceUnavailable, "unavailable")
}

func TestAuthAppleValidation(t *testing.T) {
	s, v := newAppleServer(newFakeUserRepo(), "apple-sub-1", "")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/apple", "", `{}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	rec = postApple(t, s, "   ", "Загир", testAppleNonce, "")
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	// пустой nonce — тоже 400: проверять было бы нечего
	rec = postApple(t, s, testAppleToken, "Загир", "  ", "")
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	if v.calls != 0 {
		t.Errorf("невалидное тело ушло в верификатор %d раз(а)", v.calls)
	}
}

// Троттлинг per-IP со СВОИМ префиксом: бюджет входа через Apple не пересекается
// ни с Google, ни с входом по коду
func TestAuthAppleThrottled(t *testing.T) {
	s, _ := newAppleServer(newFakeUserRepo(), "apple-sub-1", "")

	for i := 0; i < oauthAttemptsPerMin; i++ {
		if rec := postApple(t, s, "forged-token", "", testAppleNonce, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: status = %d, want 401", i, rec.Code)
		}
	}
	rec := postApple(t, s, "forged-token", "", testAppleNonce, "")
	assertErrorCode(t, rec, http.StatusTooManyRequests, "rate_limited")

	// бюджет входа по коду с того же адреса не тронут
	codeRec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code":"ABCDEF"}`)
	assertErrorCode(t, codeRec, http.StatusUnauthorized, "invalid_code")
}

// authorizationCode меняется на refresh token — иначе при удалении аккаунта
// отзывать у Apple будет нечего (Guideline 5.1.1(v))
func TestAuthAppleStoresRefreshToken(t *testing.T) {
	userRepo := newFakeUserRepo()
	tokens := &fakeAppleTokens{refreshToken: "apple-refresh-1"}
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "user@example.com", "").
		withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	s := newTestServer(Config{AppleVerifier: v, AppleTokens: tokens}, userRepo, newFakeRoomRepo())

	rec := postApple(t, s, testAppleToken, "Загир", testAppleNonce, "auth-code-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if got := userRepo.users[resp.User.ID].AppleRefreshToken; got != "apple-refresh-1" {
		t.Errorf("apple_refresh_token = %q, want apple-refresh-1", got)
	}
	if len(tokens.codes) != 1 || tokens.codes[0] != "auth-code-1" {
		t.Errorf("в обмен ушли коды %q, ожидался ровно auth-code-1", tokens.codes)
	}
	if strings.Contains(rec.Body.String(), "apple-refresh-1") {
		t.Errorf("refresh token утёк клиенту: %s", rec.Body.String())
	}

	// повторный вход обновляет refresh token существующего пользователя
	tokens.refreshToken = "apple-refresh-2"
	if rec = postApple(t, s, testAppleToken, "", testAppleNonce, "auth-code-2"); rec.Code != http.StatusOK {
		t.Fatalf("повторный вход: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if got := userRepo.users[resp.User.ID].AppleRefreshToken; got != "apple-refresh-2" {
		t.Errorf("apple_refresh_token = %q, повторный вход не обновил токен", got)
	}
}

// Без authorizationCode в обмен никто не ходит
func TestAuthAppleSkipsExchangeWithoutCode(t *testing.T) {
	tokens := &fakeAppleTokens{refreshToken: "apple-refresh-1"}
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "", "").
		withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	s := newTestServer(Config{AppleVerifier: v, AppleTokens: tokens}, newFakeUserRepo(), newFakeRoomRepo())

	if rec := appleLogin(t, s, "Загир"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if len(tokens.codes) != 0 {
		t.Errorf("обмен вызван без кода: %q", tokens.codes)
	}
}

// Обмен best-effort: недоступный Apple не должен закрывать вход. Отсутствие
// refresh token — проблема отзыва при удалении, а не повод не пустить человека
func TestAuthAppleExchangeFailureDoesNotBlockLogin(t *testing.T) {
	userRepo := newFakeUserRepo()
	tokens := &fakeAppleTokens{err: errors.New("apple недоступен")}
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "user@example.com", "").
		withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	s := newTestServer(Config{AppleVerifier: v, AppleTokens: tokens}, userRepo, newFakeRoomRepo())

	rec := postApple(t, s, testAppleToken, "Загир", testAppleNonce, "auth-code-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("сбой обмена уронил вход: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if got := userRepo.users[resp.User.ID].AppleRefreshToken; got != "" {
		t.Errorf("apple_refresh_token = %q, при сбое обмена он должен остаться пустым", got)
	}
}

// Без ключа .p8 (AppleTokens == nil) вход работает как обычно: локальная
// разработка не обязана иметь ключ Apple
func TestAuthAppleWorksWithoutPrivateKey(t *testing.T) {
	userRepo := newFakeUserRepo()
	s, _ := newAppleServer(userRepo, "apple-sub-1", "user@example.com")

	rec := postApple(t, s, testAppleToken, "Загир", testAppleNonce, "auth-code-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

// Retry на duplicate key начинается с повторного FindByAppleSub: проигравший
// гонку двух первых входов подбирает документ победителя
func TestAuthAppleDuplicateKeyPicksUpWinner(t *testing.T) {
	const winnerID = 1_000_000_000_888
	base := newFakeUserRepo()
	repo := &racingUserRepo{
		fakeUserRepo: base,
		winner:       api.User{ID: winnerID, AppleSub: "apple-sub-1", DisplayName: "Загир"},
	}
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "user@example.com", "").
		withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	roomRepo := newFakeRoomRepo()
	s := NewServer(Config{JwtSecret: "test-secret", AppleVerifier: v}, repo, roomRepo, newFakeLoginCodeRepo(),
		service.NewRoomService(roomRepo), service.NewOperationService(roomRepo), newFakeUserIDAllocator())

	rec := appleLogin(t, s, "Загир")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp authResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if resp.User.ID != winnerID {
		t.Errorf("resp.User.ID = %d, want %d (документ победителя гонки)", resp.User.ID, winnerID)
	}
	if repo.creates != 1 {
		t.Errorf("CreateIdentityUser вызван %d раз(а), ожидался 1: повторная вставка упёрлась бы в unique-индекс", repo.creates)
	}
	if len(base.users) != 1 {
		t.Errorf("в базе %d пользователей, ожидался 1", len(base.users))
	}
}

// --- ревью: решения accountAlive в auth-middleware ---

// Ошибка чтения базы НЕ пропускает запрос. Это осознанный fail-closed: без него
// лежащая (или отвечающая ошибками) mongo превращается в обход инвалидации
// токена — удалённый аккаунт продолжал бы работать, пока база нездорова
func TestAuthMiddlewareFailsClosedOnRepoError(t *testing.T) {
	repo := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, repo, newFakeRoomRepo())
	token := mustToken(t, s, testUser1.ID)

	repo.findErr = errors.New("mongo недоступна")
	rec := doRequest(t, s, http.MethodGet, "/api/v1/me", token, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (fail-closed), body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, http.StatusInternalServerError, "internal")

	// вердикт «недоступно» кешироваться не должен: как только база ожила,
	// запрос обязан проходить без ожидания accountTTL
	repo.findErr = nil
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/me", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("после восстановления базы: status = %d, want 200", rec.Code)
	}
}

// Валидный JWT пользователя, документа которого нет (база восстановлена из
// старого дампа, документ вычищен руками) — 401, а не 500 и не тихий пропуск
func TestAuthMiddlewareUnknownUser(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(), newFakeRoomRepo())
	assertErrorCode(t, doRequest(t, s, http.MethodGet, "/api/v1/me", mustToken(t, s, 777), ""),
		http.StatusUnauthorized, "unauthorized")
}

// Аллокатор номеров не выдал номер — 500, а не пользователь с нулевым _id
func TestAuthGoogleAllocatorFailure(t *testing.T) {
	repo := newFakeRoomRepo()
	userRepo := newFakeUserRepo()
	alloc := newFakeUserIDAllocator()
	alloc.err = errors.New("sequence недоступна")
	v := newFakeVerifier().with(testGoogleToken, "google-sub-1", "user@example.com", "Загир")
	s := NewServer(Config{JwtSecret: "test-secret", GoogleVerifier: v}, userRepo, repo, newFakeLoginCodeRepo(),
		service.NewRoomService(repo), service.NewOperationService(repo), alloc)

	assertErrorCode(t, postGoogle(t, s, testGoogleToken), http.StatusInternalServerError, "internal")
	if len(userRepo.users) != 0 {
		t.Fatalf("создан пользователь без номера: %v", userRepo.users)
	}
}

// alwaysRacingUserRepo проигрывает гонку КАЖДЫЙ раз: победитель, документ
// которого можно было бы подобрать, так и не появляется. Такое возможно только
// при рассинхронизации индексов, но retry-цикл обязан завершиться ошибкой, а не
// крутиться вечно и не отдать (nil, nil)
type alwaysRacingUserRepo struct {
	*fakeUserRepo
	creates int
}

func (r *alwaysRacingUserRepo) CreateIdentityUser(context.Context, api.User) error {
	r.creates++
	return errDuplicateKey
}

func TestAuthGoogleRetriesExhausted(t *testing.T) {
	roomRepo := newFakeRoomRepo()
	repo := &alwaysRacingUserRepo{fakeUserRepo: newFakeUserRepo()}
	v := newFakeVerifier().with(testGoogleToken, "google-sub-1", "user@example.com", "Загир")
	s := NewServer(Config{JwtSecret: "test-secret", GoogleVerifier: v}, repo, roomRepo, newFakeLoginCodeRepo(),
		service.NewRoomService(roomRepo), service.NewOperationService(roomRepo), newFakeUserIDAllocator())

	assertErrorCode(t, postGoogle(t, s, testGoogleToken), http.StatusInternalServerError, "internal")
	if repo.creates != identityAuthAttempts {
		t.Fatalf("попыток вставки %d, ожидалось %d", repo.creates, identityAuthAttempts)
	}
}

func TestAuthAppleRetriesExhausted(t *testing.T) {
	roomRepo := newFakeRoomRepo()
	repo := &alwaysRacingUserRepo{fakeUserRepo: newFakeUserRepo()}
	v := newFakeVerifier().with(testAppleToken, "apple-sub-1", "", "").withNonce(testAppleToken, appleNonceHash(testAppleNonce))
	s := NewServer(Config{JwtSecret: "test-secret", AppleVerifier: v}, repo, roomRepo, newFakeLoginCodeRepo(),
		service.NewRoomService(roomRepo), service.NewOperationService(roomRepo), newFakeUserIDAllocator())

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/apple", "",
		fmt.Sprintf(`{"idToken": %q, "nonce": %q}`, testAppleToken, testAppleNonce))
	assertErrorCode(t, rec, http.StatusInternalServerError, "internal")
	if repo.creates != identityAuthAttempts {
		t.Fatalf("попыток вставки %d, ожидалось %d", repo.creates, identityAuthAttempts)
	}
}
