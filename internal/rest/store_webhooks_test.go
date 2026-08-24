package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/almaznur91/splitty/internal/store"
)

// fakeStatusReader — ответ магазина на перезапрос состояния.
type fakeStatusReader struct {
	receipt store.Receipt
	err     error
	calls   int
}

func (f *fakeStatusReader) Status(_ context.Context, _, _ string) (store.Receipt, error) {
	f.calls++
	if f.err != nil {
		return store.Receipt{}, f.err
	}
	return f.receipt, nil
}

// googleStatusAdapter — у Google подпись Status без окружения.
type googleStatusAdapter struct{ inner *fakeStatusReader }

func (g *googleStatusAdapter) Status(ctx context.Context, token string) (store.Receipt, error) {
	return g.inner.Status(ctx, token, "")
}

// webhookServer собирает сервер с подписками и перезапросом состояния.
func webhookServer(t *testing.T, subs *fakeSubStore, status *fakeStatusReader, ack PurchaseAcknowledger) *Server {
	t.Helper()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(newTestRoom()))
	s.SetEntitlements(service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
	}))
	s.SetSubscriptions(subs, &fakeVerifier{}, &fakeVerifier{}, ack)
	s.SetStoreWebhooks(status, &googleStatusAdapter{inner: status})
	return s
}

// jwsWith собирает JWS-подобную строку: подпись не проверяется намеренно —
// содержимому уведомления мы не верим, из него берётся только идентификатор
// покупки (см. store_webhooks.go).
func jwsWith(t *testing.T, payload interface{}) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(raw) + ".signature"
}

func appleWebhookBody(t *testing.T, notifType, ref, env string, signedAt time.Time) string {
	t.Helper()
	txJWS := jwsWith(t, map[string]string{"originalTransactionId": ref})
	payload := map[string]interface{}{
		"notificationType": notifType,
		"signedDate":       signedAt.UnixMilli(),
		"data": map[string]string{
			"environment":           env,
			"signedTransactionInfo": txJWS,
		},
	}
	body, err := json.Marshal(map[string]string{"signedPayload": jwsWith(t, payload)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

func googleWebhookBody(t *testing.T, note map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body, err := json.Marshal(map[string]interface{}{
		"message": map[string]string{
			"data":      base64.StdEncoding.EncodeToString(raw),
			"messageId": "1",
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

func seedSub(subs *fakeSubStore, storeName, ref string, expires time.Time, notified time.Time) {
	subs.byRef[refKey(storeName, ref)] = &api.Subscription{
		UserId: testUser1.ID, Store: storeName, StoreRef: ref,
		ExpiresAt: expires, AutoRenew: true, Environment: api.EnvProduction,
		LastNotifiedAt: notified,
	}
}

// TestAppleWebhookSyncsFromStoreNotPayload — состояние берётся у магазина, а не
// из уведомления.
//
// Проверяется буквально: уведомление называется EXPIRED, а магазин отвечает
// «активна до через месяц» — записаться обязано второе.
func TestAppleWebhookSyncsFromStoreNotPayload(t *testing.T) {
	subs := newFakeSubStore()
	seedSub(subs, api.StoreApple, "orig-1", time.Now().Add(24*time.Hour), time.Time{})

	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	status := &fakeStatusReader{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: future, AutoRenew: true, Environment: api.EnvProduction,
	}}
	s := webhookServer(t, subs, status, &fakeAck{})

	body := appleWebhookBody(t, "EXPIRED", "orig-1", api.EnvProduction, time.Now())
	rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — иначе Apple будет ретраить вечно", rec.Code)
	}
	if status.calls != 1 {
		t.Fatalf("обращений к магазину %d, хотели 1: состояние обязано браться у него", status.calls)
	}

	saved := subs.byRef[refKey(api.StoreApple, "orig-1")]
	if !saved.ExpiresAt.Equal(future) {
		t.Errorf("записан срок %v вместо ответа магазина %v", saved.ExpiresAt, future)
	}
}

// TestAppleWebhookIgnoresStaleNotification — опоздавшее уведомление не гасит
// действующую подписку.
//
// Доставка приходит не по порядку, и EXPIRED, подписанный ДО продления, вполне
// может прилететь после него. Применённый как есть, он снял бы Plus у человека,
// который только что заплатил.
func TestAppleWebhookIgnoresStaleNotification(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeSubStore()
	// Продление уже применено, отметка — «сейчас».
	seedSub(subs, api.StoreApple, "orig-1", now.Add(30*24*time.Hour), now)
	// Наш фейк повторяет главное свойство настоящего репозитория.
	subs.rejectStale = true

	status := &fakeStatusReader{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: now.Add(-time.Hour), AutoRenew: false, Environment: api.EnvProduction,
	}}
	s := webhookServer(t, subs, status, &fakeAck{})

	// Уведомление подписано ЧАСОМ РАНЬШЕ применённого.
	body := appleWebhookBody(t, "EXPIRED", "orig-1", api.EnvProduction, now.Add(-time.Hour))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	saved := subs.byRef[refKey(api.StoreApple, "orig-1")]
	if !saved.ExpiresAt.After(now) {
		t.Errorf("опоздавшее уведомление погасило действующую подписку: ExpiresAt = %v", saved.ExpiresAt)
	}
}

// TestWebhookKeepsStateWhenStoreUnavailable — недоступность магазина не
// отбирает Plus.
//
// Оставить лишний доступ на время лучше, чем выгнать платящего из-за таймаута:
// уведомление придёт снова, а не придёт — досинхронизирует фоновый воркер.
func TestWebhookKeepsStateWhenStoreUnavailable(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeSubStore()
	seedSub(subs, api.StoreApple, "orig-1", now.Add(30*24*time.Hour), time.Time{})

	status := &fakeStatusReader{err: errors.New("503 from apple")}
	s := webhookServer(t, subs, status, &fakeAck{})

	body := appleWebhookBody(t, "DID_RENEW", "orig-1", api.EnvProduction, now)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if saved := subs.byRef[refKey(api.StoreApple, "orig-1")]; !saved.ExpiresAt.After(now) {
		t.Error("таймаут магазина снял подписку")
	}
}

// TestWebhookUnknownPurchaseIsAccepted — уведомление о покупке, которой у нас
// нет, не считается ошибкой.
//
// Так бывает законно: человек оплатил и закрыл приложение, чек до сервера не
// дошёл. Ответить не-200 значит заставить магазин ретраить вечно.
func TestWebhookUnknownPurchaseIsAccepted(t *testing.T) {
	subs := newFakeSubStore()
	status := &fakeStatusReader{}
	s := webhookServer(t, subs, status, &fakeAck{})

	body := appleWebhookBody(t, "DID_RENEW", "orig-unknown", api.EnvProduction, time.Now())
	rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if status.calls != 0 {
		t.Error("ходили в магазин за покупкой, которую не к кому привязать")
	}
}

// TestGoogleVoidedPurchaseRevokesPlus — возврат снимает Plus.
//
// ⚠️ Возвраты приходят ТОЛЬКО в voidedPurchaseNotification, отдельным типом
// уведомления. В subscriptionNotification их нет, и ждать их там бесполезно.
func TestGoogleVoidedPurchaseRevokesPlus(t *testing.T) {
	subs := newFakeSubStore()
	seedSub(subs, api.StoreGoogle, "tok-1", time.Now().Add(300*24*time.Hour), time.Time{})
	s := webhookServer(t, subs, &fakeStatusReader{}, &fakeAck{})

	body := googleWebhookBody(t, map[string]interface{}{
		"version":         "1.0",
		"packageName":     "com.zagir.splitty",
		"eventTimeMillis": "1756000000000",
		"voidedPurchaseNotification": map[string]interface{}{
			"purchaseToken": "tok-1",
			"orderId":       "GPA.1",
			"productType":   1,
			"refundType":    1,
		},
	})
	rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/google", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if subs.byRef[refKey(api.StoreGoogle, "tok-1")].RevokedAt == nil {
		t.Error("возврат не снял Plus")
	}
	active, _ := subs.ActiveByUser(context.Background(), testUser1.ID)
	if len(active) != 0 {
		t.Errorf("после возврата осталось %d активных подписок", len(active))
	}
}

// TestGoogleWebhookAcknowledgesPendingPurchase — вебхук добивает подтверждение,
// если клиент до сервера не дошёл.
//
// Без подтверждения Google откатит покупку через трое суток и вернёт деньги.
func TestGoogleWebhookAcknowledgesPendingPurchase(t *testing.T) {
	subs := newFakeSubStore()
	seedSub(subs, api.StoreGoogle, "tok-1", time.Now().Add(24*time.Hour), time.Time{})

	status := &fakeStatusReader{receipt: store.Receipt{
		StoreRef: "tok-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour), AutoRenew: true,
		Environment: api.EnvProduction, NeedsAck: true,
	}}
	ack := &fakeAck{}
	s := webhookServer(t, subs, status, ack)

	body := googleWebhookBody(t, map[string]interface{}{
		"version":         "1.0",
		"eventTimeMillis": "1756000000000",
		"subscriptionNotification": map[string]interface{}{
			"version":          "1.0",
			"notificationType": 4,
			"purchaseToken":    "tok-1",
			"subscriptionId":   "com.zagir.splitty.plus.monthly",
		},
	})
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/google", "", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ack.calls != 1 {
		t.Errorf("вызовов подтверждения %d, хотели 1 — иначе деньги вернутся через трое суток", ack.calls)
	}
}

// TestGoogleTestNotificationAccepted — проверочное уведомление из Play Console
// принимается: им проверяют настройку доставки.
func TestGoogleTestNotificationAccepted(t *testing.T) {
	s := webhookServer(t, newFakeSubStore(), &fakeStatusReader{}, &fakeAck{})

	body := googleWebhookBody(t, map[string]interface{}{
		"version":          "1.0",
		"testNotification": map[string]string{"version": "1.0"},
	})
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/google", "", body); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — Play Console решит, что доставка не настроена", rec.Code)
	}
}

// TestWebhooksRejectMalformedBodies — мусор отвергается 400: повторная доставка
// его не исправит.
func TestWebhooksRejectMalformedBodies(t *testing.T) {
	s := webhookServer(t, newFakeSubStore(), &fakeStatusReader{}, &fakeAck{})

	tests := []struct {
		name string
		path string
		body string
	}{
		{"apple: не json", "/api/v1/webhooks/apple", "не json"},
		{"apple: пустой payload", "/api/v1/webhooks/apple", `{"signedPayload":""}`},
		{"apple: payload не jws", "/api/v1/webhooks/apple", `{"signedPayload":"мусор"}`},
		{"google: не json", "/api/v1/webhooks/google", "не json"},
		{"google: не base64", "/api/v1/webhooks/google", `{"message":{"data":"!!!не base64!!!"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPost, tc.path, "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestWebhooksNeedNoAuth — уведомления приходят без нашего JWT.
//
// Аутентифицирует их не заголовок, а то, что состояние мы перезапрашиваем у
// магазина по своему ключу: подделанное уведомление в худшем случае заставит
// сходить в магазин лишний раз.
func TestWebhooksNeedNoAuth(t *testing.T) {
	subs := newFakeSubStore()
	seedSub(subs, api.StoreApple, "orig-1", time.Now().Add(24*time.Hour), time.Time{})
	status := &fakeStatusReader{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour), Environment: api.EnvProduction,
	}}
	s := webhookServer(t, subs, status, &fakeAck{})

	body := appleWebhookBody(t, "DID_RENEW", "orig-1", api.EnvProduction, time.Now())
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body); rec.Code == http.StatusUnauthorized {
		t.Error("вебхук требует авторизации — магазин её не пришлёт никогда")
	}
}

// TestWebhookRepeatedDeliveryIsIdempotent — повторная доставка не ломает
// состояние. Оба магазина шлют одно событие несколько раз, это норма.
func TestWebhookRepeatedDeliveryIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeSubStore()
	seedSub(subs, api.StoreApple, "orig-1", now.Add(24*time.Hour), time.Time{})

	future := now.Add(30 * 24 * time.Hour)
	status := &fakeStatusReader{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: future, AutoRenew: true, Environment: api.EnvProduction,
	}}
	s := webhookServer(t, subs, status, &fakeAck{})

	body := appleWebhookBody(t, "DID_RENEW", "orig-1", api.EnvProduction, now)
	for i := 0; i < 3; i++ {
		if rec := doRequest(t, s, http.MethodPost, "/api/v1/webhooks/apple", "", body); rec.Code != http.StatusOK {
			t.Fatalf("доставка %d: status = %d", i+1, rec.Code)
		}
	}

	if len(subs.byRef) != 1 {
		t.Errorf("повторная доставка завела %d документов", len(subs.byRef))
	}
	if saved := subs.byRef[refKey(api.StoreApple, "orig-1")]; !saved.ExpiresAt.Equal(future) {
		t.Errorf("состояние поехало: %v", saved.ExpiresAt)
	}
}
