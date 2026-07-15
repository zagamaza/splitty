package service

import (
	"context"
	"testing"
	"time"
)

// fakeCounter — in-memory реализация UsageCounter для тестов.
type fakeCounter struct {
	counts map[string]int64
	err    error
}

func newFakeCounter() *fakeCounter { return &fakeCounter{counts: map[string]int64{}} }

func (f *fakeCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.counts[key]++
	return f.counts[key], nil
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestRateLimiter_WithinLimits(t *testing.T) {
	rl := NewRateLimiter(newFakeCounter(), 5, 50)
	rl.now = fixedNow(time.Unix(1_000_000, 0))
	for i := 0; i < 5; i++ {
		ok, reason, err := rl.AllowParse(context.Background(), 42)
		if err != nil || !ok {
			t.Fatalf("запрос %d должен пройти, got ok=%v reason=%q err=%v", i, ok, reason, err)
		}
	}
}

func TestRateLimiter_PerMinuteExceeded(t *testing.T) {
	rl := NewRateLimiter(newFakeCounter(), 3, 50)
	rl.now = fixedNow(time.Unix(1_000_000, 0))
	for i := 0; i < 3; i++ {
		if ok, _, _ := rl.AllowParse(context.Background(), 42); !ok {
			t.Fatalf("запрос %d должен пройти", i)
		}
	}
	ok, reason, _ := rl.AllowParse(context.Background(), 42)
	if ok || reason == "" {
		t.Fatalf("4-й запрос за минуту должен быть отклонён, got ok=%v", ok)
	}
}

func TestRateLimiter_MinuteWindowResets(t *testing.T) {
	fc := newFakeCounter()
	rl := NewRateLimiter(fc, 2, 50)
	base := time.Unix(1_000_000, 0)
	rl.now = fixedNow(base)
	rl.AllowParse(context.Background(), 42)
	rl.AllowParse(context.Background(), 42)
	if ok, _, _ := rl.AllowParse(context.Background(), 42); ok {
		t.Fatal("третий за ту же минуту должен быть отклонён")
	}
	// следующая минута — окно сброшено (новый ключ)
	rl.now = fixedNow(base.Add(61 * time.Second))
	if ok, _, _ := rl.AllowParse(context.Background(), 42); !ok {
		t.Fatal("в новой минуте запрос должен пройти")
	}
}

func TestRateLimiter_DailyQuotaExceeded(t *testing.T) {
	fc := newFakeCounter()
	rl := NewRateLimiter(fc, 1000, 3) // высокий минутный, низкий дневной
	base := time.Unix(1_000_000, 0)
	allowed := 0
	for i := 0; i < 5; i++ {
		rl.now = fixedNow(base.Add(time.Duration(i) * time.Minute)) // разные минуты
		if ok, _, _ := rl.AllowParse(context.Background(), 42); ok {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("должно пройти ровно 3 запроса в сутки, прошло %d", allowed)
	}
}

func TestRateLimiter_CounterError(t *testing.T) {
	fc := newFakeCounter()
	fc.err = context.DeadlineExceeded
	rl := NewRateLimiter(fc, 5, 50)
	rl.now = fixedNow(time.Unix(1_000_000, 0))
	if _, _, err := rl.AllowParse(context.Background(), 42); err == nil {
		t.Fatal("ошибка счётчика должна пробрасываться")
	}
}
