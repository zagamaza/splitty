package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/oidc"
)

const (
	testLinkGoogleToken = "google-link-token"
	testLinkAppleToken  = "apple-link-token"
)

// linkUser — пользователь с уже привязанным telegram: с него начинается почти
// каждый сценарий привязки (человек пришёл из бота и добавляет второй способ)
func linkUser(id int) api.User {
	return withTelegram(api.User{ID: id, Username: "zagir", DisplayName: "Загир"}, id)
}

func postLink(t *testing.T, s *Server, provider, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodPost, "/api/v1/me/link/"+provider, token, body)
}

func deleteLink(t *testing.T, s *Server, provider, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodDelete, "/api/v1/me/link/"+provider, token, "")
}

func parseLinkResponse(t *testing.T, rec *httptest.ResponseRecorder) linkResponseDto {
	t.Helper()
	var resp linkResponseDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse link response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func assertProviders(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("linkedProviders = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("linkedProviders = %v, want %v", got, want)
		}
	}
}

// telegramLinkBody тело /me/link/telegram с валидной подписью виджета
func telegramLinkBody(tgID int, authDate int64) string {
	hash := signTelegramFields(map[string]string{
		"auth_date": strconv.FormatInt(authDate, 10),
		"id":        strconv.Itoa(tgID),
	}, testTgToken)
	return fmt.Sprintf(`{"id": %d, "authDate": %d, "hash": %q}`, tgID, authDate, hash)
}

// newLinkServer сервер, у которого сконфигурированы все три способа входа
func newLinkServer(userRepo *fakeUserRepo, googleSub, appleSub string) *Server {
	cfg := Config{
		TgToken:        testTgToken,
		GoogleVerifier: newFakeVerifier().with(testLinkGoogleToken, googleSub, "user@example.com", "Загир"),
		AppleVerifier:  newFakeVerifier().with(testLinkAppleToken, appleSub, "", ""),
	}
	return newTestServer(cfg, userRepo, newFakeRoomRepo())
}

// Привязка каждого провайдера к аккаунту, где уже есть telegram
func TestLinkIdentitySuccess(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		body     string
		want     []string
		check    func(t *testing.T, u *api.User)
	}{
		{
			name:     "google",
			provider: providerGoogle,
			body:     fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken),
			want:     []string{providerTelegram, providerGoogle},
			check: func(t *testing.T, u *api.User) {
				if u.GoogleSub != "google-sub-link" {
					t.Fatalf("google_sub = %q, want google-sub-link", u.GoogleSub)
				}
			},
		},
		{
			name:     "apple",
			provider: providerApple,
			body:     fmt.Sprintf(`{"idToken": %q}`, testLinkAppleToken),
			want:     []string{providerTelegram, providerApple},
			check: func(t *testing.T, u *api.User) {
				if u.AppleSub != "apple-sub-link" {
					t.Fatalf("apple_sub = %q, want apple-sub-link", u.AppleSub)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeUserRepo(linkUser(42))
			s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

			rec := postLink(t, s, tc.provider, mustToken(t, s, 42), tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
			}
			resp := parseLinkResponse(t, rec)
			assertProviders(t, resp.User.LinkedProviders, tc.want)
			if resp.Warning != "" {
				t.Errorf("привязка не должна возвращать предупреждение, получено: %q", resp.Warning)
			}
			tc.check(t, repo.users[42])
		})
	}
}

// Привязка telegram к google-аккаунту: номер Splitty синтетический, telegram id
// свой — именно ради этого случая telegram_id стал отдельным полем
func TestLinkTelegramSuccess(t *testing.T) {
	const userID = 1_000_000_000_005
	repo := newFakeUserRepo(api.User{ID: userID, DisplayName: "Загир", GoogleSub: "google-sub-link"})
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	rec := postLink(t, s, providerTelegram, mustToken(t, s, userID), telegramLinkBody(304898122, time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerTelegram, providerGoogle})

	u := repo.users[userID]
	if u.TelegramID == nil || *u.TelegramID != 304898122 {
		t.Fatalf("telegram_id = %v, want 304898122", u.TelegramID)
	}
	if u.ID != userID {
		t.Fatalf("_id изменился на %d: номер Splitty менять нельзя", u.ID)
	}
}

// Повторная привязка ТОЙ ЖЕ личности к ТОМУ ЖЕ аккаунту — 200, а не ошибка:
// клиент вправе повторить запрос (ретрай, второе нажатие)
func TestLinkIdempotent(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")
	token := mustToken(t, s, 42)
	body := fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken)

	if rec := postLink(t, s, providerGoogle, token, body); rec.Code != http.StatusOK {
		t.Fatalf("первая привязка: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	rec := postLink(t, s, providerGoogle, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("повторная привязка: status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerTelegram, providerGoogle})
	if len(repo.users) != 1 {
		t.Fatalf("в базе %d пользователей, ожидался 1", len(repo.users))
	}
}

// Личность, занятая ДРУГИМ профилем, — 409 identity_taken. Слияние профилей
// вне объёма: снимки пользователей денормализованы по всем комнатам
func TestLinkIdentityTaken(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		other    api.User
		body     string
	}{
		{"google", providerGoogle, api.User{ID: 7, GoogleSub: "google-sub-link"}, fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken)},
		{"apple", providerApple, api.User{ID: 7, AppleSub: "apple-sub-link"}, fmt.Sprintf(`{"idToken": %q}`, testLinkAppleToken)},
		{"telegram", providerTelegram, linkUser(304898122), telegramLinkBody(304898122, time.Now().Unix())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeUserRepo(api.User{ID: 42, GoogleSub: "own-google-sub"}, tc.other)
			s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

			rec := postLink(t, s, tc.provider, mustToken(t, s, 42), tc.body)
			assertErrorCode(t, rec, http.StatusConflict, "identity_taken")
		})
	}
}

// ВТОРАЯ личность того же провайдера — 409 identity_already_linked, а не тихая
// перезамена первой.
//
// Для Apple это прямо Guideline 5.1.1(v): перезапись отцепила бы прежний
// apple_sub БЕЗ auth/revoke (Splitty навсегда остался бы в списке «Вход через
// Apple» того Apple ID) и затёрла бы его refresh token — отзывать потом нечем.
// Кнопку «Привязать» у привязанного провайдера прячут оба клиента, но это UI,
// а не защита
func TestLinkIdentityAlreadyLinked(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		user     api.User
		body     string
		check    func(t *testing.T, u *api.User)
	}{
		{
			name:     "google",
			provider: providerGoogle,
			user:     api.User{ID: 42, DisplayName: "Загир", GoogleSub: "old-google-sub"},
			body:     fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken),
			check: func(t *testing.T, u *api.User) {
				if u.GoogleSub != "old-google-sub" {
					t.Fatalf("google_sub = %q, привязка подменила прежнюю личность", u.GoogleSub)
				}
			},
		},
		{
			name: "apple",
			user: api.User{
				ID: 42, DisplayName: "Загир",
				AppleSub: "old-apple-sub", AppleRefreshToken: "old-refresh",
			},
			provider: providerApple,
			body:     fmt.Sprintf(`{"idToken": %q}`, testLinkAppleToken),
			check: func(t *testing.T, u *api.User) {
				if u.AppleSub != "old-apple-sub" {
					t.Fatalf("apple_sub = %q, привязка подменила прежнюю личность", u.AppleSub)
				}
				// Токен прежней личности обязан остаться: им её отзывают при
				// отвязке и удалении аккаунта
				if u.AppleRefreshToken != "old-refresh" {
					t.Fatalf("apple_refresh_token = %q, want old-refresh", u.AppleRefreshToken)
				}
			},
		},
		{
			name:     "telegram",
			provider: providerTelegram,
			user:     withTelegram(api.User{ID: 42, DisplayName: "Загир"}, 111),
			body:     telegramLinkBody(304898122, time.Now().Unix()),
			check: func(t *testing.T, u *api.User) {
				if u.TelegramID == nil || *u.TelegramID != 111 {
					t.Fatalf("telegram_id = %v, привязка подменила прежнюю личность", u.TelegramID)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeUserRepo(tc.user)
			s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

			rec := postLink(t, s, tc.provider, mustToken(t, s, 42), tc.body)
			assertErrorCode(t, rec, http.StatusConflict, "identity_already_linked")
			tc.check(t, repo.users[42])
		})
	}
}

// Ошибки запроса: невыключенный провайдер, пустой токен, отвергнутый токен,
// несуществующий провайдер
func TestLinkIdentityErrors(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	configured := newLinkServer(repo, "google-sub-link", "apple-sub-link")
	empty := newTestServer(Config{}, repo, newFakeRoomRepo())

	tests := []struct {
		name       string
		server     *Server
		provider   string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"google не сконфигурирован", empty, providerGoogle, `{"idToken": "x"}`, http.StatusServiceUnavailable, "unavailable"},
		{"apple не сконфигурирован", empty, providerApple, `{"idToken": "x"}`, http.StatusServiceUnavailable, "unavailable"},
		{"telegram не сконфигурирован", empty, providerTelegram, `{"id": 1, "authDate": 1, "hash": "x"}`, http.StatusServiceUnavailable, "unavailable"},
		{"пустой idToken", configured, providerGoogle, `{"idToken": "  "}`, http.StatusBadRequest, "validation"},
		{"чужой idToken", configured, providerGoogle, `{"idToken": "forged"}`, http.StatusBadRequest, "provider_rejected"},
		{"неизвестный провайдер", configured, "vkontakte", `{"idToken": "x"}`, http.StatusNotFound, "not_found"},
		{"telegram без обязательных полей", configured, providerTelegram, `{"id": 1}`, http.StatusBadRequest, "validation"},
		{"telegram с неверной подписью", configured, providerTelegram, `{"id": 1, "authDate": 1, "hash": "deadbeef"}`, http.StatusUnauthorized, "unauthorized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := postLink(t, tc.server, tc.provider, mustToken(t, tc.server, 42), tc.body)
			assertErrorCode(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

// Протухший auth_date — 401: подпись телеграма валидна вечно, единственная
// защита от переигрывания перехваченного payload — его возраст
func TestLinkTelegramStaleAuthDate(t *testing.T) {
	repo := newFakeUserRepo(api.User{ID: 42, GoogleSub: "own"})
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	stale := time.Now().Add(-maxAuthAge - time.Minute).Unix()
	rec := postLink(t, s, providerTelegram, mustToken(t, s, 42), telegramLinkBody(304898122, stale))
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	if repo.users[42].TelegramID != nil {
		t.Fatal("telegram_id записан по протухшему payload")
	}
}

// Nonce в токене Apple подписан провайдером: если он есть, а клиент прислал не
// тот — токен переигран, привязку делать нельзя
func TestLinkAppleNonceMismatch(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	v := newFakeVerifier().with(testLinkAppleToken, "apple-sub-link", "", "").withNonce(testLinkAppleToken, appleNonceHash("правильный"))
	s := newTestServer(Config{AppleVerifier: v}, repo, newFakeRoomRepo())

	rec := postLink(t, s, providerApple, mustToken(t, s, 42), fmt.Sprintf(`{"idToken": %q, "nonce": "чужой"}`, testLinkAppleToken))
	// 400, а не 401: отвергнут токен ПРОВАЙДЕРА, сессия Splitty жива
	// (см. writeProviderRejected)
	assertErrorCode(t, rec, http.StatusBadRequest, "provider_rejected")
	if repo.users[42].AppleSub != "" {
		t.Fatal("apple_sub записан при несовпадении nonce")
	}

	ok := postLink(t, s, providerApple, mustToken(t, s, 42), fmt.Sprintf(`{"idToken": %q, "nonce": "правильный"}`, testLinkAppleToken))
	if ok.Code != http.StatusOK {
		t.Fatalf("совпавший nonce: status = %d, want 200, body: %s", ok.Code, ok.Body.String())
	}
}

// ⚠️ Регрессия, вскрытая в проде на первой же реальной привязке: GoogleSignIn
// SDK кладёт СВОЙ nonce в id-токен, а наш клиент для Google nonce не передаёт
// и не может — SDK его наружу не отдаёт. Пока проверка шла по «есть claim →
// сверяем», привязка Google возвращала 400 ВСЕГДА. Сверять nonce осмысленно
// только там, где его генерирует наш клиент, то есть у Apple
func TestLinkGoogleIgnoresProviderNonce(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	v := newFakeVerifier().with(testLinkGoogleToken, "google-sub-link", "", "").
		withNonce(testLinkGoogleToken, "nonce-от-google-sdk")
	s := newTestServer(Config{GoogleVerifier: v}, repo, newFakeRoomRepo())

	// клиент nonce не прислал — как и в реальном iOS-потоке
	rec := postLink(t, s, providerGoogle, mustToken(t, s, 42), fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (nonce провайдера нам сверять не с чем), body: %s", rec.Code, rec.Body.String())
	}
	if repo.users[42].GoogleSub != "google-sub-link" {
		t.Fatalf("google_sub = %q, привязка не записана", repo.users[42].GoogleSub)
	}
}

// Отвязка при двух способах входа — успех; списки способов в ответе актуальны
func TestUnlinkIdentitySuccess(t *testing.T) {
	repo := newFakeUserRepo(withTelegram(api.User{ID: 42, GoogleSub: "google-sub-link"}, 42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	rec := deleteLink(t, s, providerGoogle, mustToken(t, s, 42))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerTelegram})
	if repo.users[42].GoogleSub != "" {
		t.Fatalf("google_sub = %q, ожидалась очистка", repo.users[42].GoogleSub)
	}
}

// Отвязка последнего способа входа запрещена: иначе аккаунт остался бы без
// возможности войти, а данные — недоступными после истечения токена
func TestUnlinkLastIdentity(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	rec := deleteLink(t, s, providerTelegram, mustToken(t, s, 42))
	assertErrorCode(t, rec, http.StatusConflict, "last_identity")
	if repo.users[42].TelegramID == nil {
		t.Fatal("telegram_id вычищен, хотя отвязка должна была быть отклонена")
	}
}

// Отвязка непривязанного способа — 200 (идемпотентность, симметрично привязке),
// неизвестного провайдера — 404
func TestUnlinkIdentityEdgeCases(t *testing.T) {
	repo := newFakeUserRepo(withTelegram(api.User{ID: 42, GoogleSub: "google-sub-link"}, 42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")
	token := mustToken(t, s, 42)

	rec := deleteLink(t, s, providerApple, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("отвязка непривязанного: status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	assertProviders(t, parseLinkResponse(t, rec).User.LinkedProviders, []string{providerTelegram, providerGoogle})

	assertErrorCode(t, deleteLink(t, s, "vkontakte", token), http.StatusNotFound, "not_found")
}

// ⚠️ Отвязка telegram — особый случай: бот заведёт человеку ОТДЕЛЬНЫЙ профиль,
// если он снова напишет. Ответ обязан содержать предупреждение, а chat_state —
// быть вычищенным, иначе новый профиль подхватит незавершённый сценарий старого
// (populateChatState ищет с fallback на сырой telegram id)
func TestUnlinkTelegramWarnsAndClearsChatState(t *testing.T) {
	const userID = 1_000_000_000_005
	const tgID = 304898122
	repo := newFakeUserRepo(withTelegram(api.User{ID: userID, GoogleSub: "google-sub-link"}, tgID))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")
	states := &fakeChatStates{}
	s.SetChatStates(states)

	rec := deleteLink(t, s, providerTelegram, mustToken(t, s, userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	resp := parseLinkResponse(t, rec)
	if resp.Warning == "" {
		t.Error("отвязка telegram обязана возвращать предупреждение о втором профиле")
	}
	assertProviders(t, resp.User.LinkedProviders, []string{providerGoogle})

	// чистка идёт и по каноническому номеру, и по сырому telegram id: состояния
	// исторически сохранялись по обоим
	want := map[int]bool{userID: true, tgID: true}
	if len(states.deleted) != len(want) {
		t.Fatalf("chat_state чистился по %v, ожидались %v", states.deleted, want)
	}
	for _, id := range states.deleted {
		if !want[id] {
			t.Fatalf("chat_state чистился по неожиданному id %d", id)
		}
	}
}

// GET /me отдаёт список способов входа — на нём строится экран «Способы входа»
func TestMeReturnsLinkedProviders(t *testing.T) {
	repo := newFakeUserRepo(withTelegram(api.User{ID: 42, GoogleSub: "g", AppleSub: "a"}, 42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/me", mustToken(t, s, 42), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var me meDto
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("cannot parse me: %v", err)
	}
	assertProviders(t, me.LinkedProviders, []string{providerTelegram, providerGoogle, providerApple})
}

// blockingVerifier останавливает привязку ровно посередине: после входа в
// Verify тест успевает выполнить параллельное удаление аккаунта
type blockingVerifier struct {
	claims  *oidc.Claims
	entered chan struct{}
	release chan struct{}
}

func (b *blockingVerifier) Verify(context.Context, string) (*oidc.Claims, error) {
	close(b.entered)
	<-b.release
	return b.claims, nil
}

// ⚠️ Гонка «медленная привязка ↔ DELETE /me». Привязка уже прошла middleware,
// параллельно аккаунт становится tombstone с вычищенными личностями — и если бы
// SetIdentity делал upsert (или фильтровал только по _id), google_sub осел бы
// НА TOMBSTONE. Найти такого пользователя нельзя (поиск пропускает удалённых), а
// unique sparse индекс значение уже занял: человек навсегда лишился бы
// возможности зарегистрироваться заново тем же Google-аккаунтом
func TestLinkRaceWithAccountDeletion(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	blocking := &blockingVerifier{
		claims:  &oidc.Claims{},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	blocking.claims.Subject = "google-sub-race"
	s := newTestServer(Config{GoogleVerifier: blocking}, repo, newFakeRoomRepo())

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postLink(t, s, providerGoogle, mustToken(t, s, 42), fmt.Sprintf(`{"idToken": %q}`, "any"))
	}()

	<-blocking.entered
	// то, что делает DELETE /me (Task 13): tombstone + освобождение личностей
	deletedAt := time.Now()
	repo.users[42].DeletedAt = &deletedAt
	repo.users[42].TelegramID = nil
	repo.users[42].GoogleSub = ""
	close(blocking.release)

	rec := <-done
	if rec.Code == http.StatusOK {
		t.Fatalf("привязка на удалённый аккаунт вернула 200: %s", rec.Body.String())
	}
	if got := repo.users[42].GoogleSub; got != "" {
		t.Fatalf("google_sub = %q записан на tombstone", got)
	}

	// личность свободна: тот же google_sub регистрируется заново
	fresh := newTestServer(Config{GoogleVerifier: newFakeVerifier().with(testGoogleToken, "google-sub-race", "", "Загир")},
		repo, newFakeRoomRepo())
	authRec := postGoogle(t, fresh, testGoogleToken)
	if authRec.Code != http.StatusOK {
		t.Fatalf("повторная регистрация с тем же google_sub: status = %d, body: %s", authRec.Code, authRec.Body.String())
	}
	var auth authResponseDto
	if err := json.Unmarshal(authRec.Body.Bytes(), &auth); err != nil {
		t.Fatalf("cannot parse auth response: %v", err)
	}
	if auth.User.ID == 42 {
		t.Fatal("вход завёл токен на tombstone, ожидался новый пользователь")
	}
}

// --- ревью: привязка Apple обязана забрать authorizationCode ---

// Код одноразовый и живёт минуты: не обменяв его ЗДЕСЬ, мы навсегда лишаемся
// возможности отозвать токены Apple при удалении аккаунта (Guideline 5.1.1(v)).
// Именно этот сценарий — человек завёлся через Telegram/Google и привязал Apple
// с экрана «Способы входа» — и проверяет ревью Apple
func TestLinkAppleExchangesAuthorizationCode(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	tokens := &fakeAppleTokens{refreshToken: "apple-refresh-from-link"}
	s := newTestServer(Config{
		AppleVerifier: newFakeVerifier().with(testLinkAppleToken, "apple-sub-link", "apple@privaterelay.appleid.com", ""),
		AppleTokens:   tokens,
	}, repo, newFakeRoomRepo())

	rec := postLink(t, s, providerApple, mustToken(t, s, 42),
		fmt.Sprintf(`{"idToken": %q, "authorizationCode": "one-shot-code"}`, testLinkAppleToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if len(tokens.codes) != 1 || tokens.codes[0] != "one-shot-code" {
		t.Fatalf("authorizationCode не обменян: codes = %v", tokens.codes)
	}
	if got := repo.users[42].AppleRefreshToken; got != "apple-refresh-from-link" {
		t.Fatalf("apple_refresh_token = %q, want %q — отзывать при удалении будет нечего",
			got, "apple-refresh-from-link")
	}
	// email из токена дописывается в пустое поле, имя привязка не трогает
	if repo.users[42].Email != "apple@privaterelay.appleid.com" {
		t.Errorf("email = %q, ожидался адрес из токена", repo.users[42].Email)
	}
	if repo.users[42].DisplayName != "Загир" {
		t.Errorf("display_name = %q: привязка переименовала пользователя", repo.users[42].DisplayName)
	}
}

// Обмен кода — best-effort: недоступный Apple не должен ломать саму привязку
func TestLinkAppleSurvivesExchangeFailure(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	tokens := &fakeAppleTokens{err: errors.New("apple недоступен")}
	s := newTestServer(Config{
		AppleVerifier: newFakeVerifier().with(testLinkAppleToken, "apple-sub-link", "", ""),
		AppleTokens:   tokens,
	}, repo, newFakeRoomRepo())

	rec := postLink(t, s, providerApple, mustToken(t, s, 42),
		fmt.Sprintf(`{"idToken": %q, "authorizationCode": "one-shot-code"}`, testLinkAppleToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: сбой обмена не повод отказать в привязке", rec.Code)
	}
	if repo.users[42].AppleSub != "apple-sub-link" {
		t.Fatal("apple_sub не записан")
	}
}

// Отвязка Apple: токены отзываются ДО чистки, а сам refresh token не остаётся
// в базе — это живой секрет от личности, которой у пользователя больше нет
func TestUnlinkAppleRevokesAndClearsRefreshToken(t *testing.T) {
	user := withTelegram(api.User{ID: 42, DisplayName: "Загир"}, 42)
	user.AppleSub = "apple-sub-link"
	user.AppleRefreshToken = "apple-refresh-stored"
	repo := newFakeUserRepo(user)
	tokens := &fakeAppleTokens{}
	s := newTestServer(Config{TgToken: testTgToken, AppleTokens: tokens}, repo, newFakeRoomRepo())

	rec := deleteLink(t, s, providerApple, mustToken(t, s, 42))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if len(tokens.revoked) != 1 || tokens.revoked[0] != "apple-refresh-stored" {
		t.Fatalf("RevokeToken не вызван с сохранённым токеном: revoked = %v", tokens.revoked)
	}
	if repo.users[42].AppleRefreshToken != "" {
		t.Fatalf("apple_refresh_token остался в базе после отвязки: %q", repo.users[42].AppleRefreshToken)
	}
}

// Привязка троттлится по пользователю: каждая попытка — разбор JWT и, на
// промахе кеша ключей, поход к провайдеру. Ключ не по адресу — здесь уже есть
// проверенный токен, и подделать его (в отличие от X-Forwarded-For) нельзя
func TestLinkIdentityThrottled(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42), linkUser(43))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")

	body := fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken)
	for i := 0; i < oauthAttemptsPerMin; i++ {
		if rec := postLink(t, s, providerGoogle, mustToken(t, s, 42), body); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("попытка %d отбита раньше лимита", i)
		}
	}
	if rec := postLink(t, s, providerGoogle, mustToken(t, s, 42), body); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("после лимита ожидался 429, got %d", rec.Code)
	}
	// сосед по серверу не задет: ключ по пользователю, а не общий
	if rec := postLink(t, s, providerGoogle, mustToken(t, s, 43), body); rec.Code == http.StatusTooManyRequests {
		t.Fatal("лимит одного пользователя выжег привязку другому")
	}
}

// writeLinks не может перечитать пользователя — 500, а не тихий успех
func TestLinkWriteLinksReadFailure(t *testing.T) {
	repo := newFakeUserRepo(linkUser(42))
	s := newLinkServer(repo, "google-sub-link", "apple-sub-link")
	token := mustToken(t, s, 42)
	repo.findErr = errors.New("mongo недоступна")

	rec := postLink(t, s, providerGoogle, token, fmt.Sprintf(`{"idToken": %q}`, testLinkGoogleToken))
	assertErrorCode(t, rec, http.StatusInternalServerError, "internal")
}
