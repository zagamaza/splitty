package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeCounter — in-memory реализация UsageCounter для тестов.
type fakeCounter struct {
	counts map[string]int64
	err    error
	// incrs считает записи: Plus не должен трогать суточное окно вовсе, и
	// проверить это можно только по числу обращений к счётчику.
	incrs int
}

func newFakeCounter() *fakeCounter { return &fakeCounter{counts: map[string]int64{}} }

func (f *fakeCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.incrs++
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeCounter) Get(_ context.Context, key string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[key], nil
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func newTestLimiter(ratePerMin int, at time.Time) (*RateLimiter, *fakeCounter) {
	counter := newFakeCounter()
	rl := NewRateLimiter(counter, ratePerMin)
	rl.now = fixedNow(at)
	return rl, counter
}

func TestRateLimiterWithinLimits(t *testing.T) {
	rl, _ := newTestLimiter(5, time.Unix(1_000_000, 0))
	for i := 0; i < 5; i++ {
		dec, err := rl.AllowParse(context.Background(), 42, 50)
		if err != nil || !dec.Allowed {
			t.Fatalf("запрос %d должен пройти: %+v err=%v", i, dec, err)
		}
	}
}

// TestRateLimiterDailyBeatsMinuteWhenEqual — ГЛАВНЫЙ сценарий paywall.
//
// Пять распознаваний за одну минуту при лимитах 5/мин и 5/сутки: шестое обязано
// назваться суточным, а не минутным. Раньше минутное окно проверялось первым, и
// при равных лимитах суточная граница внутри минуты была недостижима — человек
// получал «слишком часто» вместо экрана оплаты, то есть заплатить ему просто не
// предлагали.
func TestRateLimiterDailyBeatsMinuteWhenEqual(t *testing.T) {
	rl, _ := newTestLimiter(5, time.Unix(1_000_000, 0))

	for i := 0; i < 5; i++ {
		if dec, err := rl.AllowParse(context.Background(), 42, 5); err != nil || !dec.Allowed {
			t.Fatalf("запрос %d должен пройти: %+v err=%v", i, dec, err)
		}
	}

	dec, err := rl.AllowParse(context.Background(), 42, 5)
	if err != nil {
		t.Fatalf("AllowParse: %v", err)
	}
	if dec.Allowed {
		t.Fatal("шестое распознавание прошло, хотя суточная норма исчерпана")
	}
	if dec.Kind != KindDaily {
		t.Errorf("Kind = %q, хотели %q — иначе клиент покажет тост вместо экрана оплаты", dec.Kind, KindDaily)
	}
	if dec.Remaining() != 0 {
		t.Errorf("Remaining = %d, хотели 0", dec.Remaining())
	}
}

// TestRateLimiterMinuteThrottleDoesNotBurnDailyQuota — отклонённый по минуте
// запрос не расходует суточную норму.
//
// Иначе лавина запросов подряд съедала бы платный лимит, ничего не распознав.
func TestRateLimiterMinuteThrottleDoesNotBurnDailyQuota(t *testing.T) {
	rl, counter := newTestLimiter(2, time.Unix(1_000_000, 0))

	for i := 0; i < 2; i++ {
		if dec, _ := rl.AllowParse(context.Background(), 42, 50); !dec.Allowed {
			t.Fatalf("запрос %d должен пройти", i)
		}
	}

	dec, err := rl.AllowParse(context.Background(), 42, 50)
	if err != nil {
		t.Fatalf("AllowParse: %v", err)
	}
	if dec.Allowed {
		t.Fatal("третий запрос в минуту прошёл")
	}
	if dec.Kind != KindMinute {
		t.Errorf("Kind = %q, хотели %q", dec.Kind, KindMinute)
	}

	dayKey := "42:day:" + time.Unix(1_000_000, 0).UTC().Format("2006-01-02")
	if got := counter.counts[dayKey]; got != 2 {
		t.Errorf("суточный счётчик = %d, хотели 2: минутный отказ не должен его тратить", got)
	}
}

// TestRateLimiterUsedNeverExceedsLimit — счётчик не показывает «использовано
// больше, чем разрешено», а остаток не уходит в минус.
//
// Раньше суточное окно инкрементировалось до сравнения и росло на каждом
// отказе: после десятка попыток человек увидел бы «осталось -7 из 5».
func TestRateLimiterUsedNeverExceedsLimit(t *testing.T) {
	rl, _ := newTestLimiter(100, time.Unix(1_000_000, 0))

	for i := 0; i < 5; i++ {
		if dec, _ := rl.AllowParse(context.Background(), 42, 5); !dec.Allowed {
			t.Fatalf("запрос %d должен пройти", i)
		}
	}
	for i := 0; i < 10; i++ {
		dec, err := rl.AllowParse(context.Background(), 42, 5)
		if err != nil {
			t.Fatalf("AllowParse: %v", err)
		}
		if dec.Used > int64(dec.Limit) {
			t.Fatalf("Used = %d при Limit = %d", dec.Used, dec.Limit)
		}
		if dec.Remaining() < 0 {
			t.Fatalf("Remaining = %d", dec.Remaining())
		}
	}
}

// TestRateLimiterUnlimitedSkipsDailyCounter — Plus не пишет суточное окно.
//
// Считать нечего, а лишняя запись в mongo на каждое распознавание — плата ни
// за что.
func TestRateLimiterUnlimitedSkipsDailyCounter(t *testing.T) {
	rl, counter := newTestLimiter(100, time.Unix(1_000_000, 0))

	for i := 0; i < 20; i++ {
		dec, err := rl.AllowParse(context.Background(), 42, UnlimitedQuota)
		if err != nil || !dec.Allowed {
			t.Fatalf("Plus должен проходить всегда: %+v err=%v", dec, err)
		}
		if !dec.Unlimited() {
			t.Fatal("Decision не помечен безлимитным")
		}
	}

	dayKey := "42:day:" + time.Unix(1_000_000, 0).UTC().Format("2006-01-02")
	if _, written := counter.counts[dayKey]; written {
		t.Error("безлимитный тариф пишет суточный счётчик")
	}
	if counter.incrs != 20 {
		t.Errorf("записей в счётчик %d, хотели 20 (только минутное окно)", counter.incrs)
	}
}

// TestRateLimiterMinuteStillAppliesToUnlimited — защита от лавины действует и
// на Plus: безлимит по суткам не даёт права лить запросы пачками.
func TestRateLimiterMinuteStillAppliesToUnlimited(t *testing.T) {
	rl, _ := newTestLimiter(3, time.Unix(1_000_000, 0))

	for i := 0; i < 3; i++ {
		if dec, _ := rl.AllowParse(context.Background(), 42, UnlimitedQuota); !dec.Allowed {
			t.Fatalf("запрос %d должен пройти", i)
		}
	}
	dec, err := rl.AllowParse(context.Background(), 42, UnlimitedQuota)
	if err != nil {
		t.Fatalf("AllowParse: %v", err)
	}
	if dec.Allowed || dec.Kind != KindMinute {
		t.Errorf("хотели минутный отказ, получили %+v", dec)
	}
}

// TestRateLimiterResetsAtIsUTCMidnight — окно суток обнуляется в полночь UTC,
// и ключ счётчика строится по тому же времени.
//
// Раньше ключ строился от местного времени процесса: смена часового пояса
// сервера сдвигала границу суток и могла подарить или отнять день лимита.
func TestRateLimiterResetsAtIsUTCMidnight(t *testing.T) {
	// 23:30 UTC — в любом часовом поясе восточнее это уже следующие сутки.
	at := time.Date(2026, 8, 24, 23, 30, 0, 0, time.UTC)
	rl, counter := newTestLimiter(10, at)

	dec, err := rl.AllowParse(context.Background(), 42, 5)
	if err != nil {
		t.Fatalf("AllowParse: %v", err)
	}

	want := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	if !dec.ResetsAt.Equal(want) {
		t.Errorf("ResetsAt = %v, хотели %v", dec.ResetsAt, want)
	}
	if _, ok := counter.counts["42:day:2026-08-24"]; !ok {
		t.Errorf("ключ суточного окна построен не по UTC: %v", counter.counts)
	}
}

// TestRateLimiterCounterErrors — сбой счётчика возвращается ошибкой, а не
// «разрешено»: лежащая база не должна открывать безлимит.
func TestRateLimiterCounterErrors(t *testing.T) {
	boom := errors.New("counter down")
	rl, counter := newTestLimiter(5, time.Unix(1_000_000, 0))
	counter.err = boom

	dec, err := rl.AllowParse(context.Background(), 42, 5)
	if !errors.Is(err, boom) {
		t.Fatalf("хотели ошибку счётчика, получили %v", err)
	}
	if dec.Allowed {
		t.Error("при сбое счётчика запрос разрешён")
	}
}

// TestRateLimiterPeekDoesNotConsume — просмотр остатка его не расходует.
func TestRateLimiterPeekDoesNotConsume(t *testing.T) {
	rl, counter := newTestLimiter(10, time.Unix(1_000_000, 0))

	if dec, _ := rl.AllowParse(context.Background(), 42, 5); !dec.Allowed {
		t.Fatal("первый запрос должен пройти")
	}
	before := counter.incrs

	dec, err := rl.Peek(context.Background(), 42, 5)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if dec.Used != 1 || dec.Remaining() != 4 {
		t.Errorf("Peek вернул Used=%d Remaining=%d, хотели 1 и 4", dec.Used, dec.Remaining())
	}
	if counter.incrs != before {
		t.Error("Peek израсходовал квоту")
	}
}

// TestRateLimiterPeekUnlimited — у Plus просмотр не ходит в счётчик и не
// сообщает о пределе.
func TestRateLimiterPeekUnlimited(t *testing.T) {
	rl, _ := newTestLimiter(10, time.Unix(1_000_000, 0))

	dec, err := rl.Peek(context.Background(), 42, UnlimitedQuota)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if !dec.Allowed || !dec.Unlimited() {
		t.Errorf("хотели разрешено и безлимит, получили %+v", dec)
	}
}

// TestRateLimiterSeparatesUsers — счётчики не пересекаются между людьми.
func TestRateLimiterSeparatesUsers(t *testing.T) {
	rl, _ := newTestLimiter(10, time.Unix(1_000_000, 0))

	for i := 0; i < 5; i++ {
		if dec, _ := rl.AllowParse(context.Background(), 1, 5); !dec.Allowed {
			t.Fatalf("запрос %d первого пользователя должен пройти", i)
		}
	}
	if dec, _ := rl.AllowParse(context.Background(), 1, 5); dec.Allowed {
		t.Fatal("первый пользователь должен упереться")
	}
	if dec, _ := rl.AllowParse(context.Background(), 2, 5); !dec.Allowed {
		t.Fatal("второму пользователю чужой лимит не мешает")
	}
}
