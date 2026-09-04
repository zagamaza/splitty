package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/store"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// Коды ответов подписки.
const (
	errCodeSubscriptionsOff = "subscriptions_disabled"
	errCodeReceiptInvalid   = "receipt_invalid"
	errCodeReceiptForeign   = "receipt_belongs_to_other_account"
	errCodeStoreUnavailable = "store_unavailable"
)

// ReceiptVerifier — проверка чека одного магазина. Экспортирован, потому что
// main объявляет переменные этого типа: nil означает «ключи магазина не заданы».
type ReceiptVerifier interface {
	Verify(ctx context.Context, receipt string) (store.Receipt, error)
}

// PurchaseAcknowledger — подтверждение покупки (только Google).
type PurchaseAcknowledger interface {
	Acknowledge(ctx context.Context, token string) error
}

// subscriptionStore — запись подписок.
type subscriptionStore interface {
	Upsert(ctx context.Context, s api.Subscription) error
	ActiveByUser(ctx context.Context, userId int) ([]api.Subscription, error)
	ByStoreRef(ctx context.Context, store, ref string) (*api.Subscription, error)
	Supersede(ctx context.Context, store, ref string, at time.Time) error
	SetAckState(ctx context.Context, store, ref, state string) error
	MarkRevoked(ctx context.Context, store, ref string, at time.Time) error
	// DeleteByUserId — чистка при удалении аккаунта. В документах лежат
	// идентификаторы покупок, привязанные к человеку: без чистки они пережили
	// бы аккаунт, а повторная регистрация упёрлась бы в собственную прошлую
	// привязку и никогда не смогла бы купить снова
	DeleteByUserId(ctx context.Context, userId int) error
}

// plusGrantStore — гранты глазами HTTP-слоя.
//
// ОДИН узкий интерфейс на обе нужды, по образцу subscriptionStore: срок для
// экрана подписки и чистка при удалении аккаунта. Второй сеттер ради
// DeleteByUserId завёл бы две настройки одной коллекции, которые можно забыть
// подключить по отдельности.
type plusGrantStore interface {
	LiveByUser(ctx context.Context, userId int, now time.Time) (*api.PlusGrant, error)
	Grant(ctx context.Context, userId int, expiresAt time.Time, reason string, now time.Time) error
	Revoke(ctx context.Context, userId int, reason string, at time.Time) error
	ListLive(ctx context.Context, now time.Time) ([]api.PlusGrant, error)
	// DeleteByUserId — чистка при удалении аккаунта: строка гранта несёт номер
	// человека и причину, то есть данные о нём
	DeleteByUserId(ctx context.Context, userId int) error
}

// SetPlusGrants подключает гранты. Вызывать до Run: поле читается из
// обработчиков без блокировки. Nil-безопасно — без него экран подписки ведёт
// себя ровно как раньше.
func (s *Server) SetPlusGrants(g plusGrantStore) {
	s.plusGrants = g
}

// SetSubscriptions подключает проверку чеков и хранилище подписок.
//
// nil-верификаторы означают, что ключи магазина не заданы: эндпоинты отдают 503,
// никто не становится платным, остальной сервер работает как раньше. Та же
// политика, что у Gemini и FCM.
func (s *Server) SetSubscriptions(subs subscriptionStore, apple ReceiptVerifier, google ReceiptVerifier, ack PurchaseAcknowledger) {
	s.subscriptions = subs
	s.appleReceipts = apple
	s.googleReceipts = google
	s.googleAck = ack
}

// SetSandboxReceipts разрешает чеки песочницы — но только перечисленным людям.
//
// Ревьюер магазина покупает в песочнице: его чек приходит на боевой сервер, и
// прод, принимающий только Production, отвечает отказом — кнопка «Оформить»
// показывает ошибку, а это отказ по 2.1 «In-app purchases must be complete and
// functional». Открыть песочницу всем нельзя: такие подписки бесплатны и
// продлеваются каждые несколько минут, то есть раздавали бы вечный Plus даром.
//
// Список тот же, что у comp-тарифа: владелец проекта и демо-аккаунт ревьюера.
func (s *Server) SetSandboxReceipts(userIds []int, apple ReceiptVerifier, google ReceiptVerifier) {
	s.sandboxUsers = make(map[int]struct{}, len(userIds))
	for _, id := range userIds {
		s.sandboxUsers[id] = struct{}{}
	}
	s.appleSandboxReceipts = apple
	s.googleSandboxReceipts = google
}

// sandboxVerifierFor — «песочный» проверяльщик для этого человека, если он есть
// в списке и магазин настроен. Иначе nil: обычный отказ по окружению.
func (s *Server) sandboxVerifierFor(storeName string, userId int) ReceiptVerifier {
	if _, ok := s.sandboxUsers[userId]; !ok {
		return nil
	}
	if storeName == api.StoreApple {
		return s.appleSandboxReceipts
	}
	return s.googleSandboxReceipts
}

type appleReceiptRequest struct {
	JWS string `json:"jws"`
}

type googleReceiptRequest struct {
	PurchaseToken string `json:"purchaseToken"`
	ProductId     string `json:"productId"`
}

type subscriptionDto struct {
	Tier      api.Tier   `json:"tier"`
	Store     string     `json:"store,omitempty"`
	ProductId string     `json:"productId,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	AutoRenew bool       `json:"autoRenew"`
	// ManageUrl — куда вести человека за отменой. Отмена делается в магазине, а
	// не у нас: своей кнопки «отменить подписку» у приложения нет и быть не может.
	ManageUrl string `json:"manageUrl,omitempty"`
}

const (
	appleManageUrl  = "https://apps.apple.com/account/subscriptions"
	googleManageUrl = "https://play.google.com/store/account/subscriptions"
)

// handlePostAppleSubscription POST /api/v1/me/subscription/apple
func (s *Server) handlePostAppleSubscription(w http.ResponseWriter, r *http.Request) {
	var req appleReceiptRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	s.applyReceipt(w, r, api.StoreApple, s.appleReceipts, req.JWS)
}

// handlePostGoogleSubscription POST /api/v1/me/subscription/google
func (s *Server) handlePostGoogleSubscription(w http.ResponseWriter, r *http.Request) {
	var req googleReceiptRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	s.applyReceipt(w, r, api.StoreGoogle, s.googleReceipts, req.PurchaseToken)
}

// applyReceipt — общий путь обоих магазинов: проверить чек, сверить привязку,
// записать подписку, подтвердить покупку.
func (s *Server) applyReceipt(w http.ResponseWriter, r *http.Request, storeName string, verifier ReceiptVerifier, raw string) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	if verifier == nil || s.subscriptions == nil {
		writeError(w, http.StatusServiceUnavailable, errCodeSubscriptionsOff, "покупки сейчас недоступны")
		return
	}
	if raw == "" {
		writeError(w, http.StatusBadRequest, "validation", "пустой чек")
		return
	}

	receipt, err := verifier.Verify(ctx, raw)
	if errors.Is(err, store.ErrWrongEnvironment) {
		if sandbox := s.sandboxVerifierFor(storeName, userId); sandbox != nil {
			log.Info().Int("userId", userId).Str("store", storeName).
				Msg("принимаю чек песочницы: пользователь в списке comp")
			receipt, err = sandbox.Verify(ctx, raw)
		}
	}
	if err != nil {
		s.writeReceiptError(w, storeName, err)
		return
	}

	if hErr := s.checkReceiptBinding(ctx, storeName, userId, receipt); hErr != nil {
		hErr.write(w)
		return
	}

	// Смена плана в Google выдаёт НОВЫЙ purchaseToken и кладёт старый в
	// linkedPurchaseToken. Гасим предшественника до записи новой подписки:
	// иначе на одного человека остаются две «активные», и старая продолжает
	// давать Plus даже после возврата денег по новой.
	if receipt.LinkedRef != "" {
		if err := s.subscriptions.Supersede(ctx, storeName, receipt.LinkedRef, time.Now().UTC()); err != nil {
			log.Error().Err(err).Str("linkedRef", receipt.LinkedRef).Msg("cannot supersede previous purchase")
		}
	}

	sub := api.Subscription{
		UserId:       userId,
		Store:        storeName,
		ProductId:    receipt.ProductId,
		StoreRef:     receipt.StoreRef,
		LinkedRef:    receipt.LinkedRef,
		BindingToken: receipt.BindingToken,
		ExpiresAt:    receipt.ExpiresAt,
		AutoRenew:    receipt.AutoRenew,
		Environment:  receipt.Environment,
		AckState:     ackStateFor(storeName, receipt),
	}
	if err := s.subscriptions.Upsert(ctx, sub); err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot save subscription")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось сохранить подписку")
		return
	}
	if receipt.Revoked {
		// Чек может приехать уже отозванным (человек успел вернуть деньги).
		// Пишем подписку и сразу гасим её, чтобы состояние было полным.
		if err := s.subscriptions.MarkRevoked(ctx, storeName, receipt.StoreRef, time.Now().UTC()); err != nil {
			log.Error().Err(err).Str("ref", receipt.StoreRef).Msg("cannot mark subscription revoked")
		}
	}

	// Подтверждение покупки. Состояние уже записано как pending, поэтому сбой
	// здесь не теряет покупку: её добьёт фоновый воркер. Иначе Google откатил
	// бы платёж через трое суток.
	if receipt.NeedsAck && s.googleAck != nil && storeName == api.StoreGoogle {
		if err := s.googleAck.Acknowledge(ctx, receipt.StoreRef); err != nil {
			log.Error().Err(err).Str("ref", receipt.StoreRef).Msg("cannot acknowledge purchase, worker will retry")
		} else if err := s.subscriptions.SetAckState(ctx, storeName, receipt.StoreRef, api.AckDone); err != nil {
			log.Error().Err(err).Msg("cannot mark purchase acknowledged")
		}
	}

	// Человек только что заплатил — Plus обязан появиться немедленно, а не
	// когда протухнет кеш тарифа.
	if s.entitlements != nil {
		s.entitlements.Invalidate(userId)
	}

	s.writeSubscriptionState(w, r, userId)
}

// checkReceiptBinding сверяет, кому принадлежит чек.
//
// Без этой проверки действует правило «чей чек — того, кто первый прислал»:
// утёкший или расшаренный чек забирает тот, кто успел раньше, а настоящий
// покупатель остаётся без Plus и без объяснений.
//
// Чек БЕЗ токена привязки принимается: так выглядят покупки со сборок, которые
// его ещё не передают. Отвергать их значило бы оставить без Plus тех, кто
// заплатил на старой версии.
func (s *Server) checkReceiptBinding(ctx context.Context, storeName string, userId int, receipt store.Receipt) *httpError {
	if receipt.BindingToken == "" {
		log.Info().
			Str("store", storeName).Str("ref", receipt.StoreRef).Int("userId", userId).
			Msg("чек без токена привязки: покупка со сборки, которая его ещё не шлёт")
		return s.checkReceiptNotTakenByOthers(ctx, storeName, userId, receipt)
	}

	expected, err := s.userRepo.EnsureBindingToken(ctx, userId)
	if err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot read binding token")
		return &httpError{http.StatusInternalServerError, errCodeInternal, "не удалось проверить покупку"}
	}
	if receipt.BindingToken != expected {
		log.Warn().
			Str("store", storeName).Str("ref", receipt.StoreRef).Int("userId", userId).
			Msg("чек привязан к другому аккаунту")
		return &httpError{http.StatusConflict, errCodeReceiptForeign,
			"эта подписка оформлена на другой аккаунт Splitor. Войдите в него или напишите нам, чтобы перенести"}
	}
	return nil
}

// checkReceiptNotTakenByOthers — чек без токена привязки не должен уводить
// подписку у того, за кем она уже записана.
func (s *Server) checkReceiptNotTakenByOthers(ctx context.Context, storeName string, userId int, receipt store.Receipt) *httpError {
	existing, err := s.subscriptions.ByStoreRef(ctx, storeName, receipt.StoreRef)
	if err != nil || existing == nil {
		return nil // записи нет — чек свободен
	}
	if existing.UserId != userId {
		return &httpError{http.StatusConflict, errCodeReceiptForeign,
			"эта подписка уже привязана к другому аккаунту Splitor"}
	}
	return nil
}

func ackStateFor(storeName string, receipt store.Receipt) string {
	if storeName != api.StoreGoogle {
		return api.AckNotApplicable
	}
	if receipt.NeedsAck {
		return api.AckPending
	}
	return api.AckDone
}

// writeReceiptError переводит причину отказа в http-ответ.
func (s *Server) writeReceiptError(w http.ResponseWriter, storeName string, err error) {
	switch {
	case errors.Is(err, store.ErrReceiptInvalid):
		writeError(w, http.StatusBadRequest, errCodeReceiptInvalid, "чек не прошёл проверку")
	case errors.Is(err, store.ErrForeignProduct):
		writeError(w, http.StatusBadRequest, errCodeReceiptInvalid, "чек выписан на другой продукт")
	case errors.Is(err, store.ErrWrongEnvironment):
		writeError(w, http.StatusBadRequest, errCodeReceiptInvalid, "чек выписан в тестовом окружении")
	default:
		// Недоступность магазина — НЕ «чек плохой»: клиенту надо повторить
		// позже, а не считать покупку негодной.
		log.Error().Err(err).Str("store", storeName).Msg("store verification failed")
		writeError(w, http.StatusBadGateway, errCodeStoreUnavailable, "магазин сейчас не отвечает, попробуйте позже")
	}
}

// handleGetSubscription GET /api/v1/me/subscription
func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	s.writeSubscriptionState(w, r, userIdFromCtx(r.Context()))
}

func (s *Server) writeSubscriptionState(w http.ResponseWriter, r *http.Request, userId int) {
	ctx := r.Context()

	tier := api.TierFree
	if s.entitlements != nil {
		tier = s.entitlements.TierOrFree(ctx, userId)
	}
	dto := subscriptionDto{Tier: tier}

	// Реквизиты покупки заполняются ТОЛЬКО от живой. ActiveByUser намеренно
	// возвращает и истёкшие (решение «активна ли» принимает вызывающий), и
	// раньше это было безвредно: у просрочившего tier == free, и экран уходил в
	// бесплатную ветку. С грантом tier == plus — и человек с прошлогодней
	// отменённой подпиской увидел бы «Активна до» с датой из прошлого и живую
	// ссылку в стор, где ничего нет. Это ровно та аудитория, которой гранты и
	// раздают.
	now := time.Now()
	if s.subscriptions != nil {
		if subs, err := s.subscriptions.ActiveByUser(ctx, userId); err == nil {
			for i := range subs {
				if !subs[i].Active(now, s.deliverySlack) {
					continue
				}
				if dto.ExpiresAt == nil || subs[i].ExpiresAt.After(*dto.ExpiresAt) {
					expires := subs[i].ExpiresAt
					dto.ExpiresAt = &expires
					dto.Store = subs[i].Store
					dto.ProductId = subs[i].ProductId
					dto.AutoRenew = subs[i].AutoRenew
				}
			}
		}
	}
	switch dto.Store {
	case api.StoreApple:
		dto.ManageUrl = appleManageUrl
	case api.StoreGoogle:
		dto.ManageUrl = googleManageUrl
	}

	// Грант побеждает по дате, но реквизитов не приносит: у него нет ни стора,
	// ни продукта, ни страницы управления. Победил — значит все поля покупки
	// снимаются, иначе экран показал бы дату гранта со ссылкой от чужой,
	// давно истёкшей покупки.
	//
	// tier выше взят из кеша, а срок — свежим запросом: пара не атомарна. В
	// одном процессе окна нет (выдача и отзыв зовут Invalidate), но при
	// репликах оно появится.
	if s.plusGrants != nil {
		if g, err := s.plusGrants.LiveByUser(ctx, userId, now); err == nil && g.Live(now) {
			if dto.ExpiresAt == nil || g.ExpiresAt.After(*dto.ExpiresAt) {
				expires := g.ExpiresAt
				dto.ExpiresAt = &expires
				dto.Store = ""
				dto.ProductId = ""
				dto.AutoRenew = false
				dto.ManageUrl = ""
			}
		}
	}

	writeJSON(w, http.StatusOK, dto)
}
