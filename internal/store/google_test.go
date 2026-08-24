package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	androidpublisher "google.golang.org/api/androidpublisher/v3"
)

const testPackage = "com.zagir.splitty"

type fakeGoogleAPI struct {
	purchase  *androidpublisher.SubscriptionPurchaseV2
	verifyErr error
	ackErr    error
	ackCalls  int
}

func (f *fakeGoogleAPI) VerifySubscriptionV2(_ context.Context, _, _ string) (*androidpublisher.SubscriptionPurchaseV2, error) {
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return f.purchase, nil
}

func (f *fakeGoogleAPI) AcknowledgeSubscription(_ context.Context, _, _, _ string, _ *androidpublisher.SubscriptionPurchasesAcknowledgeRequest) error {
	f.ackCalls++
	return f.ackErr
}

func activePurchase() *androidpublisher.SubscriptionPurchaseV2 {
	return &androidpublisher.SubscriptionPurchaseV2{
		SubscriptionState:    subStateActive,
		AcknowledgementState: "ACKNOWLEDGEMENT_STATE_ACKNOWLEDGED",
		StartTime:            time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		LineItems: []*androidpublisher.SubscriptionPurchaseLineItem{{
			ProductId:  testProduct,
			ExpiryTime: time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		}},
		ExternalAccountIdentifiers: &androidpublisher.ExternalAccountIdentifiers{
			ObfuscatedExternalAccountId: "binding-1",
		},
	}
}

func prodGoogle(p *androidpublisher.SubscriptionPurchaseV2) (*Google, *fakeGoogleAPI) {
	fake := &fakeGoogleAPI{purchase: p}
	g := newGoogleWithAPI(GoogleConfig{
		PackageName:        testPackage,
		ProductIds:         []string{testProduct, "com.zagir.splitty.plus.yearly"},
		AllowedEnvironment: api.EnvProduction,
	}, fake)
	return g, fake
}

func TestGoogleVerifyValidPurchase(t *testing.T) {
	g, _ := prodGoogle(activePurchase())

	got, err := g.Verify(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.StoreRef != "tok-1" {
		t.Errorf("StoreRef = %q", got.StoreRef)
	}
	if got.BindingToken != "binding-1" {
		t.Errorf("BindingToken = %q — без него покупку не привязать к аккаунту", got.BindingToken)
	}
	if !got.AutoRenew {
		t.Error("активная подписка помечена без автопродления")
	}
	if got.NeedsAck {
		t.Error("подтверждённая покупка помечена ожидающей подтверждения")
	}
}

// TestGoogleVerifyRejectsTestPurchaseOnProduction — тестовая покупка
// лицензированного тестировщика не даёт Plus в проде: она бесплатна.
func TestGoogleVerifyRejectsTestPurchaseOnProduction(t *testing.T) {
	p := activePurchase()
	p.TestPurchase = &androidpublisher.TestPurchase{}
	g, _ := prodGoogle(p)

	if _, err := g.Verify(context.Background(), "tok-1"); !errors.Is(err, ErrWrongEnvironment) {
		t.Fatalf("хотели ErrWrongEnvironment, получили %v", err)
	}
}

// TestGoogleVerifyRejectsForeignProduct — продукт берётся из ответа Play, а не
// со слов клиента, и сверяется с белым списком.
func TestGoogleVerifyRejectsForeignProduct(t *testing.T) {
	p := activePurchase()
	p.LineItems[0].ProductId = "com.zagir.splitty.tip.small"
	g, _ := prodGoogle(p)

	if _, err := g.Verify(context.Background(), "tok-1"); !errors.Is(err, ErrForeignProduct) {
		t.Fatalf("хотели ErrForeignProduct, получили %v", err)
	}
}

// TestGoogleVerifyReportsLinkedToken — при смене плана Play выдаёт НОВЫЙ токен
// и кладёт старый в linkedPurchaseToken.
//
// Без этой связи смена месяц↔год плодит вторую запись, и старая продолжает
// держать Plus даже после возврата денег по новой.
func TestGoogleVerifyReportsLinkedToken(t *testing.T) {
	p := activePurchase()
	p.LinkedPurchaseToken = "tok-old"
	g, _ := prodGoogle(p)

	got, err := g.Verify(context.Background(), "tok-new")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.LinkedRef != "tok-old" {
		t.Errorf("LinkedRef = %q, хотели tok-old", got.LinkedRef)
	}
}

// TestGoogleVerifyFlagsPendingAcknowledgement — неподтверждённая покупка
// помечается: Google откатит её через трое суток и вернёт деньги.
func TestGoogleVerifyFlagsPendingAcknowledgement(t *testing.T) {
	p := activePurchase()
	p.AcknowledgementState = ackStateNotAcked
	g, _ := prodGoogle(p)

	got, err := g.Verify(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.NeedsAck {
		t.Error("неподтверждённая покупка не помечена — деньги вернутся через трое суток")
	}
}

// TestGoogleVerifyKeepsPlusWhileCanceledButPaid — отменённая подписка доживает
// оплаченный срок.
//
// Отмена автопродления и возврат денег — разные вещи: выгонять человека из Plus
// сразу после отмены значит отобрать уже оплаченное.
func TestGoogleVerifyKeepsPlusWhileCanceledButPaid(t *testing.T) {
	p := activePurchase()
	p.SubscriptionState = "SUBSCRIPTION_STATE_CANCELED"
	g, _ := prodGoogle(p)

	got, err := g.Verify(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Revoked {
		t.Error("отмена автопродления принята за возврат денег")
	}
	if !got.ExpiresAt.After(time.Now()) {
		t.Error("потерян оплаченный срок")
	}
}

func TestGoogleAcknowledge(t *testing.T) {
	tests := []struct {
		name    string
		ackErr  error
		wantErr bool
	}{
		{"успех", nil, false},
		// Повтор — норма: подтверждение ретраится фоновым воркером, и уже
		// подтверждённая покупка не должна выглядеть сбоем и ретраиться вечно.
		{"уже подтверждена", errors.New("400: The subscription purchase has already been acknowledged."), false},
		{"магазин недоступен", errors.New("503 backend error"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGoogleAPI{ackErr: tc.ackErr}
			g := newGoogleWithAPI(GoogleConfig{PackageName: testPackage}, fake)

			err := g.Acknowledge(context.Background(), "tok-1")
			if tc.wantErr && err == nil {
				t.Error("сбой подтверждения проглочен — покупка останется неподтверждённой")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("неожиданная ошибка: %v", err)
			}
			if fake.ackCalls != 1 {
				t.Errorf("вызовов подтверждения %d, хотели 1", fake.ackCalls)
			}
		})
	}
}

// TestGoogleVerifyPropagatesOutage — недоступность Play возвращается ошибкой, а
// не «покупки нет»: иначе таймаут снял бы Plus у платящего.
func TestGoogleVerifyPropagatesOutage(t *testing.T) {
	fake := &fakeGoogleAPI{verifyErr: errors.New("503 backend error")}
	g := newGoogleWithAPI(GoogleConfig{PackageName: testPackage, ProductIds: []string{testProduct}}, fake)

	_, err := g.Verify(context.Background(), "tok-1")
	if err == nil {
		t.Fatal("недоступность Play выдана за отсутствие покупки")
	}
	if errors.Is(err, ErrForeignProduct) || errors.Is(err, ErrReceiptInvalid) {
		t.Errorf("сбой магазина принят за негодную покупку: %v", err)
	}
}

func TestGoogleNewRequiresConfig(t *testing.T) {
	if _, err := NewGoogle(context.Background(), GoogleConfig{PackageName: testPackage}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("хотели ErrNotConfigured, получили %v", err)
	}
}
