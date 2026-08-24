package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/almaznur91/splitty/internal/store"
	"go.mongodb.org/mongo-driver/mongo"
)

// fakeSubStore — подписки в памяти с тем же контрактом, что у mongo-репозитория.
type fakeSubStore struct {
	byRef map[string]*api.Subscription
	// supersedes фиксирует, что именно сделал хендлер: без этого нельзя
	// отличить «записал» от «записал и погасил предшественника».
	supersedes []string
	// rejectStale повторяет главное свойство настоящего репозитория —
	// уведомление старше уже применённого не откатывает состояние. Включается
	// в тестах, которые проверяют именно переупорядоченную доставку.
	rejectStale bool
}

func newFakeSubStore() *fakeSubStore {
	return &fakeSubStore{byRef: map[string]*api.Subscription{}}
}

func refKey(storeName, ref string) string { return storeName + "|" + ref }

func (f *fakeSubStore) Upsert(_ context.Context, s api.Subscription) error {
	key := refKey(s.Store, s.StoreRef)
	if existing, ok := f.byRef[key]; ok {
		if f.rejectStale && !s.LastNotifiedAt.IsZero() &&
			!existing.LastNotifiedAt.IsZero() && s.LastNotifiedAt.Before(existing.LastNotifiedAt) {
			return repository.ErrStaleNotification
		}
		// Возврат и замена не отменяются очередной записью — как в mongo.
		s.RevokedAt, s.SupersededAt = existing.RevokedAt, existing.SupersededAt
	}
	cp := s
	f.byRef[key] = &cp
	return nil
}

func (f *fakeSubStore) ActiveByUser(_ context.Context, userId int) ([]api.Subscription, error) {
	var out []api.Subscription
	for _, s := range f.byRef {
		if s.UserId == userId && s.RevokedAt == nil && s.SupersededAt == nil {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (f *fakeSubStore) ByStoreRef(_ context.Context, storeName, ref string) (*api.Subscription, error) {
	if s, ok := f.byRef[refKey(storeName, ref)]; ok {
		return s, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeSubStore) Supersede(_ context.Context, storeName, ref string, at time.Time) error {
	f.supersedes = append(f.supersedes, ref)
	if s, ok := f.byRef[refKey(storeName, ref)]; ok {
		moment := at
		s.SupersededAt = &moment
	}
	return nil
}

func (f *fakeSubStore) MarkRevoked(_ context.Context, storeName, ref string, at time.Time) error {
	if s, ok := f.byRef[refKey(storeName, ref)]; ok {
		moment := at
		s.RevokedAt = &moment
	}
	return nil
}

func (f *fakeSubStore) DeleteByUserId(_ context.Context, userId int) error {
	for key, s := range f.byRef {
		if s.UserId == userId {
			delete(f.byRef, key)
		}
	}
	return nil
}

func (f *fakeSubStore) SetAckState(_ context.Context, storeName, ref, state string) error {
	if s, ok := f.byRef[refKey(storeName, ref)]; ok {
		s.AckState = state
	}
	return nil
}

// fakeVerifier отдаёт заранее заданный чек или ошибку.
type fakeVerifier struct {
	receipt store.Receipt
	err     error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (store.Receipt, error) {
	if f.err != nil {
		return store.Receipt{}, f.err
	}
	return f.receipt, nil
}

type fakeAck struct {
	err   error
	calls int
}

func (f *fakeAck) Acknowledge(_ context.Context, _ string) error {
	f.calls++
	return f.err
}

func subServer(t *testing.T, subs *fakeSubStore, verifier ReceiptVerifier, ack PurchaseAcknowledger) *Server {
	t.Helper()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(newTestRoom()))
	s.SetEntitlements(service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
	}))
	s.SetSubscriptions(subs, verifier, verifier, ack)
	return s
}

func goodReceipt() store.Receipt {
	return store.Receipt{
		StoreRef:     "orig-1",
		ProductId:    "com.zagir.splitty.plus.monthly",
		BindingToken: "binding-" + strconv.Itoa(testUser1.ID),
		ExpiresAt:    time.Now().UTC().Add(30 * 24 * time.Hour),
		AutoRenew:    true,
		Environment:  api.EnvProduction,
	}
}

func postAppleReceipt(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodPost, "/api/v1/me/subscription/apple", token, body)
}

// TestSubscriptionAppleHappyPath — валидный чек делает человека платным.
func TestSubscriptionAppleHappyPath(t *testing.T) {
	subs := newFakeSubStore()
	s := subServer(t, subs, &fakeVerifier{receipt: goodReceipt()}, &fakeAck{})

	rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var dto subscriptionDto
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if dto.Tier != api.TierPlus {
		t.Errorf("tier = %q, хотели plus сразу после покупки", dto.Tier)
	}
	if dto.ManageUrl == "" {
		t.Error("не сказано, где отменять подписку — своей кнопки отмены у нас нет и быть не может")
	}
	if _, ok := subs.byRef[refKey(api.StoreApple, "orig-1")]; !ok {
		t.Error("подписка не записана")
	}
}

// TestSubscriptionRejectsReceiptOfOtherAccount — чужой чек не даёт Plus.
//
// Без этой проверки действует «чей чек — того, кто первый прислал»: утёкший или
// расшаренный чек забирает тот, кто успел раньше, а настоящий покупатель
// остаётся без Plus.
func TestSubscriptionRejectsReceiptOfOtherAccount(t *testing.T) {
	receipt := goodReceipt()
	receipt.BindingToken = "binding-999999"

	subs := newFakeSubStore()
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, &fakeAck{})

	rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}

	var env errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if env.Error.Code != errCodeReceiptForeign {
		t.Errorf("code = %q, want %q", env.Error.Code, errCodeReceiptForeign)
	}
	if env.Error.Message == "" {
		t.Error("нет объяснения, что делать человеку, который реально заплатил")
	}
	if len(subs.byRef) != 0 {
		t.Error("чужая подписка записана на этот аккаунт")
	}
}

// TestSubscriptionAcceptsReceiptWithoutBindingToken — покупки со старых сборок
// принимаются.
//
// Токен привязки шлют только сборки 1.7+. Отвергать чеки без него значило бы
// оставить без Plus тех, кто заплатил раньше.
func TestSubscriptionAcceptsReceiptWithoutBindingToken(t *testing.T) {
	receipt := goodReceipt()
	receipt.BindingToken = ""

	subs := newFakeSubStore()
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, &fakeAck{})

	rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, покупка со старой сборки отвергнута: %s", rec.Code, rec.Body.String())
	}
}

// TestSubscriptionWithoutBindingTokenDoesNotStealForeignPurchase — чек без
// токена привязки не уводит подписку у того, за кем она уже записана.
func TestSubscriptionWithoutBindingTokenDoesNotStealForeignPurchase(t *testing.T) {
	receipt := goodReceipt()
	receipt.BindingToken = ""

	subs := newFakeSubStore()
	subs.byRef[refKey(api.StoreApple, "orig-1")] = &api.Subscription{
		UserId: testUser2.ID, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, &fakeAck{})

	rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 — подписку увели у другого аккаунта", rec.Code)
	}
	if got := subs.byRef[refKey(api.StoreApple, "orig-1")].UserId; got != testUser2.ID {
		t.Errorf("владелец подписки сменился на %d", got)
	}
}

// TestSubscriptionSupersedesPreviousTokenOnPlanChange — смена месяц↔год в
// Google гасит предыдущий токен.
//
// Иначе на человеке остаются две «активные» подписки, и старая продолжает
// давать Plus даже после возврата денег по новой.
func TestSubscriptionSupersedesPreviousTokenOnPlanChange(t *testing.T) {
	receipt := goodReceipt()
	receipt.StoreRef = "tok-new"
	receipt.LinkedRef = "tok-old"

	subs := newFakeSubStore()
	subs.byRef[refKey(api.StoreGoogle, "tok-old")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreGoogle, StoreRef: "tok-old",
		ExpiresAt: time.Now().Add(300 * 24 * time.Hour),
	}
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, &fakeAck{})

	rec := doRequest(t, s, http.MethodPost, "/api/v1/me/subscription/google",
		mustToken(t, s, testUser1.ID), `{"purchaseToken":"tok-new","productId":"com.zagir.splitty.plus.yearly"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	if len(subs.supersedes) != 1 || subs.supersedes[0] != "tok-old" {
		t.Errorf("предшественник не погашен: %v", subs.supersedes)
	}
	active, _ := subs.ActiveByUser(context.Background(), testUser1.ID)
	if len(active) != 1 {
		t.Errorf("активных подписок %d, хотели 1: %+v", len(active), active)
	}
}

// TestSubscriptionKeepsPendingAckWhenAcknowledgeFails — сбой подтверждения не
// теряет покупку.
//
// Не подтвердив покупку за трое суток, Google откатит платёж. Состояние
// pending обязано пережить сбой, чтобы фоновый воркер довёл дело до конца.
func TestSubscriptionKeepsPendingAckWhenAcknowledgeFails(t *testing.T) {
	receipt := goodReceipt()
	receipt.StoreRef = "tok-1"
	receipt.NeedsAck = true

	subs := newFakeSubStore()
	ack := &fakeAck{err: errors.New("play down")}
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, ack)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/me/subscription/google",
		mustToken(t, s, testUser1.ID), `{"purchaseToken":"tok-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d — покупка должна сохраниться даже при сбое подтверждения", rec.Code)
	}
	if ack.calls != 1 {
		t.Errorf("вызовов подтверждения %d, хотели 1", ack.calls)
	}
	saved := subs.byRef[refKey(api.StoreGoogle, "tok-1")]
	if saved == nil || saved.AckState != api.AckPending {
		t.Errorf("состояние подтверждения = %v, хотели pending для ретрая", saved)
	}
}

// TestSubscriptionMarksAcknowledged — удачное подтверждение снимает pending.
func TestSubscriptionMarksAcknowledged(t *testing.T) {
	receipt := goodReceipt()
	receipt.StoreRef = "tok-1"
	receipt.NeedsAck = true

	subs := newFakeSubStore()
	s := subServer(t, subs, &fakeVerifier{receipt: receipt}, &fakeAck{})

	doRequest(t, s, http.MethodPost, "/api/v1/me/subscription/google",
		mustToken(t, s, testUser1.ID), `{"purchaseToken":"tok-1"}`)

	if got := subs.byRef[refKey(api.StoreGoogle, "tok-1")].AckState; got != api.AckDone {
		t.Errorf("ack_state = %q, хотели done", got)
	}
}

// TestSubscriptionReceiptErrors — отказы магазина переводятся в разные ответы.
func TestSubscriptionReceiptErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"битая подпись", store.ErrReceiptInvalid, http.StatusBadRequest, errCodeReceiptInvalid},
		{"чужой продукт", store.ErrForeignProduct, http.StatusBadRequest, errCodeReceiptInvalid},
		// Sandbox-чек на проде — попытка обойти оплату, а не сбой.
		{"sandbox на проде", store.ErrWrongEnvironment, http.StatusBadRequest, errCodeReceiptInvalid},
		// Недоступность магазина — НЕ «чек плохой»: клиенту надо повторить позже.
		{"магазин не отвечает", errors.New("503 from store"), http.StatusBadGateway, errCodeStoreUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			subs := newFakeSubStore()
			s := subServer(t, subs, &fakeVerifier{err: tc.err}, &fakeAck{})

			rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var env errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("разбор ответа: %v", err)
			}
			if env.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, tc.wantCode)
			}
			if len(subs.byRef) != 0 {
				t.Error("негодный чек записан как подписка")
			}
		})
	}
}

// TestSubscriptionDisabledWithoutStoreKeys — без ключей магазина эндпоинт
// отдаёт 503, а не молча делает вид, что всё хорошо.
func TestSubscriptionDisabledWithoutStoreKeys(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	s.SetSubscriptions(newFakeSubStore(), nil, nil, nil)

	rec := postAppleReceipt(t, s, mustToken(t, s, testUser1.ID), `{"jws":"signed"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestSubscriptionRequiresAuth — чужой или отсутствующий токен не проходит.
func TestSubscriptionRequiresAuth(t *testing.T) {
	s := subServer(t, newFakeSubStore(), &fakeVerifier{receipt: goodReceipt()}, &fakeAck{})

	if rec := postAppleReceipt(t, s, "", `{"jws":"signed"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("без токена status = %d, want 401", rec.Code)
	}
	if rec := postAppleReceipt(t, s, "garbage", `{"jws":"signed"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("с мусорным токеном status = %d, want 401", rec.Code)
	}
}

// TestMeExposesPurchaseBindingToken — токен привязки едет в профиле: он нужен
// клиенту ДО покупки, чтобы передать его магазину.
func TestMeExposesPurchaseBindingToken(t *testing.T) {
	s := subServer(t, newFakeSubStore(), &fakeVerifier{receipt: goodReceipt()}, &fakeAck{})

	rec := doRequest(t, s, http.MethodGet, "/api/v1/me", mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var me meDto
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if me.PurchaseBindingToken == "" {
		t.Error("токен привязки не отдан — покупку будет нечем привязать к аккаунту")
	}
}

