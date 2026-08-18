package reminders

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
	"github.com/rs/zerolog/log"
)

// Mode — режим работы джоба.
type Mode string

const (
	// ModeOff — джоб не запускается вовсе (значение по умолчанию).
	ModeOff Mode = "off"
	// ModeDry — считает и логирует агрегаты, но ничего не шлёт и не пишет.
	// Рассылка по живым людям включается отдельным шагом, после того как на
	// проде видно, кого и сколько она заденет.
	ModeDry Mode = "dry"
	ModeOn  Mode = "on"
)

// Config — настройки рассылки.
type Config struct {
	Mode Mode
	// Hour — час UTC, в который идёт рассылка. Часового пояса человека в модели
	// нет, так что это компромисс под основную аудиторию, а не гарантия: в
	// UTC+9 полдень по UTC — уже вечер.
	Hour int
	// MaxRoomAge — насколько старые комнаты вообще смотрим.
	MaxRoomAge time.Duration
	// MaxIdle — сколько группа может молчать, прежде чем считаться мёртвой.
	MaxIdle time.Duration
	// Cooldown — минимум между напоминаниями одному человеку.
	Cooldown time.Duration
	// MaxStreak — сколько напоминаний подряд отправляем, прежде чем замолчать.
	MaxStreak int
	// Batch — размер порции комнат.
	Batch int
	// SkipUsers — кого не трогаем вовсе. Сюда идёт демо-аккаунт ревьюеров App
	// Store: это не человек, а витрина, и напоминание о долге ему бессмысленно
	SkipUsers []int
}

// DefaultConfig — значения по умолчанию: рассылка выключена.
func DefaultConfig() Config {
	return Config{
		Mode:       ModeOff,
		Hour:       15, // 18:00 МСК
		MaxRoomAge: 60 * 24 * time.Hour,
		MaxIdle:    60 * 24 * time.Hour,
		Cooldown:   7 * 24 * time.Hour,
		MaxStreak:  4,
		Batch:      50,
	}
}

// Rooms — обход комнат порциями.
type Rooms interface {
	EachRoomCreatedAfter(ctx context.Context, since time.Time, batch int, fn func([]api.Room) error) error
}

// State — память о напоминаниях.
type State interface {
	Claim(ctx context.Context, userId int, debtsKey string, now time.Time, cooldown time.Duration, maxStreak int) (bool, *api.DebtReminder, error)
	Release(ctx context.Context, userId int, previous *api.DebtReminder) error
	Reset(ctx context.Context, userId int) error
}

// Users читает канонический документ: настройки уведомлений и токены лежат
// только там, во встроенных снимках комнат их нет.
type Users interface {
	FindById(ctx context.Context, id int) (*api.User, error)
}

// Telegram — запасной канал доставки. Пуш умеет только приложение, а
// подавляющее большинство должников живёт в боте: без этого канала рассылка
// проходит мимо девяти человек из десяти (замерено на проде: из 47 должников
// приложение стоит у пятерых).
//
// Ошибку возвращает наружу: джобу она нужна, чтобы вернуть человеку попытку —
// списывать её за нашу неудачу нельзя
type Telegram interface {
	SendDebtReminder(ctx context.Context, user *api.User, text string, roomId string) error
}

// Queue — очередь пушей. Именно очередь, а не push.Sender: Sender глотает
// ошибку постановки, а джобу она нужна — иначе он спишет попытку человеку,
// которому ничего не ушло.
type Queue interface {
	Enqueue(ctx context.Context, userID int, n push.Notification) error
}

// Job — рассылка напоминаний о невозвращённом долге.
type Job struct {
	cfg   Config
	rooms Rooms
	state State
	users Users
	queue Queue
	// telegram опционален: nil — бот не запущен в этом процессе, и канал
	// недоступен. Тогда рассылка работает как раньше, только по пушам
	telegram Telegram
}

func NewJob(cfg Config, rooms Rooms, state State, users Users, queue Queue) *Job {
	return &Job{cfg: cfg, rooms: rooms, state: state, users: users, queue: queue}
}

// SetTelegram включает запасной канал. Зовётся только когда бот ДЕЙСТВИТЕЛЬНО
// поднят: с заглушкой отправки джоб считал бы недоставленное доставленным
func (j *Job) SetTelegram(t Telegram) { j.telegram = t }

// Run — один прогон рассылки. Возвращает сводку (её же печатает dry-режим).
type Stats struct {
	Rooms   int
	Debtors int
	Sent    int
	// ByPush/ByTelegram — каким каналом ушло. Разбивка нужна не для красоты:
	// пуш и телеграм расходятся по охвату в разы, и «отправлено 5» без неё
	// выглядит нормальной рассылкой
	ByPush      int
	ByTelegram  int
	SkippedRoom int
	// SkippedUser — не подошли: выключены уведомления, нет ни одного канала,
	// удалён аккаунт или ещё не остыло предыдущее напоминание.
	SkippedUser int
}

func (j *Job) Run(ctx context.Context, now time.Time) (Stats, error) {
	var stats Stats

	collector := &Collector{Now: now, MaxIdle: j.cfg.MaxIdle}
	err := j.rooms.EachRoomCreatedAfter(ctx, now.Add(-j.cfg.MaxRoomAge), j.cfg.Batch, func(rooms []api.Room) error {
		stats.Rooms += len(rooms)
		collector.Add(rooms)
		return nil
	})
	if err != nil {
		return stats, err
	}
	stats.SkippedRoom = collector.Skipped

	targets := collector.Targets()
	stats.Debtors = len(targets)

	for _, target := range targets {
		channel, err := j.remind(ctx, target, now)
		if err != nil {
			// Один человек не должен ронять всю рассылку.
			log.Error().Err(err).Int("user", target.UserId).Msg("debt reminder failed")
			continue
		}
		switch channel {
		case channelPush:
			stats.Sent++
			stats.ByPush++
		case channelTelegram:
			stats.Sent++
			stats.ByTelegram++
		default:
			stats.SkippedUser++
		}
	}

	// Агрегаты, и только они: имена и точные суммы в продовый лог не уходят —
	// уровень info выбран ровно ради этого.
	log.Info().
		Str("mode", string(j.cfg.Mode)).
		Int("rooms", stats.Rooms).
		Int("debtors", stats.Debtors).
		Int("sent", stats.Sent).
		Int("by_push", stats.ByPush).
		Int("by_telegram", stats.ByTelegram).
		Int("skipped_rooms", stats.SkippedRoom).
		Int("skipped_users", stats.SkippedUser).
		Msg("debt reminders")

	return stats, nil
}

// channel — каким каналом ушло напоминание.
type channel int

const (
	channelNone channel = iota
	channelPush
	channelTelegram
)

// skipped — аккаунт, которому рассылка не адресована вовсе.
func (j *Job) skipped(userId int) bool {
	for _, id := range j.cfg.SkipUsers {
		if id == userId {
			return true
		}
	}
	return false
}

// pickChannel выбирает канал доставки. Пуш первым, телеграм запасным — а НЕ
// оба сразу: уведомление о расходе продублировать не жалко, а напоминание о
// долге, пришедшее дважды, читается как претензия.
//
// Телеграм берётся и тогда, когда пуши человек выключил сам, а телеграм
// оставил: это его выбор канала, а не отказ от напоминаний
func (j *Job) pickChannel(user *api.User) channel {
	if user.WantsPush(api.NotifyDebts) && len(user.PushTokens) > 0 {
		return channelPush
	}
	if j.telegram != nil && user.HasTelegram() && user.AllowsTelegram(api.NotifyDebts) {
		return channelTelegram
	}
	return channelNone
}

// remind обрабатывает одного человека и возвращает канал, которым ушло
// напоминание (channelNone — не ушло).
func (j *Job) remind(ctx context.Context, target Target, now time.Time) (channel, error) {
	user, err := j.users.FindById(ctx, target.UserId)
	if err != nil {
		return channelNone, err
	}
	if user == nil || user.IsDeleted() {
		return channelNone, nil
	}

	if j.skipped(user.ID) {
		return channelNone, nil
	}

	via := j.pickChannel(user)
	if via == channelNone {
		return channelNone, nil
	}

	if j.cfg.Mode == ModeDry {
		// Считаем, но не трогаем ни очередь, ни состояние: иначе «холостой»
		// прогон сжёг бы людям попытки.
		return via, nil
	}

	// Право забирается ДО отправки и одной операцией: два инстанса или
	// перекрывающийся деплой иначе прислали бы два одинаковых напоминания.
	claimed, previous, err := j.state.Claim(ctx, target.UserId, target.Key, now, j.cfg.Cooldown, j.cfg.MaxStreak)
	if err != nil {
		return channelNone, err
	}
	if !claimed {
		return channelNone, nil
	}

	lang := api.DefineLang(user)
	if via == channelPush {
		err = j.queue.Enqueue(ctx, target.UserId, push.Notification{
			Title: Title(lang),
			Body:  Body(target, lang),
			Data:  PushData(target),
		})
	} else {
		err = j.telegram.SendDebtReminder(ctx, user, Body(target, lang), target.RoomId)
	}
	if err != nil {
		// Не ушло — возвращаем право: списывать человеку попытку за нашу же
		// неудачу нельзя, иначе серия из четырёх выгорит вхолостую.
		if rErr := j.state.Release(ctx, target.UserId, previous); rErr != nil {
			log.Error().Err(rErr).Int("user", target.UserId).Msg("cannot release debt reminder claim")
		}
		return channelNone, err
	}
	return via, nil
}

// Start запускает суточный цикл. Блокирующий вызов — звать из горутины.
func (j *Job) Start(ctx context.Context) {
	if j.cfg.Mode == ModeOff {
		log.Info().Msg("напоминания о долгах выключены (DEBT_REMINDERS=off)")
		return
	}
	log.Info().Str("mode", string(j.cfg.Mode)).Int("hour_utc", j.cfg.Hour).Msg("напоминания о долгах включены")

	for {
		wait := untilNextRun(time.Now().UTC(), j.cfg.Hour)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if _, err := j.Run(ctx, time.Now().UTC()); err != nil {
			log.Error().Err(err).Msg("прогон напоминаний о долгах не удался")
		}
	}
}

// untilNextRun — сколько ждать до ближайшего запуска в hour UTC.
func untilNextRun(now time.Time, hour int) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}
