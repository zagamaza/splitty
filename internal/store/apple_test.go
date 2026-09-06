package store

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	appstore "github.com/awa/go-iap/appstore/api"
)

const (
	testBundle  = "com.zagir.splitty"
	testProduct = "com.zagir.splitty.plus.monthly"
)

// fakeAppleAPI подставляет разобранные транзакции вместо настоящего App Store.
//
// Подпись здесь не проверяется намеренно: её проверку делает go-iap (цепочка до
// Apple Root CA G3), подделать валидный чек в тесте невозможно, а проверять
// чужую библиотеку — не наша работа. Тесты ниже про НАШИ решения: чей bundle,
// какое окружение, тот ли продукт, какая транзакция свежее.
type fakeAppleAPI struct {
	tx        *appstore.JWSTransaction
	parseErr  error
	status    *appstore.StatusResponse
	statusErr error
	// byJWS позволяет отдавать разные транзакции на разные строки (нужно
	// Status, который разбирает несколько подписанных транзакций).
	byJWS map[string]*appstore.JWSTransaction
	calls int
}

func (f *fakeAppleAPI) ParseSignedTransaction(jws string) (*appstore.JWSTransaction, error) {
	f.calls++
	if f.parseErr != nil {
		return nil, f.parseErr
	}
	if tx, ok := f.byJWS[jws]; ok {
		return tx, nil
	}
	return f.tx, nil
}

func (f *fakeAppleAPI) GetALLSubscriptionStatuses(_ context.Context, _ string, _ *url.Values) (*appstore.StatusResponse, error) {
	f.calls++
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.status, nil
}

func validTx() *appstore.JWSTransaction {
	return &appstore.JWSTransaction{
		OriginalTransactionId: "orig-100",
		BundleID:              testBundle,
		ProductID:             testProduct,
		AppAccountToken:       "binding-1",
		ExpiresDate:           time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
		SignedDate:            time.Now().UnixMilli(),
		Environment:           appstore.Environment(api.EnvProduction),
	}
}

func prodApple(tx *appstore.JWSTransaction) (*Apple, *fakeAppleAPI) {
	fake := &fakeAppleAPI{tx: tx}
	a := newAppleWithAPI(AppleConfig{
		BundleID:           testBundle,
		AllowedEnvironment: api.EnvProduction,
		ProductIds:         []string{testProduct, "com.zagir.splitty.plus.yearly"},
	}, fake, fake)
	return a, fake
}

func TestAppleVerifyValidReceipt(t *testing.T) {
	a, _ := prodApple(validTx())

	got, err := a.Verify(context.Background(), "jws")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.StoreRef != "orig-100" {
		t.Errorf("StoreRef = %q", got.StoreRef)
	}
	if got.BindingToken != "binding-1" {
		t.Errorf("BindingToken = %q — без него чек не привязать к аккаунту", got.BindingToken)
	}
	if got.ProductId != testProduct {
		t.Errorf("ProductId = %q", got.ProductId)
	}
	if got.Revoked {
		t.Error("новая покупка помечена отозванной")
	}
	if got.ExpiresAt.IsZero() {
		t.Error("не разобран срок окончания")
	}
}

// TestAppleVerifyRejectsSandboxOnProduction — sandbox-чек не даёт Plus в проде.
//
// Sandbox-подписки бесплатны и продлеваются каждые несколько минут: принять
// такой чек значит раздать вечный Plus даром. Это не «странный чек», а рабочий
// способ обойти оплату.
func TestAppleVerifyRejectsSandboxOnProduction(t *testing.T) {
	tx := validTx()
	tx.Environment = appstore.Environment(api.EnvSandbox)
	a, _ := prodApple(tx)

	_, err := a.Verify(context.Background(), "jws")
	if !errors.Is(err, ErrWrongEnvironment) {
		t.Fatalf("хотели ErrWrongEnvironment, получили %v", err)
	}
}

// TestAppleVerifyRejects — чеки, которые не должны включать Plus.
func TestAppleVerifyRejects(t *testing.T) {
	foreignBundle := validTx()
	foreignBundle.BundleID = "com.someone.else"

	foreignProduct := validTx()
	foreignProduct.ProductID = "com.zagir.splitty.tip.small"

	tests := []struct {
		name    string
		tx      *appstore.JWSTransaction
		parse   error
		wantErr error
	}{
		{"битая подпись", nil, errors.New("signature mismatch"), ErrReceiptInvalid},
		{"чужой bundle id", foreignBundle, nil, ErrReceiptInvalid},
		{"продукт вне белого списка", foreignProduct, nil, ErrForeignProduct},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeAppleAPI{tx: tc.tx, parseErr: tc.parse}
			a := newAppleWithAPI(AppleConfig{
				BundleID:           testBundle,
				AllowedEnvironment: api.EnvProduction,
				ProductIds:         []string{testProduct},
			}, fake, fake)

			if _, err := a.Verify(context.Background(), "jws"); !errors.Is(err, tc.wantErr) {
				t.Errorf("хотели %v, получили %v", tc.wantErr, err)
			}
		})
	}
}

// TestAppleVerifyMarksRefundedReceipt — возврат виден прямо в чеке.
func TestAppleVerifyMarksRefundedReceipt(t *testing.T) {
	tx := validTx()
	tx.RevocationDate = time.Now().UnixMilli()
	a, _ := prodApple(tx)

	got, err := a.Verify(context.Background(), "jws")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.Revoked {
		t.Error("возврат не распознан — Plus остался бы после возврата денег")
	}
}

// TestAppleNewRequiresKeys — без ключей покупки просто выключены.
func TestAppleNewRequiresKeys(t *testing.T) {
	if _, err := NewApple(AppleConfig{BundleID: testBundle}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("хотели ErrNotConfigured, получили %v", err)
	}
}

// TestAppleStatusPicksNewestTransaction — из нескольких транзакций берётся
// САМАЯ СВЕЖАЯ по дате подписи.
//
// App Store возвращает историю, и порядок в ней не гарантирован: взять первую
// попавшуюся значит однажды записать продление задним числом и снять Plus у
// платящего.
func TestAppleStatusPicksNewestTransaction(t *testing.T) {
	now := time.Now()

	old := validTx()
	old.SignedDate = now.Add(-48 * time.Hour).UnixMilli()
	old.ExpiresDate = now.Add(-time.Hour).UnixMilli()

	fresh := validTx()
	fresh.SignedDate = now.UnixMilli()
	fresh.ExpiresDate = now.Add(30 * 24 * time.Hour).UnixMilli()

	fake := &fakeAppleAPI{
		byJWS: map[string]*appstore.JWSTransaction{"old": old, "fresh": fresh},
		status: &appstore.StatusResponse{
			Environment: appstore.Environment(api.EnvProduction),
			Data: []appstore.SubscriptionGroupIdentifierItem{{
				LastTransactions: []appstore.LastTransactionsItem{
					// Свежая идёт ПЕРВОЙ, протухшая второй: если код берёт
					// последнюю по порядку, тест это поймает.
					{OriginalTransactionId: "orig-100", Status: 1, SignedTransactionInfo: "fresh"},
					{OriginalTransactionId: "orig-100", Status: 2, SignedTransactionInfo: "old"},
				},
			}},
		},
	}
	a := newAppleWithAPI(AppleConfig{BundleID: testBundle, ProductIds: []string{testProduct}}, fake, fake)

	got, err := a.Status(context.Background(), "orig-100", api.EnvProduction)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.ExpiresAt.After(now) {
		t.Errorf("взята протухшая транзакция: ExpiresAt = %v", got.ExpiresAt)
	}
	if !got.AutoRenew {
		t.Error("AutoRenew = false, хотя статус свежей транзакции — активна")
	}
}

// TestAppleStatusUsesHostMatchingEnvironment — про sandbox-подписку спрашиваем
// у sandbox-хоста.
//
// Спросить у боевого значит получить 404, решить, что подписки нет, и снять
// Plus у настоящего платящего человека.
func TestAppleStatusUsesHostMatchingEnvironment(t *testing.T) {
	mkFake := func() *fakeAppleAPI {
		tx := validTx()
		return &fakeAppleAPI{
			byJWS: map[string]*appstore.JWSTransaction{"t": tx},
			status: &appstore.StatusResponse{
				Environment: appstore.Environment(api.EnvProduction),
				Data: []appstore.SubscriptionGroupIdentifierItem{{
					LastTransactions: []appstore.LastTransactionsItem{
						{OriginalTransactionId: "orig-100", Status: 1, SignedTransactionInfo: "t"},
					},
				}},
			},
		}
	}
	prod, sandbox := mkFake(), mkFake()
	a := newAppleWithAPI(AppleConfig{BundleID: testBundle, ProductIds: []string{testProduct}}, prod, sandbox)

	if _, err := a.Status(context.Background(), "orig-100", api.EnvSandbox); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if sandbox.calls == 0 {
		t.Error("про sandbox-подписку спросили не у sandbox-хоста")
	}
	if prod.calls != 0 {
		t.Error("про sandbox-подписку ходили на боевой хост — там её нет, и Plus сняли бы зря")
	}
}

// TestAppleStatusPropagatesStoreOutage — недоступность магазина возвращается
// ошибкой, а не «подписки нет».
//
// Разница принципиальная: «нет подписки» снимает Plus, ошибка — оставляет
// текущее состояние (см. вызывающий код).
func TestAppleStatusPropagatesStoreOutage(t *testing.T) {
	boom := errors.New("503 from apple")
	fake := &fakeAppleAPI{statusErr: boom}
	a := newAppleWithAPI(AppleConfig{BundleID: testBundle}, fake, fake)

	_, err := a.Status(context.Background(), "orig-100", api.EnvProduction)
	if err == nil {
		t.Fatal("недоступность магазина выдана за отсутствие подписки")
	}
	if errors.Is(err, ErrReceiptInvalid) {
		t.Error("сбой магазина принят за недействительный чек")
	}
}

// TestApplePrivateKeyFormats — ключ принимается и настоящим PEM, и одной
// строкой с экранированными \n, а битый отвергается сразу, а не у кассы.
func TestApplePrivateKeyFormats(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	escaped := strings.ReplaceAll(string(pemBytes), "\n", `\n`)

	cfg := func(content string) AppleConfig {
		return AppleConfig{KeyContent: []byte(content), KeyID: "K1", Issuer: "I1", BundleID: "com.example"}
	}

	if _, err := NewApple(cfg(string(pemBytes))); err != nil {
		t.Errorf("настоящий PEM отвергнут: %v", err)
	}
	if _, err := NewApple(cfg(escaped)); err != nil {
		t.Errorf("ключ с экранированными \\n отвергнут: %v", err)
	}
	if _, err := NewApple(cfg("не ключ вовсе")); err == nil {
		t.Error("битый ключ принят — сбой всплывёт только при покупке")
	}
}
