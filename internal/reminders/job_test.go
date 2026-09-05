package reminders

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
)

type fakeRooms struct {
	rooms []api.Room
	// since — граница, которую джоб попросил у хранилища: правило «только тусы
	// моложе двух месяцев» держится именно на ней
	since time.Time
}

func (f *fakeRooms) EachRoomCreatedAfter(_ context.Context, since time.Time, batch int, fn func([]api.Room) error) error {
	f.since = since
	var fresh []api.Room
	for _, r := range f.rooms {
		if !r.CreateAt.Before(since) {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	return fn(fresh)
}

type fakeState struct {
	mu       sync.Mutex
	claims   map[int]int
	released map[int]int
	deny     map[int]bool
	failNext bool
}

func newFakeState() *fakeState {
	return &fakeState{claims: map[int]int{}, released: map[int]int{}, deny: map[int]bool{}}
}

func (f *fakeState) Claim(_ context.Context, userId int, _ string, _ time.Time, _ time.Duration, _ int) (bool, *api.DebtReminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		return false, nil, errors.New("база недоступна")
	}
	if f.deny[userId] {
		return false, nil, nil
	}
	f.claims[userId]++
	return true, nil, nil
}

func (f *fakeState) Release(_ context.Context, userId int, _ *api.DebtReminder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released[userId]++
	return nil
}

func (f *fakeState) Reset(context.Context, int) error { return nil }

type fakeUsers struct{ users map[int]api.User }

func (f *fakeUsers) FindById(_ context.Context, id int) (*api.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	return &u, nil
}

type fakeQueue struct {
	mu   sync.Mutex
	sent []push.Notification
	fail bool
}

func (f *fakeQueue) Enqueue(_ context.Context, _ int, _ string, n push.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("очередь недоступна")
	}
	f.sent = append(f.sent, n)
	return nil
}

// pushable — человек с включёнными уведомлениями и живым токеном.
func pushable(u api.User) api.User {
	u.PushTokens = []api.PushToken{{Token: "t"}}
	return u
}

func jobFor(t *testing.T, cfg Config, rooms []api.Room, users map[int]api.User) (*Job, *fakeState, *fakeQueue) {
	t.Helper()
	state := newFakeState()
	queue := &fakeQueue{}
	return NewJob(cfg, &fakeRooms{rooms: rooms}, state, &fakeUsers{users: users}, queue), state, queue
}

func onConfig() Config {
	cfg := DefaultConfig()
	cfg.Mode = ModeOn
	return cfg
}

func TestJobSendsToDebtor(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, state, queue := jobFor(t, onConfig(), rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
	})

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}
	if stats.Sent != 1 {
		t.Fatalf("отправлено %d: %+v", stats.Sent, stats)
	}
	if len(queue.sent) != 1 {
		t.Fatalf("в очереди %d пушей", len(queue.sent))
	}
	if state.claims[almaz.ID] != 1 {
		t.Errorf("право взяли %d раз", state.claims[almaz.ID])
	}
	// Кредитору напоминать не о чем.
	if state.claims[zagir.ID] != 0 {
		t.Error("напомнили кредитору")
	}
	// Без roomId тап по уведомлению не открывает ничего.
	if queue.sent[0].Data["roomId"] == "" {
		t.Error("пуш без комнаты")
	}
	if queue.sent[0].Data["channel"] != "debts" {
		t.Errorf("канал = %q", queue.sent[0].Data["channel"])
	}
}

// Выключенные уведомления, отсутствие токенов и удалённый аккаунт — три причины
// молчать, и все три обязаны отсекать ДО захвата права.
func TestJobRespectsUserState(t *testing.T) {
	now := time.Now().UTC()
	off := false
	silent := almaz
	silent.NotificationOn = &off

	noTokens := almaz

	deleted := pushable(almaz)
	deleted.DeletedAt = &now

	for name, user := range map[string]api.User{
		"выключил уведомления": pushable(silent),
		"нет токенов":          noTokens,
		"удалённый аккаунт":    deleted,
	} {
		rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}
		job, state, queue := jobFor(t, onConfig(), rooms, map[int]api.User{
			zagir.ID: pushable(zagir),
			almaz.ID: user,
		})

		if _, err := job.Run(context.Background(), now); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(queue.sent) != 0 {
			t.Errorf("%s: пуш всё-таки ушёл", name)
		}
		if state.claims[almaz.ID] != 0 {
			t.Errorf("%s: право взяли зря — попытка сгорела бы впустую", name)
		}
	}
}

// Провал очереди не должен стоить человеку попытки: право возвращается.
func TestJobReleasesClaimWhenQueueFails(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, state, queue := jobFor(t, onConfig(), rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
	})
	queue.fail = true

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон не должен падать из-за одного человека: %v", err)
	}
	if stats.Sent != 0 {
		t.Errorf("отправлено %d, хотя очередь недоступна", stats.Sent)
	}
	if state.released[almaz.ID] != 1 {
		t.Error("право не возвращено — серия из четырёх выгорела бы вхолостую")
	}
}

// Право уже занято (кто-то опередил или человек не остыл) — молчим.
func TestJobSkipsWhenClaimDenied(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	job, state, queue := jobFor(t, onConfig(), rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
	})
	state.deny[almaz.ID] = true

	stats, _ := job.Run(context.Background(), now)
	if len(queue.sent) != 0 || stats.Sent != 0 {
		t.Error("отправили, хотя право не досталось")
	}
}

// Холостой прогон считает, но не шлёт и не трогает состояние — иначе он сжёг бы
// людям попытки ещё до включения рассылки.
func TestDryRunTouchesNothing(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)}

	cfg := DefaultConfig()
	cfg.Mode = ModeDry
	job, state, queue := jobFor(t, cfg, rooms, map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
	})

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("холостой прогон: %v", err)
	}
	if stats.Debtors != 1 {
		t.Errorf("должников %d — холостой прогон обязан их посчитать", stats.Debtors)
	}
	if len(queue.sent) != 0 {
		t.Error("холостой прогон отправил пуш")
	}
	if len(state.claims) != 0 {
		t.Error("холостой прогон занял право")
	}
}

// Час запуска: следующий прогон всегда впереди, а не в прошлом.
func TestUntilNextRun(t *testing.T) {
	cases := []struct {
		now  string
		hour int
		want time.Duration
	}{
		{"2026-08-18T10:00:00Z", 15, 5 * time.Hour},
		{"2026-08-18T15:00:00Z", 15, 24 * time.Hour}, // ровно в час — ждём следующие сутки
		{"2026-08-18T20:00:00Z", 15, 19 * time.Hour},
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		if got := untilNextRun(now, c.hour); got != c.want {
			t.Errorf("%s: ждём %v, ожидалось %v", c.now, got, c.want)
		}
	}
}

// Тусы старше двух месяцев не трогаем вовсе — это исходное условие рассылки:
// напоминать о поездке, с которой все давно разъехались, поздно и неуместно.
//
// Отсечка живёт в запросе к базе (EachRoomCreatedAfter), поэтому проверяем и
// сам результат, и границу, которую джоб просит у хранилища: без этого правило
// держалось на одной строке, которую нечем было защитить.
func TestJobIgnoresOldRooms(t *testing.T) {
	now := time.Now().UTC()
	rooms := []api.Room{
		room("Прошлогодняя", "RUB", now.AddDate(0, 0, -70), zagir, zagir, almaz),
		room("Свежая", "RUB", now.AddDate(0, 0, -3), zagir, zagir, sanya),
	}

	store := &fakeRooms{rooms: rooms}
	state := newFakeState()
	queue := &fakeQueue{}
	job := NewJob(onConfig(), store, state, &fakeUsers{users: map[int]api.User{
		zagir.ID: pushable(zagir),
		almaz.ID: pushable(almaz),
		sanya.ID: pushable(sanya),
	}}, queue)

	stats, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("прогон: %v", err)
	}

	if stats.Rooms != 1 {
		t.Errorf("в обход попало %d тус, ожидалась одна свежая", stats.Rooms)
	}
	// Должник старой тусы молчит, должник свежей — получает
	if state.claims[almaz.ID] != 0 {
		t.Errorf("напомнили про тусу семидесятидневной давности")
	}
	if state.claims[sanya.ID] != 1 {
		t.Errorf("должник свежей тусы остался без напоминания")
	}

	// Граница — ровно два месяца назад, а не «примерно»
	want := now.Add(-60 * 24 * time.Hour)
	if diff := store.since.Sub(want); diff > time.Second || diff < -time.Second {
		t.Errorf("у базы попросили тусы с %v, ожидалось %v", store.since, want)
	}
}
