package reminders

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

type fakeTelegram struct {
	mu   sync.Mutex
	sent []string
	to   []int
	fail bool
}

func (f *fakeTelegram) SendDebtReminder(_ context.Context, user *api.User, text string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("telegram недоступен")
	}
	f.sent = append(f.sent, text)
	f.to = append(f.to, user.ID)
	return nil
}

// inTelegram — человек, который живёт в боте: приложения нет, telegram есть.
// Таких на проде девять из десяти должников.
func inTelegram(u api.User) api.User {
	id := 100000 + u.ID
	u.TelegramID = &id
	u.PushTokens = nil
	return u
}

func jobWithTelegram(t *testing.T, rooms []api.Room, users map[int]api.User) (*Job, *fakeState, *fakeQueue, *fakeTelegram) {
	t.Helper()
	job, state, queue := jobFor(t, onConfig(), rooms, users)
	tg := &fakeTelegram{}
	job.SetTelegram(tg)
	return job, state, queue, tg
}

// Должник без приложения обязан получить напоминание в бот: до этого рассылка
// проходила мимо почти всех — пуш умеет только приложение.
func TestJobFallsBackToTelegram(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, state, queue, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: inTelegram(zagir),
		almaz.ID: inTelegram(almaz),
	})

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}
	if stats.Sent != 1 || stats.ByTelegram != 1 || stats.ByPush != 0 {
		t.Fatalf("сводка: %+v", stats)
	}
	if len(tg.sent) != 1 || tg.to[0] != almaz.ID {
		t.Fatalf("в telegram ушло %v (кому %v)", tg.sent, tg.to)
	}
	if len(queue.sent) != 0 {
		t.Errorf("в очередь пушей всё-таки попало %d", len(queue.sent))
	}
	if state.claims[almaz.ID] != 1 {
		t.Errorf("право взяли %d раз", state.claims[almaz.ID])
	}
	// Сумма в тексте — то, ради чего напоминание и шлётся
	if tg.sent[0] == "" {
		t.Error("текст напоминания пуст")
	}
}

// Оба канала сразу — нет: уведомление о расходе продублировать не жалко, а
// напоминание о долге, пришедшее дважды, читается как претензия.
func TestJobPrefersPushOverTelegram(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	withBoth := inTelegram(almaz)
	withBoth.PushTokens = []api.PushToken{{Token: "t"}}

	job, _, queue, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: withBoth,
	})

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}
	if stats.ByPush != 1 || stats.ByTelegram != 0 {
		t.Fatalf("сводка: %+v", stats)
	}
	if len(queue.sent) != 1 {
		t.Errorf("пушей в очереди: %d", len(queue.sent))
	}
	if len(tg.sent) != 0 {
		t.Errorf("продублировали в telegram: %v", tg.sent)
	}
}

// Пуши человек выключил сам, telegram оставил — это выбор канала, а не отказ
// от напоминаний.
func TestJobUsesTelegramWhenPushTurnedOff(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	no := false
	quiet := inTelegram(almaz)
	quiet.PushTokens = []api.PushToken{{Token: "t"}}
	quiet.Notify = &api.NotifySettings{Debts: api.ChannelPrefs{Push: &no}}

	job, _, queue, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: quiet,
	})

	stats, _ := job.Run(context.Background(), now)
	if stats.ByTelegram != 1 || len(queue.sent) != 0 {
		t.Fatalf("сводка %+v, пушей %d", stats, len(queue.sent))
	}
	_ = tg
}

// Выключил и то и другое — значит не беспокоить.
func TestJobSkipsWhenBothChannelsOff(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	no := false
	silent := inTelegram(almaz)
	silent.NotificationOn = &no

	job, state, queue, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: silent,
	})

	stats, _ := job.Run(context.Background(), now)
	if stats.Sent != 0 || stats.SkippedUser != 1 {
		t.Fatalf("сводка: %+v", stats)
	}
	if len(tg.sent) != 0 || len(queue.sent) != 0 {
		t.Errorf("что-то всё-таки ушло: telegram %d, push %d", len(tg.sent), len(queue.sent))
	}
	// Право не забирали: человеку нечего списывать
	if state.claims[almaz.ID] != 0 {
		t.Errorf("право взяли %d раз", state.claims[almaz.ID])
	}
}

// Бот не поднят — канала нет вовсе, и рассылка ведёт себя как раньше.
func TestJobWithoutTelegramChannel(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, _, queue := jobFor(t, onConfig(), rooms, map[int]api.User{
		zagir.ID: inTelegram(zagir),
		almaz.ID: inTelegram(almaz),
	})

	stats, _ := job.Run(context.Background(), now)
	if stats.Sent != 0 || stats.SkippedUser != 1 {
		t.Fatalf("сводка: %+v", stats)
	}
	if len(queue.sent) != 0 {
		t.Errorf("пушей: %d", len(queue.sent))
	}
}

// Telegram не принял сообщение — право обязано вернуться: списывать попытку за
// нашу неудачу нельзя, иначе серия из четырёх выгорит вхолостую.
func TestJobReleasesClaimWhenTelegramFails(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, state, _, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: inTelegram(zagir),
		almaz.ID: inTelegram(almaz),
	})
	tg.fail = true

	stats, _ := job.Run(context.Background(), now)
	if stats.Sent != 0 {
		t.Fatalf("сводка: %+v", stats)
	}
	if state.claims[almaz.ID] != 1 || state.released[almaz.ID] != 1 {
		t.Errorf("право взяли %d раз, вернули %d", state.claims[almaz.ID], state.released[almaz.ID])
	}
}

// Холостой прогон обязан считать канал так же, как боевой: ради этого его и
// запускают — увидеть, скольких заденет рассылка и чем.
func TestDryRunCountsChannels(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{
		room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz),
		room("Дача", "RUB", now.AddDate(0, 0, -2), zagir, zagir, sanya),
	}

	cfg := DefaultConfig()
	cfg.Mode = ModeDry
	job, state, queue := jobFor(t, cfg, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: inTelegram(almaz),
		sanya.ID: pushable(sanya),
	})
	tg := &fakeTelegram{}
	job.SetTelegram(tg)

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}
	if stats.Sent != 2 || stats.ByTelegram != 1 || stats.ByPush != 1 {
		t.Fatalf("сводка: %+v", stats)
	}
	// И при этом ничего не отправлено и не списано
	if len(tg.sent) != 0 || len(queue.sent) != 0 || len(state.claims) != 0 {
		t.Errorf("холостой прогон что-то сделал: tg %d, push %d, прав %d",
			len(tg.sent), len(queue.sent), len(state.claims))
	}
}

// Долг не крупнее числа расходов неотличим от погрешности деления: доли режутся
// с усечением копеек, и каждый расход оставляет до единицы валюты. Напоминать
// по такому — прислать человеку «верните 3 ₽».
func TestJobIgnoresRoundingNoise(t *testing.T) {
	now := time.Now().UTC()
	// Комната с одним расходом на 3 единицы: долг выйдет 1–2, то есть в
	// пределах погрешности
	tiny := room("Мелочь", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)
	(*tiny.Operations)[0].Sum = 3
	for i := range (*tiny.Operations)[0].RecipientsWithSum {
		(*tiny.Operations)[0].RecipientsWithSum[i].Sum = 1.5
	}

	job, state, queue, tg := jobWithTelegram(t, []api.Room{tiny}, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: inTelegram(almaz),
	})

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}
	if stats.Debtors != 0 || stats.Sent != 0 {
		t.Fatalf("напомнили про остаток от округления: %+v", stats)
	}
	if len(tg.sent) != 0 || len(queue.sent) != 0 || len(state.claims) != 0 {
		t.Errorf("что-то ушло: tg %d, push %d", len(tg.sent), len(queue.sent))
	}
}

// Настоящий долг порог не съедает: он отсекает единицы, а не сотни.
func TestJobKeepsRealDebtAboveNoise(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, _, _, tg := jobWithTelegram(t, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: inTelegram(almaz),
	})

	stats, _ := job.Run(context.Background(), now)
	if stats.Sent != 1 || len(tg.sent) != 1 {
		t.Fatalf("настоящий долг потерялся: %+v", stats)
	}
}

// Демо-аккаунт ревьюеров App Store — витрина, а не человек: напоминать ему не о
// чем, и попытку он тратить не должен.
func TestJobSkipsExcludedUsers(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	cfg := onConfig()
	cfg.SkipUsers = []int{almaz.ID}
	job, state, queue := jobFor(t, cfg, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
	})
	tg := &fakeTelegram{}
	job.SetTelegram(tg)

	stats, _ := job.Run(context.Background(), now)
	if stats.Sent != 0 || stats.SkippedUser != 1 {
		t.Fatalf("сводка: %+v", stats)
	}
	if len(queue.sent) != 0 || len(tg.sent) != 0 {
		t.Errorf("демо-аккаунту всё-таки написали")
	}
	if state.claims[almaz.ID] != 0 {
		t.Errorf("право взяли зря")
	}
}
