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
}

// Store — персистентная очередь пушей (реализация — Mongo в repository).
type Store interface {
	// Enqueue кладёт пуш в очередь к немедленной доставке.
	Enqueue(ctx context.Context, userID int, n Notification) error
	// Due возвращает пачку записей, у которых подошло время доставки.
	Due(ctx context.Context, now time.Time, limit int) ([]PendingPush, error)
	// Delete убирает запись (успех либо исчерпаны попытки).
	Delete(ctx context.Context, id string) error
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
type Sender interface {
	SendToUser(ctx context.Context, user api.User, n Notification)
}

// NoopSender — заглушка, когда FCM/очередь не сконфигурированы: молча ничего не
// делает, остальной сервер работает как раньше.
type NoopSender struct{}

func (NoopSender) SendToUser(context.Context, api.User, Notification) {}

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

func (s *queueSender) SendToUser(ctx context.Context, user api.User, n Notification) {
	if err := s.store.Enqueue(ctx, user.ID, n); err != nil {
		log.Error().Err(err).Int("user", user.ID).Msg("push: enqueue failed")
	}
}

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
func (w *Worker) deliver(ctx context.Context, p PendingPush) {
	user, err := w.users.FindById(ctx, p.UserID)
	if err != nil {
		w.retry(ctx, p) // не смогли прочитать пользователя — ещё попытка
		return
	}
	if user == nil || len(user.PushTokens) == 0 {
		_ = w.store.Delete(ctx, p.ID) // некому слать — не держим запись
		return
	}

	messages := make([]*messaging.Message, 0, len(user.PushTokens))
	for _, t := range user.PushTokens {
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

	resp, err := w.client.SendEach(ctx, messages)
	if err != nil {
		log.Warn().Err(err).Int("user", p.UserID).Msg("push: SendEach failed, retrying")
		w.retry(ctx, p)
		return
	}

	transient := false
	for i, r := range resp.Responses {
		if r.Success {
			continue
		}
		token := user.PushTokens[i].Token
		switch {
		case messaging.IsUnregistered(r.Error) || messaging.IsInvalidArgument(r.Error):
			// Мёртвый/битый токен — чистим, не ретраим (это не сбой доставки).
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
	_ = w.store.Delete(ctx, p.ID)
}

func (w *Worker) retry(ctx context.Context, p PendingPush) {
	attempts := p.Attempts + 1
	if attempts >= maxAttempts {
		log.Warn().Int("user", p.UserID).Int("attempts", attempts).Msg("push: giving up after max attempts")
		_ = w.store.Delete(ctx, p.ID)
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
