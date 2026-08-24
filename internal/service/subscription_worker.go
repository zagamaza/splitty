package service

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/store"
	"github.com/rs/zerolog/log"
)

// Фоновая доработка подписок: то, что нельзя доверить ни клиенту, ни вебхуку.
//
// Две задачи, обе про деньги.
//
// ПОДТВЕРЖДЕНИЕ ПОКУПОК. Google откатывает неподтверждённую покупку через трое
// суток и возвращает деньги. Подтверждать её пытается и хендлер покупки, и
// вебхук, но оба могут не сработать: сервер упал сразу после валидации,
// уведомление потерялось, человек снёс приложение. Ретрай здесь — последняя
// линия, после которой деньги действительно уходят обратно.
//
// ДОСИНХРОНИЗАЦИЯ ИСТЕКАЮЩИХ. Уведомление о продлении может не дойти. Без
// сверки такая подписка навсегда осталась бы «истёкшей» у платящего человека —
// или, наоборот, «активной» у переставшего платить.

// SubscriptionMaintenanceStore — то, что воркеру нужно от хранилища подписок.
type SubscriptionMaintenanceStore interface {
	PendingAcks(ctx context.Context, limit int64) ([]api.Subscription, error)
	ExpiringBefore(ctx context.Context, deadline time.Time, limit int64) ([]api.Subscription, error)
	Upsert(ctx context.Context, s api.Subscription) error
	SetAckState(ctx context.Context, store, ref, state string) error
	MarkRevoked(ctx context.Context, store, ref string, at time.Time) error
}

// AppleStatusReader/GoogleStatusReader — перезапрос состояния у магазина.
type AppleStatusReader interface {
	Status(ctx context.Context, ref, environment string) (store.Receipt, error)
}

type GoogleStatusReader interface {
	Status(ctx context.Context, token string) (store.Receipt, error)
	Acknowledge(ctx context.Context, token string) error
}

// SubscriptionWorkerConfig — параметры фоновой доработки.
type SubscriptionWorkerConfig struct {
	// Interval — период тика. Подтверждение терпит часы (у Google трое суток),
	// поэтому частить незачем.
	Interval time.Duration
	// LookAhead — насколько вперёд смотреть на истекающие подписки.
	LookAhead time.Duration
	// BatchLimit — сколько записей обрабатывать за тик.
	BatchLimit int64
}

// SubscriptionWorker досинхронизирует подписки в фоне.
type SubscriptionWorker struct {
	subs   SubscriptionMaintenanceStore
	apple  AppleStatusReader
	google GoogleStatusReader
	ents   *Entitlements
	cfg    SubscriptionWorkerConfig
	now    func() time.Time
}

func NewSubscriptionWorker(
	subs SubscriptionMaintenanceStore,
	apple AppleStatusReader,
	google GoogleStatusReader,
	ents *Entitlements,
	cfg SubscriptionWorkerConfig,
) *SubscriptionWorker {
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Minute
	}
	if cfg.LookAhead <= 0 {
		cfg.LookAhead = time.Hour
	}
	if cfg.BatchLimit <= 0 {
		cfg.BatchLimit = 100
	}
	return &SubscriptionWorker{subs: subs, apple: apple, google: google, ents: ents, cfg: cfg, now: time.Now}
}

// Run крутит тики до отмены контекста. Блокирующий — звать из горутины.
func (w *SubscriptionWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	log.Info().Dur("interval", w.cfg.Interval).Msg("фоновая доработка подписок запущена")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Tick — один проход. Публичный, чтобы тесты не ждали таймера.
func (w *SubscriptionWorker) Tick(ctx context.Context) {
	w.retryAcks(ctx)
	w.resyncExpiring(ctx)
}

// retryAcks добивает подтверждение покупок Google.
func (w *SubscriptionWorker) retryAcks(ctx context.Context) {
	if w.google == nil {
		return
	}
	pending, err := w.subs.PendingAcks(ctx, w.cfg.BatchLimit)
	if err != nil {
		log.Error().Err(err).Msg("не удалось прочитать неподтверждённые покупки")
		return
	}
	for i := range pending {
		sub := pending[i]
		if sub.Store != api.StoreGoogle {
			continue
		}
		if err := w.google.Acknowledge(ctx, sub.StoreRef); err != nil {
			// Оставляем pending: следующий тик попробует снова. Пометить
			// «сделано» при ошибке значило бы дать Google откатить платёж молча.
			log.Error().Err(err).Str("ref", sub.StoreRef).Msg("не удалось подтвердить покупку, повторим")
			continue
		}
		if err := w.subs.SetAckState(ctx, sub.Store, sub.StoreRef, api.AckDone); err != nil {
			log.Error().Err(err).Str("ref", sub.StoreRef).Msg("не удалось отметить подтверждение")
		}
	}
}

// resyncExpiring сверяет истекающие подписки с магазином.
func (w *SubscriptionWorker) resyncExpiring(ctx context.Context) {
	deadline := w.now().UTC().Add(w.cfg.LookAhead)
	expiring, err := w.subs.ExpiringBefore(ctx, deadline, w.cfg.BatchLimit)
	if err != nil {
		log.Error().Err(err).Msg("не удалось прочитать истекающие подписки")
		return
	}
	for i := range expiring {
		w.resyncOne(ctx, expiring[i])
	}
}

func (w *SubscriptionWorker) resyncOne(ctx context.Context, sub api.Subscription) {
	receipt, err := w.status(ctx, sub)
	if err != nil {
		// Магазин не ответил — НИЧЕГО не меняем. Снять Plus у платящего из-за
		// таймаута хуже, чем подождать до следующего тика.
		log.Warn().Err(err).Str("store", sub.Store).Str("ref", sub.StoreRef).
			Msg("не удалось сверить подписку с магазином")
		return
	}

	updated := api.Subscription{
		UserId:       sub.UserId,
		Store:        sub.Store,
		ProductId:    receipt.ProductId,
		StoreRef:     sub.StoreRef,
		LinkedRef:    receipt.LinkedRef,
		BindingToken: sub.BindingToken,
		ExpiresAt:    receipt.ExpiresAt,
		AutoRenew:    receipt.AutoRenew,
		Environment:  receipt.Environment,
		AckState:     sub.AckState,
		// LastNotifiedAt НЕ трогаем: это ответ магазина, а не уведомление, и
		// отсечка переупорядочивания к нему не относится.
	}
	if err := w.subs.Upsert(ctx, updated); err != nil {
		log.Error().Err(err).Str("ref", sub.StoreRef).Msg("не удалось записать сверенную подписку")
		return
	}
	if receipt.Revoked {
		if err := w.subs.MarkRevoked(ctx, sub.Store, sub.StoreRef, w.now().UTC()); err != nil {
			log.Error().Err(err).Str("ref", sub.StoreRef).Msg("не удалось отметить возврат")
		}
	}
	if w.ents != nil {
		w.ents.Invalidate(sub.UserId)
	}
}

func (w *SubscriptionWorker) status(ctx context.Context, sub api.Subscription) (store.Receipt, error) {
	if sub.Store == api.StoreApple {
		if w.apple == nil {
			return store.Receipt{}, store.ErrNotConfigured
		}
		return w.apple.Status(ctx, sub.StoreRef, sub.Environment)
	}
	if w.google == nil {
		return store.Receipt{}, store.ErrNotConfigured
	}
	return w.google.Status(ctx, sub.StoreRef)
}
