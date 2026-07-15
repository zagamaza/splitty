package service

import (
	"context"
	"fmt"
	"time"
)

// UsageCounter атомарный счётчик окон обращений (реализуется repository.
// MongoAiUsageRepository). Возвращает новое значение счётчика окна key.
type UsageCounter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// RateLimiter ограничивает частоту AI-парсинга на пользователя: не более
// ratePerMin запросов в минуту и dailyQuota в сутки. Оба окна считаются
// независимо через UsageCounter.
type RateLimiter struct {
	counter    UsageCounter
	ratePerMin int
	dailyQuota int
	now        func() time.Time // подменяется в тестах
}

func NewRateLimiter(c UsageCounter, ratePerMin, dailyQuota int) *RateLimiter {
	return &RateLimiter{counter: c, ratePerMin: ratePerMin, dailyQuota: dailyQuota, now: time.Now}
}

// AllowParse проверяет и учитывает один запрос пользователя. Возвращает
// (false, причина, nil) при превышении лимита; ошибку — при сбое счётчика.
// Минутное окно проверяется первым — при его превышении суточный счётчик не
// инкрементируется.
func (rl *RateLimiter) AllowParse(ctx context.Context, userId int) (bool, string, error) {
	now := rl.now()

	minKey := fmt.Sprintf("%d:min:%d", userId, now.Unix()/60)
	n, err := rl.counter.Incr(ctx, minKey, 2*time.Minute)
	if err != nil {
		return false, "", err
	}
	if n > int64(rl.ratePerMin) {
		return false, "слишком часто, попробуйте через минуту", nil
	}

	dayKey := fmt.Sprintf("%d:day:%s", userId, now.Format("2006-01-02"))
	d, err := rl.counter.Incr(ctx, dayKey, 25*time.Hour)
	if err != nil {
		return false, "", err
	}
	if d > int64(rl.dailyQuota) {
		return false, "исчерпан дневной лимит распознаваний", nil
	}

	return true, "", nil
}
