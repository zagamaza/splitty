package store

import (
	"context"
	"net/url"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	appstore "github.com/awa/go-iap/appstore/api"
	"github.com/pkg/errors"
)

// AppleConfig — доступ к App Store Server API.
//
// ⚠️ KeyID/Issuer — ключ In-App Purchase, ОТДЕЛЬНЫЙ и от ключа выкладки сборок,
// и от ключа Sign in with Apple. KeyContent — содержимое .p8, не путь.
type AppleConfig struct {
	KeyContent []byte
	KeyID      string
	Issuer     string
	BundleID   string
	// AllowedEnvironment — какие чеки принимает этот инстанс ("Production" или
	// "Sandbox"). Пусто — принимаются любые (только для локальной разработки).
	AllowedEnvironment string
	ProductIds         []string
}

// appleAPI — то, что нужно от App Store Server API.
//
// Интерфейс объявлен здесь, чтобы тесты проверяли НАШУ логику (bundle id,
// окружение, белый список продуктов, выбор свежайшей транзакции) без похода в
// сеть и без подделки настоящей подписи Apple. Саму криптографию проверяет
// go-iap: цепочка сертификатов до корневого Apple Root CA G3 разбирается в
// StoreClient.parseJWS, и подменять её тут нечем и незачем.
type appleAPI interface {
	ParseSignedTransaction(jws string) (*appstore.JWSTransaction, error)
	GetALLSubscriptionStatuses(ctx context.Context, originalTransactionId string, query *url.Values) (*appstore.StatusResponse, error)
}

// Apple проверяет чеки App Store.
//
// Клиентов ДВА — боевой и песочница. Хост App Store Server API выбирается по
// окружению самого чека: спросить про sandbox-подписку у боевого хоста значит
// получить 404 и решить, что подписки нет, — то есть снять Plus у платящего.
type Apple struct {
	prod    appleAPI
	sandbox appleAPI
	cfg     AppleConfig
}

// NewApple собирает проверяльщик. Пустой ключ — ErrNotConfigured: покупки
// выключены, эндпоинты подписки отдают 503, все считаются бесплатными.
func NewApple(cfg AppleConfig) (*Apple, error) {
	if len(cfg.KeyContent) == 0 || cfg.KeyID == "" || cfg.Issuer == "" {
		return nil, ErrNotConfigured
	}
	base := appstore.StoreConfig{
		KeyContent: cfg.KeyContent,
		KeyID:      cfg.KeyID,
		BundleID:   cfg.BundleID,
		Issuer:     cfg.Issuer,
	}
	prodCfg, sandboxCfg := base, base
	sandboxCfg.Sandbox = true

	return &Apple{
		prod:    appstore.NewStoreClient(&prodCfg),
		sandbox: appstore.NewStoreClient(&sandboxCfg),
		cfg:     cfg,
	}, nil
}

// newAppleWithAPI собирает проверяльщик поверх подставного клиента (тесты).
func newAppleWithAPI(cfg AppleConfig, prod, sandbox appleAPI) *Apple {
	return &Apple{prod: prod, sandbox: sandbox, cfg: cfg}
}

// Verify проверяет подписанную транзакцию StoreKit 2.
//
// Порядок проверок — от дешёвого к дорогому и от общего к частному: подпись
// (её подделка обесценивает всё остальное) → bundle id → окружение → продукт.
func (a *Apple) Verify(_ context.Context, jws string) (Receipt, error) {
	tx, err := a.prod.ParseSignedTransaction(jws)
	if err != nil {
		return Receipt{}, errors.Wrap(ErrReceiptInvalid, err.Error())
	}
	if a.cfg.BundleID != "" && tx.BundleID != a.cfg.BundleID {
		return Receipt{}, errors.Wrapf(ErrReceiptInvalid, "чужой bundle id %q", tx.BundleID)
	}

	env := string(tx.Environment)
	if err := a.checkEnvironment(env); err != nil {
		return Receipt{}, err
	}
	if !productAllowed(tx.ProductID, a.cfg.ProductIds) {
		return Receipt{}, errors.Wrapf(ErrForeignProduct, "продукт %q", tx.ProductID)
	}

	return Receipt{
		StoreRef:     tx.OriginalTransactionId,
		ProductId:    tx.ProductID,
		BindingToken: tx.AppAccountToken,
		ExpiresAt:    msToTime(tx.ExpiresDate),
		// Сам чек не сообщает, включено ли автопродление: это отдельный
		// признак из renewalInfo. Достоверный ответ даёт Status.
		AutoRenew:   true,
		Environment: env,
		Revoked:     tx.RevocationDate != 0,
		SignedAt:    msToTime(tx.SignedDate),
	}, nil
}

// Status спрашивает у App Store текущее состояние подписки.
//
// Нужен там, где свежего чека нет: продление через месяц, страховка от
// потерянного уведомления, проверка после возврата. Одной Verify для этого мало
// — подписанная транзакция есть только в момент покупки.
func (a *Apple) Status(ctx context.Context, originalTransactionId, environment string) (Receipt, error) {
	client := a.clientFor(environment)

	resp, err := client.GetALLSubscriptionStatuses(ctx, originalTransactionId, nil)
	if err != nil {
		return Receipt{}, errors.Wrap(err, "app store server api")
	}

	var (
		latest    Receipt
		haveAny   bool
		latestSig time.Time
	)
	for _, group := range resp.Data {
		for _, item := range group.LastTransactions {
			tx, parseErr := client.ParseSignedTransaction(item.SignedTransactionInfo)
			if parseErr != nil {
				return Receipt{}, errors.Wrap(ErrReceiptInvalid, parseErr.Error())
			}
			signed := msToTime(tx.SignedDate)
			if haveAny && !signed.After(latestSig) {
				continue
			}
			haveAny, latestSig = true, signed
			latest = Receipt{
				StoreRef:     tx.OriginalTransactionId,
				ProductId:    tx.ProductID,
				BindingToken: tx.AppAccountToken,
				ExpiresAt:    msToTime(tx.ExpiresDate),
				// Статусы 1 и 4 (активна и в льготном периоде) означают, что
				// магазин намерен продлевать; 2, 3, 5 — истекла, в billing
				// retry, отозвана.
				AutoRenew:   item.Status == 1 || item.Status == 4,
				Environment: string(resp.Environment),
				Revoked:     tx.RevocationDate != 0,
				SignedAt:    signed,
			}
		}
	}
	if !haveAny {
		return Receipt{}, ErrReceiptInvalid
	}
	return latest, nil
}

// checkEnvironment отвергает чек чужого окружения.
func (a *Apple) checkEnvironment(env string) error {
	if a.cfg.AllowedEnvironment == "" || env == "" {
		return nil
	}
	if env != a.cfg.AllowedEnvironment {
		return errors.Wrapf(ErrWrongEnvironment, "окружение чека %q, принимается только %q", env, a.cfg.AllowedEnvironment)
	}
	return nil
}

// clientFor выбирает хост по окружению подписки.
func (a *Apple) clientFor(environment string) appleAPI {
	if environment == api.EnvSandbox {
		return a.sandbox
	}
	return a.prod
}

// msToTime переводит миллисекунды Apple в время. Ноль — «даты нет»
// (нулевой time.Time), а не 1970 год: пожизненная покупка без срока не должна
// выглядеть протухшей полвека назад.
func msToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
