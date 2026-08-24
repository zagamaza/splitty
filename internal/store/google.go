package store

import (
	"context"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/awa/go-iap/playstore"
	"github.com/pkg/errors"
	androidpublisher "google.golang.org/api/androidpublisher/v3"
)

// Состояния подписки Google, которые нас различают.
// https://developer.android.com/google/play/billing/subscriptions
const (
	subStateActive   = "SUBSCRIPTION_STATE_ACTIVE"
	subStateInGrace  = "SUBSCRIPTION_STATE_IN_GRACE_PERIOD"
	subStateExpired  = "SUBSCRIPTION_STATE_EXPIRED"
	ackStateNotAcked = "ACKNOWLEDGEMENT_STATE_PENDING"
)

// googleAPI — то, что нужно от Play Developer API. Интерфейс объявлен здесь,
// чтобы тесты проверяли нашу логику без сети и без живого биллинга.
type googleAPI interface {
	VerifySubscriptionV2(ctx context.Context, packageName, token string) (*androidpublisher.SubscriptionPurchaseV2, error)
	AcknowledgeSubscription(ctx context.Context, packageName, subscriptionID, token string, req *androidpublisher.SubscriptionPurchasesAcknowledgeRequest) error
}

// GoogleConfig — доступ к Play Developer API.
type GoogleConfig struct {
	// ServiceAccountJSON — содержимое ключа сервисного аккаунта.
	ServiceAccountJSON []byte
	PackageName        string
	ProductIds         []string
	// AllowedEnvironment — "Production" отвергает тестовые покупки
	// лицензированных тестировщиков: они бесплатны, как и sandbox у Apple.
	AllowedEnvironment string
}

// Google проверяет покупки Google Play.
type Google struct {
	api googleAPI
	cfg GoogleConfig
}

func NewGoogle(ctx context.Context, cfg GoogleConfig) (*Google, error) {
	if len(cfg.ServiceAccountJSON) == 0 || cfg.PackageName == "" {
		return nil, ErrNotConfigured
	}
	client, err := playstore.New(cfg.ServiceAccountJSON)
	if err != nil {
		return nil, errors.Wrap(err, "play developer api")
	}
	return &Google{api: client, cfg: cfg}, nil
}

// newGoogleWithAPI собирает проверяльщик поверх подставного клиента (тесты).
func newGoogleWithAPI(cfg GoogleConfig, api googleAPI) *Google {
	return &Google{api: api, cfg: cfg}
}

// Verify проверяет покупку по её токену.
//
// productId приходит от клиента и служит лишь подсказкой: настоящий продукт
// берётся из ответа Play. Верить клиенту тут нельзя — иначе достаточно назвать
// свой чек нужным продуктом.
func (g *Google) Verify(ctx context.Context, token string) (Receipt, error) {
	purchase, err := g.api.VerifySubscriptionV2(ctx, g.cfg.PackageName, token)
	if err != nil {
		return Receipt{}, errors.Wrap(err, "play developer api")
	}
	return g.toReceipt(token, purchase)
}

// Status перезапрашивает состояние покупки. У Google это тот же вызов, что и
// проверка: покупка и есть её текущее состояние — отдельного «статуса» нет.
func (g *Google) Status(ctx context.Context, token string) (Receipt, error) {
	return g.Verify(ctx, token)
}

// Acknowledge подтверждает покупку.
//
// ⚠️ Не подтвердив покупку за трое суток, Google откатывает её и возвращает
// деньги. Поэтому подтверждение — состояние с ретраем, а не одиночный вызов;
// здесь оно идемпотентно: уже подтверждённая покупка ошибкой не считается.
func (g *Google) Acknowledge(ctx context.Context, token string) error {
	err := g.api.AcknowledgeSubscription(ctx, g.cfg.PackageName, "", token,
		&androidpublisher.SubscriptionPurchasesAcknowledgeRequest{})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		return nil
	}
	return errors.Wrap(err, "play acknowledge")
}

func (g *Google) toReceipt(token string, p *androidpublisher.SubscriptionPurchaseV2) (Receipt, error) {
	if p == nil {
		return Receipt{}, ErrReceiptInvalid
	}

	env := api.EnvProduction
	if p.TestPurchase != nil {
		env = api.EnvSandbox
	}
	if g.cfg.AllowedEnvironment != "" && env != g.cfg.AllowedEnvironment {
		return Receipt{}, errors.Wrapf(ErrWrongEnvironment,
			"окружение покупки %q, принимается только %q", env, g.cfg.AllowedEnvironment)
	}

	productId, expiresAt := "", time.Time{}
	for _, item := range p.LineItems {
		if item == nil {
			continue
		}
		// Из нескольких позиций берём самую позднюю: именно она определяет,
		// до какого момента подписка действует.
		if item.ExpiryTime != "" {
			if ts, err := time.Parse(time.RFC3339, item.ExpiryTime); err == nil && ts.After(expiresAt) {
				expiresAt = ts.UTC()
				productId = item.ProductId
			}
		}
		if productId == "" {
			productId = item.ProductId
		}
	}
	if !productAllowed(productId, g.cfg.ProductIds) {
		return Receipt{}, errors.Wrapf(ErrForeignProduct, "продукт %q", productId)
	}

	binding := ""
	if p.ExternalAccountIdentifiers != nil {
		binding = p.ExternalAccountIdentifiers.ObfuscatedExternalAccountId
	}

	linked := ""
	if p.LinkedPurchaseToken != "" && p.LinkedPurchaseToken != token {
		linked = p.LinkedPurchaseToken
	}

	return Receipt{
		StoreRef:     token,
		LinkedRef:    linked,
		ProductId:    productId,
		BindingToken: binding,
		ExpiresAt:    expiresAt,
		AutoRenew:    p.SubscriptionState == subStateActive || p.SubscriptionState == subStateInGrace,
		Environment:  env,
		// Отзыв: деньги вернули. On hold и canceled Plus НЕ снимают — подписка
		// доживает оплаченный срок, это разные вещи.
		Revoked:  p.SubscriptionState == subStateExpired && p.CanceledStateContext != nil && p.CanceledStateContext.SystemInitiatedCancellation != nil,
		NeedsAck: p.AcknowledgementState == ackStateNotAcked,
		SignedAt: parseRFC3339(p.StartTime),
	}, nil
}

// isAlreadyAcknowledged — Play отвечает 400 на повторное подтверждение.
// Для нас это успех: цель достигнута, деньги не откатятся.
func isAlreadyAcknowledged(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already been acknowledged") ||
		strings.Contains(msg, "alreadyAcknowledged")
}

func parseRFC3339(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}
