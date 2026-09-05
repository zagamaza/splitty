// Package push шлёт native-пуши устройствам пользователей через Firebase Cloud
// Messaging (HTTP v1). Доставка — по OUTBOX-подходу (как офлайн-очередь операций
// в приложениях): Notifier не шлёт напрямую, а кладёт пуш в персистентную очередь
// (push_outbox), а фоновый Worker доставляет с ретраями и бэк-оффом. Транзиентный
// сбой FCM/сети/рестарт сервера не теряет уведомление.
package push

import (
	"context"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/safe"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/option"
)

// maxAttempts — после стольких неудачных попыток пуш выбрасываем из очереди
// (не копим мёртвые записи, как permanently-failed в аутбоксе приложений).
const maxAttempts = 6

// Notification — полезная нагрузка одного пуша.
type Notification struct {
	Title string
	Body  string
	// Data — доп. поля для deeplink в приложении (roomId/operationId) и выбора
	// Android-канала (data["channel"]); приходят в onMessageReceived.
	Data map[string]string
}

// PendingPush — запись очереди доставки (push_outbox). Переживает рестарт.
type PendingPush struct {
	ID           string
	UserID       int
	Notification Notification
	Attempts     int
	// Locale — язык устройств-адресатов. Пусто: запись старой очереди или
	// устройства без локали — доставляем на все токены, как раньше.
	Locale string
}

// TokenOutcome — судьба одного токена в отправке. Token хранит только ХВОСТ
// токена: полный лежит в документе пользователя, а для сопоставления «какое из
// устройств не приняло» хватает шести символов, и копия секрета в другой
// коллекции не заводится.
type TokenOutcome struct {
	Token string
	// Error пусто — доставлено; иначе текст ошибки FCM.
	Error string
}

// Исходы доставки, попадающие в след записи.
const (
	OutcomeSent     = "sent"      // FCM принял хотя бы один токен
	OutcomeRejected = "rejected"  // приняты НОЛЬ токенов: все отбракованы навсегда
	OutcomeNoTokens = "no_tokens" // слать некуда: у человека не осталось устройств
	OutcomeGaveUp   = "gave_up"   // исчерпаны попытки на транзиентных сбоях
)

// DeliveryResult — что случилось с пушем. Оседает в очереди рядом с записью.
type DeliveryResult struct {
	Outcome string
	Tokens  []TokenOutcome
}

// Store — персистентная очередь пушей (реализация — Mongo в repository).
type Store interface {
	// Enqueue кладёт пуш в очередь к немедленной доставке. locale — язык
	// устройств-адресатов; пусто = устройства без локали.
	Enqueue(ctx context.Context, userID int, locale string, n Notification) error
	// Due возвращает пачку записей, у которых подошло время доставки.
	// Обязана отдавать только НЕотправленные — иначе доставленный пуш уходил
	// бы по кругу.
	Due(ctx context.Context, now time.Time, limit int) ([]PendingPush, error)
	// MarkSent закрывает запись, оставляя след: что ответил FCM по каждому
	// токену. Запись не удаляется — её уносит TTL. Раньше здесь был Delete, и
	// успешная отправка была неотличима от «ушло в никуда»: очередь пуста в
	// обоих случаях, а логов не было вовсе.
	MarkSent(ctx context.Context, id string, result DeliveryResult) error
	// Reschedule откладывает повторную попытку (транзиентный сбой + бэк-офф).
	Reschedule(ctx context.Context, id string, nextAt time.Time, attempts int) error
}

// TokenRemover убирает отбракованный FCM-токен (UNREGISTERED — приложение удалено).
type TokenRemover interface {
	RemovePushToken(ctx context.Context, userId int, token string) error
}

// UserFinder читает канонический документ пользователя (с PushTokens).
type UserFinder interface {
	FindById(ctx context.Context, id int) (*api.User, error)
}

// Sender — то, что зовёт Notifier: кладёт пуш в очередь (не шлёт синхронно).
//
// locale — язык УСТРОЙСТВ, которым адресована эта запись. Пусто означает
// устройства без локали (старый клиент). Notifier ставит по одной записи на
// каждый различный язык среди токенов пользователя, поэтому русский телефон и
// английский планшет получают разный текст.
type Sender interface {
	SendToUser(ctx context.Context, user api.User, locale string, n Notification)
}

// NoopSender — заглушка, когда FCM/очередь не сконфигурированы: молча ничего не
// делает, остальной сервер работает как раньше.
type NoopSender struct{}

func (NoopSender) SendToUser(context.Context, api.User, string, Notification) {}

// queueSender кладёт пуш в Store (outbox). Доставку делает Worker.
type queueSender struct {
	store Store
}

// NewSender отдаёт Sender поверх очереди. Пустой store — NoopSender.
func NewSender(store Store) Sender {
	if store == nil {
		return NoopSender{}
	}
	return &queueSender{store: store}
}

func (s *queueSender) SendToUser(ctx context.Context, user api.User, locale string, n Notification) {
	if err := s.store.Enqueue(ctx, user.ID, locale, n); err != nil {
		log.Error().Err(err).Int("user", user.ID).Msg("push: enqueue failed")
	}
}

// sendTimeout — окно на одну отправку в FCM.
const sendTimeout = 15 * time.Second

// Worker доставляет пуши из очереди через FCM с ретраями.
type Worker struct {
	client  *messaging.Client
	store   Store
	users   UserFinder
	remover TokenRemover
	tick    time.Duration
	batch   int
}

// NewWorker поднимает Firebase-приложение из service-account JSON и собирает
// воркер очереди. Пустой credentialsFile — воркера нет (nil, nil): пуши выключены.
func NewWorker(ctx context.Context, credentialsFile string, store Store, users UserFinder, remover TokenRemover) (*Worker, error) {
	if credentialsFile == "" || store == nil {
		log.Warn().Msg("push: FIREBASE_CREDENTIALS_FILE пуст — пуши выключены")
		return nil, nil
	}
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, err
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, err
	}
	log.Info().Msg("push: FCM-воркер инициализирован")
	return &Worker{client: client, store: store, users: users, remover: remover, tick: 5 * time.Second, batch: 50}, nil
}

// Run гоняет очередь до отмены ctx (best-effort, ошибки логируются).
func (w *Worker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer safe.Recover("воркер доставки пушей")
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.drain(ctx)
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	due, err := w.store.Due(ctx, time.Now(), w.batch)
	if err != nil {
		log.Error().Err(err).Msg("push: read queue failed")
		return
	}
	for _, p := range due {
		w.deliver(ctx, p)
	}
}

// deliver шлёт один пуш на все токены пользователя. Успех/фатальный результат
// (нет токенов, исчерпаны попытки) — удаляем из очереди; транзиентный сбой —
// откладываем с экспоненциальным бэк-оффом.
// tokensFor — токены ровно этой локали; пустая локаль тоже локаль.
//
// Раньше запись без языка уходила на ВСЕ токены, и человек со старым телефоном
// (токен без языка) и обновлённым планшетом получал на планшет два пуша: один
// от записи «без языка», второй от записи «en». Ровно то, что происходит на
// выкате, пока одно устройство обновилось, а другое нет.
//
// Записи, легшие в очередь до появления поля, тоже приходят сюда с пустым
// языком — и попадают на все токены такого пользователя, потому что до
// перерегистрации у его устройств языка нет.
func tokensFor(tokens []api.PushToken, locale string) []api.PushToken {
	out := make([]api.PushToken, 0, len(tokens))
	for _, t := range tokens {
		if t.Locale == locale {
			out = append(out, t)
		}
	}
	return out
}

func (w *Worker) deliver(ctx context.Context, p PendingPush) {
	user, err := w.users.FindById(ctx, p.UserID)
	if err != nil {
		w.retry(ctx, p) // не смогли прочитать пользователя — ещё попытка
		return
	}
	if user == nil || len(user.PushTokens) == 0 {
		// Некому слать. След оставляем: пустой список токенов — самый частый
		// ответ на вопрос «почему не пришло», и раньше он был невидим.
		w.mark(ctx, p, DeliveryResult{Outcome: OutcomeNoTokens})
		return
	}

	tokens := tokensFor(user.PushTokens, p.Locale)
	if len(tokens) == 0 {
		// Устройство успело сменить язык или отвалиться, пока пуш ждал.
		w.mark(ctx, p, DeliveryResult{Outcome: OutcomeNoTokens})
		return
	}

	messages := make([]*messaging.Message, 0, len(tokens))
	for _, t := range tokens {
		messages = append(messages, &messaging.Message{
			Token:        t.Token,
			Notification: &messaging.Notification{Title: p.Notification.Title, Body: p.Notification.Body},
			Data:         p.Notification.Data,
			Android: &messaging.AndroidConfig{
				Priority:     "high",
				Notification: &messaging.AndroidNotification{ChannelID: p.Notification.Data["channel"]},
			},
		})
	}

	// Своё окно на отправку: ctx живёт столько же, сколько процесс, и зависший
	// вызов FCM держал бы очередь пушей до перезапуска
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	resp, err := w.client.SendEach(sendCtx, messages)
	if err != nil {
		log.Warn().Err(err).Int("user", p.UserID).Msg("push: SendEach failed, retrying")
		w.retry(ctx, p)
		return
	}

	transient := false
	outcomes := make([]TokenOutcome, 0, len(resp.Responses))
	for i, r := range resp.Responses {
		token := tokens[i].Token
		if r.Success {
			outcomes = append(outcomes, TokenOutcome{Token: tokenTail(token)})
			continue
		}
		outcomes = append(outcomes, TokenOutcome{Token: tokenTail(token), Error: r.Error.Error()})
		switch {
		case messaging.IsUnregistered(r.Error) || messaging.IsInvalidArgument(r.Error):
			// Мёртвый/битый токен — чистим, не ретраим (это не сбой доставки).
			// Логируем: молчаливая чистка делала картину «токены есть, ошибок
			// нет, пуш не пришёл» необъяснимой.
			log.Warn().Int("user", p.UserID).Str("token", tokenTail(token)).
				Err(r.Error).Msg("push: токен отбракован FCM, удаляем")
			if w.remover != nil {
				_ = w.remover.RemovePushToken(ctx, p.UserID, token)
			}
		default:
			transient = true // FCM недоступен/квота — попробуем ещё раз
		}
	}
	if transient {
		w.retry(ctx, p)
		return
	}
	outcome := outcomeFor(resp.SuccessCount)
	log.Info().Int("user", p.UserID).Int("tokens", len(outcomes)).
		Int("failed", resp.FailureCount).Str("outcome", outcome).Msg("push: отправлен")
	w.mark(ctx, p, DeliveryResult{Outcome: outcome, Tokens: outcomes})
}

// outcomeFor — исход по числу принятых FCM токенов, а не по «не было
// транзиентных сбоёв»: если ВСЕ токены отбракованы навсегда, принято ноль, и
// писать в след «sent» значит врать ровно в том случае, ради которого след и
// заводился.
func outcomeFor(successCount int) string {
	if successCount == 0 {
		return OutcomeRejected
	}
	return OutcomeSent
}

// mark закрывает запись очереди следом доставки. Ошибку только логируем: пуш
// уже отправлен, и падать из-за неудачной пометки нечестно — зато потерянный
// след виден.
func (w *Worker) mark(ctx context.Context, p PendingPush, result DeliveryResult) {
	if err := w.store.MarkSent(ctx, p.ID, result); err != nil {
		log.Error().Err(err).Str("id", p.ID).Msg("push: mark sent failed")
	}
}

// tokenTail — последние 6 символов токена: столько нужно, чтобы отличить
// устройства друг от друга, и мало, чтобы это был секрет.
func tokenTail(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[len(token)-6:]
}

func (w *Worker) retry(ctx context.Context, p PendingPush) {
	attempts := p.Attempts + 1
	if attempts >= maxAttempts {
		log.Warn().Int("user", p.UserID).Int("attempts", attempts).Msg("push: giving up after max attempts")
		w.mark(ctx, p, DeliveryResult{Outcome: OutcomeGaveUp})
		return
	}
	// Экспоненциальный бэк-офф: 10с, 20с, 40с … кап 10 мин.
	backoff := time.Duration(10<<uint(attempts)) * time.Second
	if backoff > 10*time.Minute {
		backoff = 10 * time.Minute
	}
	if err := w.store.Reschedule(ctx, p.ID, time.Now().Add(backoff), attempts); err != nil {
		log.Error().Err(err).Str("id", p.ID).Msg("push: reschedule failed")
	}
}
