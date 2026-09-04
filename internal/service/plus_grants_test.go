package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

type fakeGrants struct {
	byUser map[int]*api.PlusGrant
	err    error
	calls  int
}

func (f *fakeGrants) LiveByUser(_ context.Context, userId int, _ time.Time) (*api.PlusGrant, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byUser[userId], nil
}

func grant(expires time.Time, revoked *time.Time) *api.PlusGrant {
	return &api.PlusGrant{
		UserId:    1,
		Source:    api.GrantSourcePanel,
		ExpiresAt: expires,
		RevokedAt: revoked,
	}
}

// Живой грант даёт Plus, истёкший и отозванный — нет.
func TestEntitlementsGrantResolvesTier(t *testing.T) {
	revoked := testNow.Add(-time.Hour)
	cases := []struct {
		name  string
		grant *api.PlusGrant
		want  api.Tier
	}{
		{"живой", grant(testNow.Add(24*time.Hour), nil), api.TierPlus},
		{"истёкший", grant(testNow.Add(-time.Minute), nil), api.TierFree},
		{"отозванный", grant(testNow.Add(24*time.Hour), &revoked), api.TierFree},
		{"нет гранта", nil, api.TierFree},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEntitlements(&fakeSubs{}, nil)
			e.SetGrants(&fakeGrants{byUser: map[int]*api.PlusGrant{1: c.grant}})

			got, err := e.Tier(context.Background(), 1)
			if err != nil {
				t.Fatalf("Tier: %v", err)
			}
			if got != c.want {
				t.Fatalf("тариф %q, ожидал %q", got, c.want)
			}
		})
	}
}

// Отказ коллекции грантов НЕ разжалует платящего подписчика.
//
// Ради этого гранты и читаются последними: у человека с живой покупкой Plus уже
// найден, и до отказавшего хранилища дело не доходит вовсе.
func TestEntitlementsGrantFailureDoesNotDemoteSubscriber(t *testing.T) {
	subs := &fakeSubs{byUser: map[int][]api.Subscription{1: {sub(testNow.Add(24 * time.Hour))}}}
	grants := &fakeGrants{err: errors.New("коллекция недоступна")}
	e := newTestEntitlements(subs, nil)
	e.SetGrants(grants)

	tier, err := e.Tier(context.Background(), 1)
	if err != nil {
		t.Fatalf("ошибка грантов дошла до платящего: %v", err)
	}
	if tier != api.TierPlus {
		t.Fatalf("платящий разжалован до %q", tier)
	}
	if grants.calls != 0 {
		t.Fatalf("гранты читались, хотя Plus уже был найден: %d", grants.calls)
	}
}

// У неплатящего отказ грантов виден наружу — тот же fail closed, что у подписок.
func TestEntitlementsGrantFailureIsReportedForFreeUser(t *testing.T) {
	e := newTestEntitlements(&fakeSubs{}, nil)
	e.SetGrants(&fakeGrants{err: errors.New("коллекция недоступна")})

	tier, err := e.Tier(context.Background(), 1)
	if err == nil {
		t.Fatal("ошибка чтения грантов проглочена")
	}
	if tier != api.TierFree {
		t.Fatalf("тариф при отказе %q, ожидал free", tier)
	}
}

// Попадание в кеш не читает гранты: тариф считается на каждом распознавании.
func TestEntitlementsGrantNotReadOnCacheHit(t *testing.T) {
	grants := &fakeGrants{byUser: map[int]*api.PlusGrant{1: grant(testNow.Add(24*time.Hour), nil)}}
	e := newTestEntitlements(&fakeSubs{}, func(c *EntitlementsConfig) { c.CacheTTL = time.Minute })
	e.SetGrants(grants)

	for i := 0; i < 3; i++ {
		if _, err := e.Tier(context.Background(), 1); err != nil {
			t.Fatalf("Tier: %v", err)
		}
	}
	if grants.calls != 1 {
		t.Fatalf("обращений к грантам %d, ожидал одно", grants.calls)
	}
}

// Выдача видна сразу после сброса кеша — иначе подарок выглядит поломкой.
func TestEntitlementsInvalidateShowsGrantImmediately(t *testing.T) {
	grants := &fakeGrants{byUser: map[int]*api.PlusGrant{}}
	e := newTestEntitlements(&fakeSubs{}, func(c *EntitlementsConfig) { c.CacheTTL = time.Hour })
	e.SetGrants(grants)

	if tier, _ := e.Tier(context.Background(), 1); tier != api.TierFree {
		t.Fatalf("до выдачи тариф %q", tier)
	}

	grants.byUser[1] = grant(testNow.Add(24*time.Hour), nil)
	e.Invalidate(1)

	if tier, _ := e.Tier(context.Background(), 1); tier != api.TierPlus {
		t.Fatalf("после выдачи и сброса кеша тариф %q", tier)
	}
}

// Без подключённых грантов поведение ровно прежнее.
func TestEntitlementsWithoutGrantsBehavesAsBefore(t *testing.T) {
	e := newTestEntitlements(&fakeSubs{}, nil)

	tier, err := e.Tier(context.Background(), 1)
	if err != nil {
		t.Fatalf("Tier: %v", err)
	}
	if tier != api.TierFree {
		t.Fatalf("тариф %q, ожидал free", tier)
	}
}

// IsComp отличает список в окружении от всех остальных: админскому API это
// нужно, чтобы не выдать comp-аккаунт за подарок.
func TestEntitlementsIsComp(t *testing.T) {
	e := newTestEntitlements(&fakeSubs{}, func(c *EntitlementsConfig) { c.CompUserIds = []int{42} })

	if !e.IsComp(42) {
		t.Fatal("человек из списка не опознан")
	}
	if e.IsComp(1) {
		t.Fatal("посторонний опознан как comp")
	}
}
