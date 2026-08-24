package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/store"
)

type fakeMaintStore struct {
	pending   []api.Subscription
	expiring  []api.Subscription
	upserts   []api.Subscription
	ackStates map[string]string
	revoked   []string
	pendErr   error
	expErr    error
}

func newFakeMaintStore() *fakeMaintStore {
	return &fakeMaintStore{ackStates: map[string]string{}}
}

func (f *fakeMaintStore) PendingAcks(_ context.Context, _ int64) ([]api.Subscription, error) {
	return f.pending, f.pendErr
}

func (f *fakeMaintStore) ExpiringBefore(_ context.Context, _ time.Time, _ int64) ([]api.Subscription, error) {
	return f.expiring, f.expErr
}

func (f *fakeMaintStore) Upsert(_ context.Context, s api.Subscription) error {
	f.upserts = append(f.upserts, s)
	return nil
}

func (f *fakeMaintStore) SetAckState(_ context.Context, _, ref, state string) error {
	f.ackStates[ref] = state
	return nil
}

func (f *fakeMaintStore) MarkRevoked(_ context.Context, _, ref string, _ time.Time) error {
	f.revoked = append(f.revoked, ref)
	return nil
}

type fakeGoogleStatus struct {
	receipt  store.Receipt
	statErr  error
	ackErr   error
	ackCalls int
}

func (f *fakeGoogleStatus) Status(_ context.Context, _ string) (store.Receipt, error) {
	if f.statErr != nil {
		return store.Receipt{}, f.statErr
	}
	return f.receipt, nil
}

func (f *fakeGoogleStatus) Acknowledge(_ context.Context, _ string) error {
	f.ackCalls++
	return f.ackErr
}

type fakeAppleStatus struct {
	receipt store.Receipt
	err     error
	calls   int
}

func (f *fakeAppleStatus) Status(_ context.Context, _, _ string) (store.Receipt, error) {
	f.calls++
	if f.err != nil {
		return store.Receipt{}, f.err
	}
	return f.receipt, nil
}

func googleSub(ref string) api.Subscription {
	return api.Subscription{
		UserId: 1, Store: api.StoreGoogle, StoreRef: ref,
		AckState: api.AckPending, Environment: api.EnvProduction,
	}
}

// TestWorkerRetriesPendingAcknowledgement — воркер добивает подтверждение.
//
// Это последняя линия перед тем, как Google откатит платёж через трое суток:
// и хендлер покупки, и вебхук до этого могли не сработать.
func TestWorkerRetriesPendingAcknowledgement(t *testing.T) {
	subs := newFakeMaintStore()
	subs.pending = []api.Subscription{googleSub("tok-1"), googleSub("tok-2")}
	google := &fakeGoogleStatus{}

	w := NewSubscriptionWorker(subs, nil, google, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if google.ackCalls != 2 {
		t.Errorf("подтверждений %d, хотели 2", google.ackCalls)
	}
	if subs.ackStates["tok-1"] != api.AckDone || subs.ackStates["tok-2"] != api.AckDone {
		t.Errorf("состояния подтверждения: %v", subs.ackStates)
	}
}

// TestWorkerKeepsPendingWhenAckFails — сбой подтверждения оставляет pending.
//
// Пометить «сделано» при ошибке значило бы дать Google молча вернуть деньги.
func TestWorkerKeepsPendingWhenAckFails(t *testing.T) {
	subs := newFakeMaintStore()
	subs.pending = []api.Subscription{googleSub("tok-1")}
	google := &fakeGoogleStatus{ackErr: errors.New("play down")}

	w := NewSubscriptionWorker(subs, nil, google, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if _, marked := subs.ackStates["tok-1"]; marked {
		t.Error("покупка помечена подтверждённой, хотя подтверждение упало")
	}
}

// TestWorkerResyncsExpiredSubscription — истёкшая подписка сверяется с
// магазином, и продление подхватывается без уведомления.
//
// Это страховка от потерянного вебхука: без неё продлившийся человек навсегда
// остался бы с истёкшей подпиской.
func TestWorkerResyncsExpiredSubscription(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeMaintStore()
	subs.expiring = []api.Subscription{{
		UserId: 1, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: now.Add(-time.Hour), AutoRenew: true, Environment: api.EnvProduction,
	}}
	renewed := now.Add(30 * 24 * time.Hour)
	apple := &fakeAppleStatus{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: renewed, AutoRenew: true, Environment: api.EnvProduction,
	}}

	w := NewSubscriptionWorker(subs, apple, nil, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if len(subs.upserts) != 1 {
		t.Fatalf("записей %d, хотели 1", len(subs.upserts))
	}
	if !subs.upserts[0].ExpiresAt.Equal(renewed) {
		t.Errorf("продление не подхвачено: %v", subs.upserts[0].ExpiresAt)
	}
}

// TestWorkerRevokesRefundedSubscription — возврат, узнанный при сверке, снимает
// Plus.
func TestWorkerRevokesRefundedSubscription(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeMaintStore()
	subs.expiring = []api.Subscription{{
		UserId: 1, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: now.Add(-time.Minute), Environment: api.EnvProduction,
	}}
	apple := &fakeAppleStatus{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: now.Add(24 * time.Hour), Revoked: true, Environment: api.EnvProduction,
	}}

	w := NewSubscriptionWorker(subs, apple, nil, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if len(subs.revoked) != 1 || subs.revoked[0] != "orig-1" {
		t.Errorf("возврат не отмечен: %v", subs.revoked)
	}
}

// TestWorkerKeepsStateWhenStoreUnavailable — недоступность магазина не меняет
// состояние подписки.
//
// Снять Plus у платящего из-за таймаута хуже, чем подождать следующего тика.
func TestWorkerKeepsStateWhenStoreUnavailable(t *testing.T) {
	subs := newFakeMaintStore()
	subs.expiring = []api.Subscription{{
		UserId: 1, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: time.Now().Add(-time.Minute), Environment: api.EnvProduction,
	}}
	apple := &fakeAppleStatus{err: errors.New("503 from apple")}

	w := NewSubscriptionWorker(subs, apple, nil, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if len(subs.upserts) != 0 {
		t.Errorf("состояние изменено при недоступном магазине: %+v", subs.upserts)
	}
	if len(subs.revoked) != 0 {
		t.Error("подписка отозвана из-за таймаута магазина")
	}
}

// TestWorkerTickIsIdempotent — повторный тик не плодит записей и не портит
// состояние.
func TestWorkerTickIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	subs := newFakeMaintStore()
	subs.expiring = []api.Subscription{{
		UserId: 1, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: now.Add(-time.Minute), Environment: api.EnvProduction,
	}}
	renewed := now.Add(30 * 24 * time.Hour)
	apple := &fakeAppleStatus{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: renewed, AutoRenew: true, Environment: api.EnvProduction,
	}}

	w := NewSubscriptionWorker(subs, apple, nil, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())
	w.Tick(context.Background())

	for _, up := range subs.upserts {
		if !up.ExpiresAt.Equal(renewed) {
			t.Errorf("состояние поехало между тиками: %v", up.ExpiresAt)
		}
	}
}

// TestWorkerSurvivesRepositoryErrors — сбой чтения не роняет тик: вторая
// половина работы всё равно выполняется.
func TestWorkerSurvivesRepositoryErrors(t *testing.T) {
	subs := newFakeMaintStore()
	subs.pendErr = errors.New("mongo down")
	subs.expiring = []api.Subscription{{
		UserId: 1, Store: api.StoreApple, StoreRef: "orig-1",
		ExpiresAt: time.Now().Add(-time.Minute), Environment: api.EnvProduction,
	}}
	apple := &fakeAppleStatus{receipt: store.Receipt{
		StoreRef: "orig-1", ProductId: "com.zagir.splitty.plus.monthly",
		ExpiresAt: time.Now().Add(24 * time.Hour), Environment: api.EnvProduction,
	}}
	google := &fakeGoogleStatus{}

	w := NewSubscriptionWorker(subs, apple, google, nil, SubscriptionWorkerConfig{})
	w.Tick(context.Background())

	if apple.calls != 1 {
		t.Error("сбой чтения подтверждений остановил сверку истекающих")
	}
}
