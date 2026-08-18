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
}

func NewJob(cfg Config, rooms Rooms, state State, users Users, queue Queue) *Job {
	return &Job{cfg: cfg, rooms: rooms, state: state, users: users, queue: queue}
}

// Run — один прогон рассылки. Возвращает сводку (её же печатает dry-режим).
type Stats struct {
	Rooms       int
	Debtors     int
	Sent        int
	SkippedRoom int
	// SkippedUser — не подошли: выключены уведомления, нет токенов, удалён
	// аккаунт или ещё не остыл предыдущий пуш.
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
		sent, err := j.remind(ctx, target, now)
		if err != nil {
			// Один человек не должен ронять всю рассылку.
			log.Error().Err(err).Int("user", target.UserId).Msg("debt reminder failed")
			continue
		}
		if sent {
			stats.Sent++
		} else {
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
		Int("skipped_rooms", stats.SkippedRoom).
		Int("skipped_users", stats.SkippedUser).
		Msg("debt reminders")

	return stats, nil
}

// remind обрабатывает одного человека. true — напоминание ушло в очередь.
func (j *Job) remind(ctx context.Context, target Target, now time.Time) (bool, error) {
	user, err := j.users.FindById(ctx, target.UserId)
	if err != nil {
		return false, err
	}
	if user == nil || user.IsDeleted() || !user.WantsPush(api.NotifyDebts) || len(user.PushTokens) == 0 {
		return false, nil
	}

	if j.cfg.Mode == ModeDry {
		// Считаем, но не трогаем ни очередь, ни состояние: иначе «холостой»
		// прогон сжёг бы людям попытки.
		return true, nil
	}

	// Право забирается ДО постановки в очередь и одной операцией: два инстанса
	// или перекрывающийся деплой иначе прислали бы два одинаковых пуша.
	claimed, previous, err := j.state.Claim(ctx, target.UserId, target.Key, now, j.cfg.Cooldown, j.cfg.MaxStreak)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}

	lang := api.DefineLang(user)
	notification := push.Notification{
		Title: Title(lang),
		Body:  Body(target, lang),
		Data:  PushData(target),
	}
	if err := j.queue.Enqueue(ctx, target.UserId, notification); err != nil {
		// В очередь не попало — возвращаем право: списывать человеку попытку
		// за нашу же неудачу нельзя, иначе серия из четырёх выгорит вхолостую.
		if rErr := j.state.Release(ctx, target.UserId, previous); rErr != nil {
			log.Error().Err(rErr).Int("user", target.UserId).Msg("cannot release debt reminder claim")
		}
		return false, err
	}
	return true, nil
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
