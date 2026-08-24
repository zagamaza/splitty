package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
)

// sharedCounter — счётчик окон, общий для запросов теста: суточное окно
// действительно накапливается, минутное остаётся свободным.
type sharedCounter struct{ counts map[string]int64 }

func newSharedCounter() *sharedCounter { return &sharedCounter{counts: map[string]int64{}} }

func (c *sharedCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	c.counts[key]++
	return c.counts[key], nil
}

func (c *sharedCounter) Get(_ context.Context, key string) (int64, error) {
	return c.counts[key], nil
}

// stubSubs — подписки пользователя для резолва тарифа.
type stubSubs struct{ byUser map[int][]api.Subscription }

func (s *stubSubs) ActiveByUser(_ context.Context, userId int) ([]api.Subscription, error) {
	return s.byUser[userId], nil
}

func activeSub(userId int) api.Subscription {
	return api.Subscription{
		UserId:    userId,
		Store:     api.StoreApple,
		StoreRef:  "orig-1",
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
		AutoRenew: true,
	}
}

// quotas — суточные лимиты теста. Живут в одном месте, как и в проде:
// единственный их владелец — service.Entitlements.
type quotas struct{ free, plus, legacy int }

func defaultQuotas() quotas {
	return quotas{free: 5, plus: service.UnlimitedQuota, legacy: 50}
}

// quotaServer собирает сервер с распознаванием, тарифами и подписками.
func quotaServer(t *testing.T, counter service.UsageCounter, subs *stubSubs, q quotas) (*Server, *api.Room) {
	t.Helper()
	if subs == nil {
		subs = &stubSubs{}
	}
	room := newTestRoom()
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room),
		&fakeParser{result: okDraft()}, service.NewRateLimiter(counter, 100))
	s.SetEntitlements(service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota:   q.free,
		PlusQuota:   q.plus,
		LegacyQuota: q.legacy,
	}))
	return s, room
}

// doParseWithVersion шлёт распознавание, объявляя версию клиента (пустая
// строка — сборка, которая заголовок не знает).
func doParseWithVersion(t *testing.T, s *Server, roomId, token, version string) *httptest.ResponseRecorder {
	t.Helper()
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+roomId+"/operations/parse", body)
	req.Header.Set("Content-Type", ct)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if version != "" {
		req.Header.Set(clientVersionHeader, version)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// getQuota читает остаток, объявляя версию клиента (пустая — старая сборка).
func getQuota(t *testing.T, s *Server, token, version string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/ai-quota", strings.NewReader(""))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if version != "" {
		req.Header.Set(clientVersionHeader, version)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestAiQuotaEndpointDoesNotConsume — просмотр остатка его не расходует.
func TestAiQuotaEndpointDoesNotConsume(t *testing.T) {
	s, _ := quotaServer(t, newSharedCounter(), nil, defaultQuotas())

	for i := 0; i < 3; i++ {
		rec := getQuota(t, s, mustToken(t, s, testUser1.ID), "1.7.0")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var q quotaDto
		if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
			t.Fatalf("разбор ответа: %v", err)
		}
		if q.Used != 0 || q.Remaining != 5 {
			t.Errorf("просмотр израсходовал квоту: used=%d remaining=%d", q.Used, q.Remaining)
		}
		if q.Tier != api.TierFree {
			t.Errorf("tier = %q, want free", q.Tier)
		}
	}
}

// TestAiQuotaReportsPlusAsUnlimited — у Plus потолка нет.
func TestAiQuotaReportsPlusAsUnlimited(t *testing.T) {
	subs := &stubSubs{byUser: map[int][]api.Subscription{testUser1.ID: {activeSub(testUser1.ID)}}}
	s, _ := quotaServer(t, newSharedCounter(), subs, defaultQuotas())

	rec := getQuota(t, s, mustToken(t, s, testUser1.ID), "1.7.0")
	var q quotaDto
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if q.Tier != api.TierPlus || !q.Unlimited {
		t.Errorf("хотели plus/безлимит, получили tier=%q unlimited=%v", q.Tier, q.Unlimited)
	}
}

// TestParseReturnsQuotaInSuccess — остаток едет в успешном ответе, чтобы
// счётчик у микрофона обновлялся без отдельного запроса.
func TestParseReturnsQuotaInSuccess(t *testing.T) {
	s, room := quotaServer(t, newSharedCounter(), nil, defaultQuotas())

	rec := doParseWithVersion(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "1.7.0")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Draft json.RawMessage `json:"draft"`
		Quota quotaDto        `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(resp.Draft) == 0 {
		t.Error("черновик пропал из корня ответа — сборки 1.6 читают именно его")
	}
	if resp.Quota.Used != 1 || resp.Quota.Remaining != 4 {
		t.Errorf("quota = %+v, хотели used=1 remaining=4", resp.Quota)
	}
}

// TestParseFreeUserExhaustsAtSixth — бесплатный упирается на шестом, и это
// именно суточный отказ, открывающий экран оплаты.
func TestParseFreeUserExhaustsAtSixth(t *testing.T) {
	s, room := quotaServer(t, newSharedCounter(), nil, defaultQuotas())
	token := mustToken(t, s, testUser1.ID)

	for i := 0; i < 5; i++ {
		if rec := doParseWithVersion(t, s, room.ID.Hex(), token, "1.7.0"); rec.Code != http.StatusOK {
			t.Fatalf("распознавание %d: status = %d", i+1, rec.Code)
		}
	}

	rec := doParseWithVersion(t, s, room.ID.Hex(), token, "1.7.0")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("шестое: status = %d, want 429", rec.Code)
	}

	var resp struct {
		Error errorBody `json:"error"`
		Quota quotaDto  `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if resp.Error.Code != errCodeAiQuotaExceeded {
		t.Errorf("code = %q, want %q", resp.Error.Code, errCodeAiQuotaExceeded)
	}
	if resp.Quota.Remaining != 0 || resp.Quota.Limit != 5 {
		t.Errorf("quota = %+v, хотели remaining=0 limit=5", resp.Quota)
	}
	if resp.Quota.ResetsAt.IsZero() {
		t.Error("не сказано, когда лимит обновится — экрану оплаты нечего написать")
	}
}

// TestParsePlusUserNeverExhausts — Plus не упирается в суточную норму.
func TestParsePlusUserNeverExhausts(t *testing.T) {
	subs := &stubSubs{byUser: map[int][]api.Subscription{testUser1.ID: {activeSub(testUser1.ID)}}}
	s, room := quotaServer(t, newSharedCounter(), subs, defaultQuotas())
	token := mustToken(t, s, testUser1.ID)

	for i := 0; i < 12; i++ {
		if rec := doParseWithVersion(t, s, room.ID.Hex(), token, "1.7.0"); rec.Code != http.StatusOK {
			t.Fatalf("распознавание %d у Plus: status = %d", i+1, rec.Code)
		}
	}
}

// TestParseLegacyClientGetsLegacyQuota — сборка без заголовка версии не умеет
// показать экран оплаты, поэтому получает legacy-лимит.
//
// Иначе в день выкатки распознавание сломалось бы у всех, кто ещё не обновился,
// и заплатить им было бы негде: 1.6 про подписку не знает вовсе.
func TestParseLegacyClientGetsLegacyQuota(t *testing.T) {
	s, room := quotaServer(t, newSharedCounter(), nil, quotas{free: 5, plus: service.UnlimitedQuota, legacy: 8})
	token := mustToken(t, s, testUser1.ID)

	for i := 0; i < 8; i++ {
		if rec := doParseWithVersion(t, s, room.ID.Hex(), token, ""); rec.Code != http.StatusOK {
			t.Fatalf("распознавание %d на старой сборке: status = %d", i+1, rec.Code)
		}
	}
	if rec := doParseWithVersion(t, s, room.ID.Hex(), token, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("девятое на старой сборке: status = %d, want 429", rec.Code)
	}
}

// TestQuotaErrorBodyStaysParsableByOldClients — тело 429 разбирается моделью,
// которую знают сборки 1.6.
//
// Они читают ровно {"error":{"code","message"}}; вынести код наверх или
// переименовать конверт значило бы сломать разбор ошибок у всех, кто не
// обновился, — а обновиться они смогут не раньше выхода 1.7.
func TestQuotaErrorBodyStaysParsableByOldClients(t *testing.T) {
	s, room := quotaServer(t, newSharedCounter(), nil, quotas{free: 1, plus: service.UnlimitedQuota, legacy: 1})
	token := mustToken(t, s, testUser1.ID)

	if rec := doParseWithVersion(t, s, room.ID.Hex(), token, "1.7.0"); rec.Code != http.StatusOK {
		t.Fatalf("первое распознавание: status = %d", rec.Code)
	}
	rec := doParseWithVersion(t, s, room.ID.Hex(), token, "1.7.0")

	// Ровно та форма, что у APIError.server на iOS и ErrorEnvelope на Android.
	var legacy struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("модель 1.6 не разбирает тело: %v (%s)", err, rec.Body.String())
	}
	if legacy.Error.Code == "" {
		t.Error("пустой code")
	}
	if strings.TrimSpace(legacy.Error.Message) == "" {
		t.Error("пустой message: сборки 1.6 показывают именно его, строки для нового кода у них нет")
	}
}

// TestAiQuotaReportsLegacyLimitForOldBuilds — старая сборка видит свой
// legacy-остаток, а не бесплатный: цифра на экране должна совпадать с тем, на
// чём человека реально отобьют.
func TestAiQuotaReportsLegacyLimitForOldBuilds(t *testing.T) {
	s, _ := quotaServer(t, newSharedCounter(), nil, quotas{free: 5, plus: service.UnlimitedQuota, legacy: 50})

	rec := getQuota(t, s, mustToken(t, s, testUser1.ID), "")
	var q quotaDto
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if q.Limit != 50 {
		t.Errorf("limit = %d, хотели 50 для сборки без заголовка версии", q.Limit)
	}
}
