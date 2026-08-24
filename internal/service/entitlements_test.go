package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

type fakeSubs struct {
	byUser map[int][]api.Subscription
	err    error
	calls  int
}

func (f *fakeSubs) ActiveByUser(_ context.Context, userId int) ([]api.Subscription, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byUser[userId], nil
}

var testNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

func newTestEntitlements(subs SubscriptionReader, tweak func(*EntitlementsConfig)) *Entitlements {
	cfg := EntitlementsConfig{
		FreeQuota:     5,
		PlusQuota:     UnlimitedQuota,
		LegacyQuota:   50,
		DeliverySlack: 2 * time.Hour,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	e := NewEntitlements(subs, cfg)
	e.now = fixedNow(testNow)
	return e
}

func sub(expires time.Time) api.Subscription {
	return api.Subscription{
		UserId:    1,
		Store:     api.StoreApple,
		StoreRef:  "orig-1",
		ExpiresAt: expires,
		AutoRenew: true,
	}
}

func TestEntitlementsTier(t *testing.T) {
	revoked := sub(testNow.Add(30 * 24 * time.Hour))
	revokedAt := testNow.Add(-time.Hour)
	revoked.RevokedAt = &revokedAt

	superseded := sub(testNow.Add(30 * 24 * time.Hour))
	supersededAt := testNow.Add(-time.Hour)
	superseded.SupersededAt = &supersededAt

	tests := []struct {
		name string
		subs []api.Subscription
		want api.Tier
	}{
		{"нет подписок", nil, api.TierFree},
		{"активная", []api.Subscription{sub(testNow.Add(24 * time.Hour))}, api.TierPlus},
		{
			// Уведомление о продлении могло задержаться — час просрочки не повод
			// выгонять платящего.
			"истекла час назад, в пределах запаса на доставку",
			[]api.Subscription{sub(testNow.Add(-time.Hour))},
			api.TierPlus,
		},
		{
			"истекла давно, запас исчерпан",
			[]api.Subscription{sub(testNow.Add(-3 * time.Hour))},
			api.TierFree,
		},
		{"возврат денег снимает Plus немедленно", []api.Subscription{revoked}, api.TierFree},
		{"заменённая новым токеном не считается", []api.Subscription{superseded}, api.TierFree},
		{
			"одна протухшая, одна живая — Plus",
			[]api.Subscription{sub(testNow.Add(-10 * 24 * time.Hour)), sub(testNow.Add(24 * time.Hour))},
			api.TierPlus,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEntitlements(&fakeSubs{byUser: map[int][]api.Subscription{1: tc.subs}}, nil)
			got, err := e.Tier(context.Background(), 1)
			if err != nil {
				t.Fatalf("Tier: %v", err)
			}
			if got != tc.want {
				t.Errorf("Tier = %q, хотели %q", got, tc.want)
			}
		})
	}
}

// TestEntitlementsCompUsersSkipRepository — comp-список отвечает до похода в
// базу: демо-аккаунт ревьюера обязан работать даже при недоступной коллекции.
func TestEntitlementsCompUsersSkipRepository(t *testing.T) {
	subs := &fakeSubs{err: errors.New("mongo down")}
	e := newTestEntitlements(subs, func(c *EntitlementsConfig) { c.CompUserIds = []int{77} })

	got, err := e.Tier(context.Background(), 77)
	if err != nil {
		t.Fatalf("Tier: %v", err)
	}
	if got != api.TierPlus {
		t.Errorf("Tier = %q, хотели plus", got)
	}
	if subs.calls != 0 {
		t.Error("comp-пользователь ходил в базу")
	}
}

// TestEntitlementsFailsClosed — сбой чтения даёт free, а не plus.
//
// Обратное означало бы, что лежащая база раздаёт платное всем.
func TestEntitlementsFailsClosed(t *testing.T) {
	boom := errors.New("mongo down")
	e := newTestEntitlements(&fakeSubs{err: boom}, nil)

	tier, err := e.Tier(context.Background(), 1)
	if !errors.Is(err, boom) {
		t.Fatalf("ошибка не возвращена: %v", err)
	}
	if tier != api.TierFree {
		t.Errorf("Tier = %q, хотели free — сбой не должен раздавать Plus", tier)
	}

	if got := e.TierOrFree(context.Background(), 1); got != api.TierFree {
		t.Errorf("TierOrFree = %q, хотели free", got)
	}
}

// TestEntitlementsCacheAvoidsRepeatedReads — тариф резолвится на каждом
// распознавании, поэтому кеш обязателен; и он обязан истекать.
func TestEntitlementsCacheAvoidsRepeatedReads(t *testing.T) {
	subs := &fakeSubs{byUser: map[int][]api.Subscription{1: {sub(testNow.Add(24 * time.Hour))}}}
	e := newTestEntitlements(subs, func(c *EntitlementsConfig) { c.CacheTTL = time.Minute })

	for i := 0; i < 5; i++ {
		if _, err := e.Tier(context.Background(), 1); err != nil {
			t.Fatalf("Tier: %v", err)
		}
	}
	if subs.calls != 1 {
		t.Errorf("обращений к базе %d, хотели 1", subs.calls)
	}

	e.now = fixedNow(testNow.Add(2 * time.Minute))
	if _, err := e.Tier(context.Background(), 1); err != nil {
		t.Fatalf("Tier после истечения кеша: %v", err)
	}
	if subs.calls != 2 {
		t.Errorf("после истечения TTL обращений %d, хотели 2", subs.calls)
	}
}

// TestEntitlementsInvalidateShowsPurchaseImmediately — человек, который только
// что заплатил, видит Plus сразу, а не через минуту.
func TestEntitlementsInvalidateShowsPurchaseImmediately(t *testing.T) {
	subs := &fakeSubs{byUser: map[int][]api.Subscription{}}
	e := newTestEntitlements(subs, func(c *EntitlementsConfig) { c.CacheTTL = time.Hour })

	if tier, _ := e.Tier(context.Background(), 1); tier != api.TierFree {
		t.Fatalf("до покупки хотели free, получили %q", tier)
	}

	subs.byUser[1] = []api.Subscription{sub(testNow.Add(30 * 24 * time.Hour))}

	if tier, _ := e.Tier(context.Background(), 1); tier != api.TierFree {
		t.Fatalf("кеш должен ещё держать free, получили %q", tier)
	}

	e.Invalidate(1)
	if tier, _ := e.Tier(context.Background(), 1); tier != api.TierPlus {
		t.Errorf("после сброса кеша хотели plus, получили %q", tier)
	}
}

func TestEntitlementsQuotaFor(t *testing.T) {
	e := newTestEntitlements(&fakeSubs{}, nil)

	tests := []struct {
		name         string
		tier         api.Tier
		knowsPaywall bool
		want         int
	}{
		{"free на свежей сборке", api.TierFree, true, 5},
		{"plus на свежей сборке", api.TierPlus, true, UnlimitedQuota},
		// Сборка без экрана оплаты: урезать её до пяти значило бы сломать
		// распознавание не обновившимся, не дав пути заплатить.
		{"free на старой сборке", api.TierFree, false, 50},
		{"plus на старой сборке", api.TierPlus, false, UnlimitedQuota},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.QuotaFor(tc.tier, tc.knowsPaywall); got != tc.want {
				t.Errorf("QuotaFor = %d, хотели %d", got, tc.want)
			}
		})
	}
}
