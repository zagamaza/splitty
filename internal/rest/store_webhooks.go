package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/store"
	"github.com/rs/zerolog/log"
)

// Уведомления магазинов о судьбе подписки: продление, отмена, истечение, возврат.
//
// Два правила, из которых следует всё остальное.
//
// ПЕРВОЕ: обработчик НЕ применяет то, что написано в уведомлении. Он берёт из
// него только идентификатор покупки, идёт в магазин за текущим состоянием и
// записывает ответ магазина. Причина — доставка приходит не по порядку:
// задержавшийся EXPIRED вполне может прилететь после DID_RENEW, и применённый
// как есть он погасил бы действующую подписку. Человек платит, а Plus снимается.
//
// ВТОРОЕ: отвечаем 200 как можно раньше и по любому исходу, который не является
// нашей ошибкой. Оба магазина ретраят по таймауту и по не-2xx, а повторная
// доставка того же события — норма, а не признак сбоя.

// errWebhookMalformed — уведомление не разобрать. Повторная доставка не
// поможет, поэтому наверх оно уходит как 400, а не как повод для ретрая.
var errWebhookMalformed = errors.New("уведомление магазина не разобрать")

// storeStatusReader перезапрашивает состояние подписки у магазина.
type storeStatusReader interface {
	Status(ctx context.Context, ref, environment string) (store.Receipt, error)
}

// googleStatusReader — у Google окружение не нужно: токен сам себя опознаёт.
type googleStatusReader interface {
	Status(ctx context.Context, token string) (store.Receipt, error)
}

// SetStoreWebhooks подключает перезапрос состояния для уведомлений магазинов.
func (s *Server) SetStoreWebhooks(apple storeStatusReader, google googleStatusReader) {
	s.appleStatus = apple
	s.googleStatus = google
}

// maxWebhookBody — уведомления обоих магазинов заведомо меньше; всё, что
// крупнее, читать незачем.
const maxWebhookBody = 512 << 10

// appleNotification — конверт App Store Server Notifications V2.
type appleNotification struct {
	SignedPayload string `json:"signedPayload"`
}

// applePayload — то, что нам нужно из разобранного уведомления. Всё остальное
// (включая срок и статус) берётся у магазина заново, см. правило ПЕРВОЕ.
type applePayload struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	SignedDate       int64  `json:"signedDate"`
	Data             struct {
		Environment           string `json:"environment"`
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}

// handleAppleStoreWebhook POST /api/v1/webhooks/apple
func (s *Server) handleAppleStoreWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var envelope appleNotification
	if err := decodeWebhookBody(r, &envelope); err != nil || envelope.SignedPayload == "" {
		log.Warn().Err(err).Msg("apple webhook: битое тело")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	payload, err := decodeJWSPayload[applePayload](envelope.SignedPayload)
	if err != nil {
		log.Warn().Err(err).Msg("apple webhook: не разобрать payload")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ref, err := appleTransactionRef(payload.Data.SignedTransactionInfo)
	if err != nil || ref == "" {
		log.Warn().Err(err).Str("type", payload.NotificationType).Msg("apple webhook: нет идентификатора транзакции")
		// 200: перепосылка не поможет — в самом уведомлении нечего обрабатывать.
		w.WriteHeader(http.StatusOK)
		return
	}

	s.syncFromStore(ctx, api.StoreApple, ref, payload.Data.Environment,
		msToTime(payload.SignedDate), payload.NotificationType)
	w.WriteHeader(http.StatusOK)
}

// pubSubPush — конверт push-доставки Google Pub/Sub.
type pubSubPush struct {
	Message struct {
		Data        string `json:"data"` // base64 от developerNotification
		MessageID   string `json:"messageId"`
		PublishTime string `json:"publishTime"`
	} `json:"message"`
}

// googleDeveloperNotification — RTDN Google Play.
type googleDeveloperNotification struct {
	Version      string `json:"version"`
	PackageName  string `json:"packageName"`
	EventTimeMs  string `json:"eventTimeMillis"`
	Subscription *struct {
		Version          string `json:"version"`
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		SubscriptionID   string `json:"subscriptionId"`
	} `json:"subscriptionNotification"`
	// VoidedPurchase — ВОЗВРАТЫ приходят только здесь, отдельным типом
	// уведомления, и включаются в Play Console отдельной галкой. Ждать их в
	// subscriptionNotification бесполезно: там их нет.
	VoidedPurchase *struct {
		PurchaseToken string `json:"purchaseToken"`
		OrderId       string `json:"orderId"`
		ProductType   int    `json:"productType"`
		RefundType    int    `json:"refundType"`
	} `json:"voidedPurchaseNotification"`
	TestNotification *struct {
		Version string `json:"version"`
	} `json:"testNotification"`
}

// handleGoogleStoreWebhook POST /api/v1/webhooks/google
func (s *Server) handleGoogleStoreWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var push pubSubPush
	if err := decodeWebhookBody(r, &push); err != nil {
		log.Warn().Err(err).Msg("google webhook: битое тело")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	raw, err := base64.StdEncoding.DecodeString(push.Message.Data)
	if err != nil {
		log.Warn().Err(err).Msg("google webhook: не разобрать base64")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var note googleDeveloperNotification
	if err := json.Unmarshal(raw, &note); err != nil {
		log.Warn().Err(err).Msg("google webhook: не разобрать уведомление")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Проверочное уведомление из Play Console: подтверждаем доставку и выходим.
	if note.TestNotification != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	signedAt := msStringToTime(note.EventTimeMs)

	switch {
	case note.VoidedPurchase != nil:
		// Возврат денег: Plus снимается немедленно, не дожидаясь конца срока.
		if s.subscriptions != nil {
			if err := s.subscriptions.MarkRevoked(ctx, api.StoreGoogle, note.VoidedPurchase.PurchaseToken, time.Now().UTC()); err != nil {
				log.Error().Err(err).Msg("google webhook: не удалось отметить возврат")
			}
			s.invalidateTierByRef(ctx, api.StoreGoogle, note.VoidedPurchase.PurchaseToken)
		}
	case note.Subscription != nil:
		s.syncFromStore(ctx, api.StoreGoogle, note.Subscription.PurchaseToken, "",
			signedAt, "subscription")
	}

	w.WriteHeader(http.StatusOK)
}

// syncFromStore перезапрашивает состояние подписки у магазина и записывает его.
//
// Именно перезапрашивает, а не верит уведомлению: см. правило ПЕРВОЕ наверху.
func (s *Server) syncFromStore(ctx context.Context, storeName, ref, environment string, signedAt time.Time, kind string) {
	if s.subscriptions == nil {
		return
	}

	existing, err := s.subscriptions.ByStoreRef(ctx, storeName, ref)
	if err != nil || existing == nil {
		// Покупки, о которой пришло уведомление, у нас нет. Это законно: чек
		// мог не дойти до сервера (человек закрыл приложение сразу после
		// оплаты). Привязать её не к кому — ждём, когда клиент пришлёт чек.
		log.Info().Str("store", storeName).Str("ref", ref).Str("kind", kind).
			Msg("уведомление о неизвестной покупке")
		return
	}

	receipt, err := s.readStoreStatus(ctx, storeName, ref, pickEnv(environment, existing.Environment))
	if err != nil {
		// Магазин не ответил — состояние НЕ трогаем. Отобрать Plus у платящего
		// из-за таймаута хуже, чем на время оставить лишний доступ; уведомление
		// придёт снова, а не придёт — досинхронизирует фоновый воркер.
		log.Error().Err(err).Str("store", storeName).Str("ref", ref).
			Msg("не удалось перезапросить состояние подписки")
		return
	}

	sub := api.Subscription{
		UserId:         existing.UserId,
		Store:          storeName,
		ProductId:      receipt.ProductId,
		StoreRef:       ref,
		LinkedRef:      receipt.LinkedRef,
		BindingToken:   existing.BindingToken,
		ExpiresAt:      receipt.ExpiresAt,
		AutoRenew:      receipt.AutoRenew,
		Environment:    receipt.Environment,
		AckState:       ackStateFor(storeName, receipt),
		LastNotifiedAt: signedAt,
	}
	if err := s.subscriptions.Upsert(ctx, sub); err != nil {
		// ErrStaleNotification — не сбой: уведомление старше уже применённого,
		// и повторная доставка ничего не исправит.
		log.Info().Err(err).Str("store", storeName).Str("ref", ref).
			Msg("уведомление не применено")
		return
	}

	if receipt.Revoked {
		if err := s.subscriptions.MarkRevoked(ctx, storeName, ref, time.Now().UTC()); err != nil {
			log.Error().Err(err).Msg("не удалось отметить возврат")
		}
	}
	// Неподтверждённая покупка: подтверждаем прямо здесь. Клиент мог не дойти
	// до сервера, а Google откатит платёж через трое суток.
	if receipt.NeedsAck && s.googleAck != nil && storeName == api.StoreGoogle {
		if err := s.googleAck.Acknowledge(ctx, ref); err != nil {
			log.Error().Err(err).Msg("не удалось подтвердить покупку, воркер повторит")
		} else if err := s.subscriptions.SetAckState(ctx, storeName, ref, api.AckDone); err != nil {
			log.Error().Err(err).Msg("не удалось отметить подтверждение")
		}
	}

	if s.entitlements != nil {
		s.entitlements.Invalidate(existing.UserId)
	}
}

func (s *Server) readStoreStatus(ctx context.Context, storeName, ref, environment string) (store.Receipt, error) {
	if storeName == api.StoreApple {
		if s.appleStatus == nil {
			return store.Receipt{}, store.ErrNotConfigured
		}
		return s.appleStatus.Status(ctx, ref, environment)
	}
	if s.googleStatus == nil {
		return store.Receipt{}, store.ErrNotConfigured
	}
	return s.googleStatus.Status(ctx, ref)
}

func (s *Server) invalidateTierByRef(ctx context.Context, storeName, ref string) {
	if s.entitlements == nil || s.subscriptions == nil {
		return
	}
	if sub, err := s.subscriptions.ByStoreRef(ctx, storeName, ref); err == nil && sub != nil {
		s.entitlements.Invalidate(sub.UserId)
	}
}

func decodeWebhookBody(r *http.Request, into interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, into)
}

// decodeJWSPayload достаёт полезную нагрузку из JWS БЕЗ проверки подписи.
//
// ⚠️ Подпись здесь не проверяется намеренно, и это безопасно ровно потому, что
// содержимому уведомления мы всё равно не верим: из него берётся только
// идентификатор покупки, а состояние перезапрашивается у самого магазина по
// нашему ключу. Подделанное уведомление в худшем случае заставит нас лишний раз
// сходить в App Store за подпиской, которая нам уже известна.
func decodeJWSPayload[T any](jws string) (T, error) {
	var out T
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return out, errWebhookMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

// appleTransactionRef достаёт originalTransactionId из подписанной транзакции.
func appleTransactionRef(signedTransactionInfo string) (string, error) {
	if signedTransactionInfo == "" {
		return "", errWebhookMalformed
	}
	tx, err := decodeJWSPayload[struct {
		OriginalTransactionId string `json:"originalTransactionId"`
	}](signedTransactionInfo)
	if err != nil {
		return "", err
	}
	return tx.OriginalTransactionId, nil
}

// pickEnv — окружение из уведомления, иначе из уже записанной подписки.
func pickEnv(fromNotification, fromRecord string) string {
	if fromNotification != "" {
		return fromNotification
	}
	return fromRecord
}

func msToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func msStringToTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	var ms int64
	for _, c := range v {
		if c < '0' || c > '9' {
			return time.Time{}
		}
		ms = ms*10 + int64(c-'0')
	}
	return msToTime(ms)
}
