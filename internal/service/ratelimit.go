package service

import (
	"context"
	"fmt"
	"time"
)

// UnlimitedQuota — суточный лимит снят (тариф Plus).
//
// Именно -1, а не 0: ноль пришлось бы трактовать как «безлимит», и тогда любая
// пустая или битая переменная окружения тихо раздавала бы безлимит всем. С -1
// неверное значение — это ноль разрешённых распознаваний, то есть заметная
// поломка, а не бесплатная раздача. Конфиг дополнительно отвергает 0 на старте.
const UnlimitedQuota = -1

// UsageCounter — окна обращений (реализуется repository.MongoAiUsageRepository).
type UsageCounter interface {
	// Incr увеличивает счётчик окна key и возвращает новое значение.
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	// Get читает счётчик окна, НЕ изменяя его. Окна нет — 0.
	Get(ctx context.Context, key string) (int64, error)
}

// Decision — результат проверки лимита.
//
// Kind отличает «подожди минуту» от «сутки кончились»: на первое клиент
// показывает тост, на второе — экран оплаты. Пока это была одна причина,
// человек, тыкнувший микрофон дважды подряд, получал бы предложение заплатить.
type Decision struct {
	Allowed  bool
	Kind     string // "" | KindMinute | KindDaily
	Used     int64
	Limit    int
	ResetsAt time.Time
}

const (
	KindMinute = "minute"
	KindDaily  = "daily"
)

// Unlimited — суточного потолка нет.
func (d Decision) Unlimited() bool { return d.Limit == UnlimitedQuota }

// Remaining — сколько распознаваний осталось на сегодня. Никогда не
// отрицательное: счётчик окна может обогнать лимит в гонке, но показывать
// человеку «осталось -3 из 5» нельзя.
func (d Decision) Remaining() int64 {
	if d.Unlimited() {
		return 0
	}
	left := int64(d.Limit) - d.Used
	if left < 0 {
		return 0
	}
	return left
}

// RateLimiter ограничивает частоту AI-распознавания: не более ratePerMin
// запросов в минуту (защита от лавины, одинакова для всех тарифов) и
// dailyQuota в сутки (это и есть платная граница).
//
// Суточную квоту limiter НЕ хранит: она зависит от тарифа и приходит
// параметром. Держать её ещё и полем значило бы завести второй источник правды.
type RateLimiter struct {
	counter    UsageCounter
	ratePerMin int
	now        func() time.Time // подменяется в тестах
}

func NewRateLimiter(c UsageCounter, ratePerMin int) *RateLimiter {
	return &RateLimiter{counter: c, ratePerMin: ratePerMin, now: time.Now}
}

// AllowParse проверяет и учитывает один запрос пользователя.
//
// Порядок проверок — не деталь реализации, а требование к продукту.
// Раньше минутное окно проверялось первым, и при равных лимитах (5 в минуту,
// 5 в сутки) человек, потративший суточную норму за одну минуту, получал на
// шестом запросе «слишком часто» вместо экрана оплаты: суточная граница внутри
// минуты была недостижима, и paywall не открывался на самом частом сценарии.
//
// Теперь суточный остаток СЧИТЫВАЕТСЯ первым (без инкремента), и только если
// он есть — проверяется минутное окно. Побочный эффект: отклонённый по минуте
// запрос по-прежнему не тратит суточную квоту, а счётчик суток не убегает за
// лимит на отказах.
//
// Шаг 3 — страховка от гонки: между чтением и инкрементом мог проскочить
// параллельный запрос. Цена гонки — одно лишнее распознавание, не больше.
func (rl *RateLimiter) AllowParse(ctx context.Context, userId, dailyQuota int) (Decision, error) {
	now := rl.now().UTC()
	resetsAt := startOfNextUTCDay(now)
	dayKey := fmt.Sprintf("%d:day:%s", userId, now.Format("2006-01-02"))

	dec := Decision{Limit: dailyQuota, ResetsAt: resetsAt}

	if dailyQuota != UnlimitedQuota {
		used, err := rl.counter.Get(ctx, dayKey)
		if err != nil {
			return Decision{}, err
		}
		if used >= int64(dailyQuota) {
			dec.Used = int64(dailyQuota)
			dec.Kind = KindDaily
			return dec, nil
		}
		dec.Used = used
	}

	minKey := fmt.Sprintf("%d:min:%d", userId, now.Unix()/60)
	n, err := rl.counter.Incr(ctx, minKey, 2*time.Minute)
	if err != nil {
		return Decision{}, err
	}
	if n > int64(rl.ratePerMin) {
		dec.Kind = KindMinute
		return dec, nil
	}

	if dailyQuota == UnlimitedQuota {
		// Plus вообще не пишет суточный счётчик: считать нечего, а лишняя
		// запись в mongo на каждое распознавание — плата ни за что.
		dec.Allowed = true
		return dec, nil
	}

	d, err := rl.counter.Incr(ctx, dayKey, 25*time.Hour)
	if err != nil {
		return Decision{}, err
	}
	dec.Used = d
	if d > int64(dailyQuota) {
		dec.Used = int64(dailyQuota)
		dec.Kind = KindDaily
		return dec, nil
	}

	dec.Allowed = true
	return dec, nil
}

// Peek возвращает состояние суточной квоты, НЕ расходуя её: этим живёт
// счётчик остатка в интерфейсе и GET /me/ai-quota.
func (rl *RateLimiter) Peek(ctx context.Context, userId, dailyQuota int) (Decision, error) {
	now := rl.now().UTC()
	dec := Decision{Allowed: true, Limit: dailyQuota, ResetsAt: startOfNextUTCDay(now)}
	if dailyQuota == UnlimitedQuota {
		return dec, nil
	}

	used, err := rl.counter.Get(ctx, fmt.Sprintf("%d:day:%s", userId, now.Format("2006-01-02")))
	if err != nil {
		return Decision{}, err
	}
	if used > int64(dailyQuota) {
		used = int64(dailyQuota)
	}
	dec.Used = used
	dec.Allowed = used < int64(dailyQuota)
	return dec, nil
}

// startOfNextUTCDay — момент обнуления суточного окна.
//
// Именно UTC, и то же самое в ключе счётчика: раньше ключ строился от местного
// времени процесса, поэтому смена часового пояса сервера (или переезд
// контейнера) сдвигала границу суток и могла подарить или отнять день лимита.
func startOfNextUTCDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
}
