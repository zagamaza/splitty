package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/mongo"
)

func testSub(ref string, expires time.Time) api.Subscription {
	return api.Subscription{
		UserId:      700,
		Store:       api.StoreGoogle,
		ProductId:   "com.zagir.splitty.plus.monthly",
		StoreRef:    ref,
		ExpiresAt:   expires.UTC(),
		AutoRenew:   true,
		Environment: api.EnvProduction,
		AckState:    api.AckDone,
	}
}

func mustUpsert(t *testing.T, repo *MongoSubscriptionRepository, s api.Subscription) {
	t.Helper()
	if err := repo.Upsert(testCtx(t), s); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

// TestSubscriptionUpsertInsertsThenUpdates — первая запись вставляет, вторая
// обновляет ту же (uniq по store+store_ref не даёт завести дубль).
func TestSubscriptionUpsertInsertsThenUpdates(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	mustUpsert(t, repo, testSub("tok-1", now.Add(24*time.Hour)))

	renewed := testSub("tok-1", now.Add(48*time.Hour))
	mustUpsert(t, repo, renewed)

	got, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-1")
	if err != nil {
		t.Fatalf("ByStoreRef: %v", err)
	}
	if !got.ExpiresAt.Equal(renewed.ExpiresAt) {
		t.Errorf("продление не записалось: хотели %v, получили %v", renewed.ExpiresAt, got.ExpiresAt)
	}

	subs, err := repo.ActiveByUser(ctx, 700)
	if err != nil {
		t.Fatalf("ActiveByUser: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("завелось %d документов вместо одного", len(subs))
	}
}

// TestSubscriptionUpsertRejectsStaleNotification — уведомление старше уже
// применённого не откатывает состояние.
//
// Главная защита от переупорядоченной доставки: задержавшийся EXPIRED,
// прилетевший после DID_RENEW, не должен гасить действующую подписку — иначе
// человек платит, а Plus снимается.
func TestSubscriptionUpsertRejectsStaleNotification(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	renew := testSub("tok-2", now.Add(30*24*time.Hour))
	renew.LastNotifiedAt = now
	mustUpsert(t, repo, renew)

	// Опоздавший EXPIRED: подписан РАНЬШЕ продления, срок в прошлом.
	expired := testSub("tok-2", now.Add(-time.Hour))
	expired.LastNotifiedAt = now.Add(-10 * time.Minute)
	expired.AutoRenew = false

	if err := repo.Upsert(ctx, expired); err != ErrStaleNotification {
		t.Fatalf("хотели ErrStaleNotification, получили %v", err)
	}

	got, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-2")
	if err != nil {
		t.Fatalf("ByStoreRef: %v", err)
	}
	if !got.ExpiresAt.Equal(renew.ExpiresAt) {
		t.Errorf("опоздавшее уведомление откатило срок: %v вместо %v", got.ExpiresAt, renew.ExpiresAt)
	}
	if !got.AutoRenew {
		t.Error("опоздавшее уведомление сбросило автопродление")
	}
}

// TestSubscriptionUpsertAppliesNewerNotification — уведомление новее
// применяется (обратная сторона предыдущего теста: отсечка не должна
// заблокировать нормальный поток).
func TestSubscriptionUpsertAppliesNewerNotification(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	first := testSub("tok-3", now.Add(24*time.Hour))
	first.LastNotifiedAt = now.Add(-time.Hour)
	mustUpsert(t, repo, first)

	second := testSub("tok-3", now.Add(60*24*time.Hour))
	second.LastNotifiedAt = now
	mustUpsert(t, repo, second)

	got, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-3")
	if err != nil {
		t.Fatalf("ByStoreRef: %v", err)
	}
	if !got.ExpiresAt.Equal(second.ExpiresAt) {
		t.Errorf("свежее уведомление не применилось: %v вместо %v", got.ExpiresAt, second.ExpiresAt)
	}
}

// TestSubscriptionUpsertFromDirectCheckAlwaysApplies — запись без отметки
// уведомления (прямая проверка чека у стора) применяется всегда, даже если
// раньше приходили уведомления.
func TestSubscriptionUpsertFromDirectCheckAlwaysApplies(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	notified := testSub("tok-4", now.Add(24*time.Hour))
	notified.LastNotifiedAt = now
	mustUpsert(t, repo, notified)

	direct := testSub("tok-4", now.Add(90*24*time.Hour)) // LastNotifiedAt нулевой
	mustUpsert(t, repo, direct)

	got, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-4")
	if err != nil {
		t.Fatalf("ByStoreRef: %v", err)
	}
	if !got.ExpiresAt.Equal(direct.ExpiresAt) {
		t.Errorf("прямая проверка не применилась: %v вместо %v", got.ExpiresAt, direct.ExpiresAt)
	}
}

// TestSubscriptionRevokeAndSupersedeHideFromActive — отозванная и заменённая
// подписки перестают попадать в выборку активных.
func TestSubscriptionRevokeAndSupersedeHideFromActive(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	mustUpsert(t, repo, testSub("tok-revoked", now.Add(24*time.Hour)))
	mustUpsert(t, repo, testSub("tok-superseded", now.Add(24*time.Hour)))
	mustUpsert(t, repo, testSub("tok-live", now.Add(24*time.Hour)))

	if err := repo.MarkRevoked(ctx, api.StoreGoogle, "tok-revoked", now); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	if err := repo.Supersede(ctx, api.StoreGoogle, "tok-superseded", now); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	subs, err := repo.ActiveByUser(ctx, 700)
	if err != nil {
		t.Fatalf("ActiveByUser: %v", err)
	}
	if len(subs) != 1 || subs[0].StoreRef != "tok-live" {
		t.Errorf("в активных оказалось %d записей: %+v", len(subs), subs)
	}
}

// TestSubscriptionRenewalDoesNotResurrectRevoked — продление, пришедшее после
// возврата денег, не воскрешает отозванную подписку.
//
// Порядок доставки не гарантирован, и «подписка активна до такого-то» вполне
// может прийти следом за возвратом.
func TestSubscriptionRenewalDoesNotResurrectRevoked(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	mustUpsert(t, repo, testSub("tok-5", now.Add(24*time.Hour)))
	if err := repo.MarkRevoked(ctx, api.StoreGoogle, "tok-5", now); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}

	mustUpsert(t, repo, testSub("tok-5", now.Add(365*24*time.Hour)))

	got, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-5")
	if err != nil {
		t.Fatalf("ByStoreRef: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("продление сняло отметку возврата — Plus вернулся после возврата денег")
	}
}

// TestSubscriptionPendingAcksAndExpiring — выборки для фонового воркера.
func TestSubscriptionPendingAcksAndExpiring(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	pending := testSub("tok-pending", now.Add(24*time.Hour))
	pending.AckState = api.AckPending
	mustUpsert(t, repo, pending)
	mustUpsert(t, repo, testSub("tok-done", now.Add(24*time.Hour)))

	soon := testSub("tok-soon", now.Add(-time.Minute))
	mustUpsert(t, repo, soon)

	acks, err := repo.PendingAcks(ctx, 10)
	if err != nil {
		t.Fatalf("PendingAcks: %v", err)
	}
	if len(acks) != 1 || acks[0].StoreRef != "tok-pending" {
		t.Errorf("PendingAcks вернул %d записей: %+v", len(acks), acks)
	}

	expiring, err := repo.ExpiringBefore(ctx, now, 10)
	if err != nil {
		t.Fatalf("ExpiringBefore: %v", err)
	}
	if len(expiring) != 1 || expiring[0].StoreRef != "tok-soon" {
		t.Errorf("ExpiringBefore вернул %d записей: %+v", len(expiring), expiring)
	}

	if err := repo.SetAckState(ctx, api.StoreGoogle, "tok-pending", api.AckDone); err != nil {
		t.Fatalf("SetAckState: %v", err)
	}
	acks, err = repo.PendingAcks(ctx, 10)
	if err != nil {
		t.Fatalf("PendingAcks после подтверждения: %v", err)
	}
	if len(acks) != 0 {
		t.Errorf("подтверждённая покупка осталась в очереди: %+v", acks)
	}
}

// TestSubscriptionDeleteByUserId — чистка при удалении аккаунта.
func TestSubscriptionDeleteByUserId(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewSubscriptionRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now().UTC()
	mustUpsert(t, repo, testSub("tok-a", now.Add(24*time.Hour)))
	other := testSub("tok-b", now.Add(24*time.Hour))
	other.UserId = 701
	mustUpsert(t, repo, other)

	if err := repo.DeleteByUserId(ctx, 700); err != nil {
		t.Fatalf("DeleteByUserId: %v", err)
	}

	if _, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-a"); err != mongo.ErrNoDocuments {
		t.Errorf("подписка удалённого аккаунта осталась: %v", err)
	}
	if _, err := repo.ByStoreRef(ctx, api.StoreGoogle, "tok-b"); err != nil {
		t.Errorf("задета чужая подписка: %v", err)
	}
}
