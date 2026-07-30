package rest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	for i := 0; i < oauthPerIPPerMin; i++ {
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
