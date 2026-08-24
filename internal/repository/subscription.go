package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoSubscriptionRepository — подписки Splitor Plus (коллекция subscriptions).
//
// Отдельная коллекция, а не поле в документе пользователя: состояние меняют
// уведомления сторов и фоновый воркер, а они приходят с ключом стора, а не с
// номером человека — искать нужно по store_ref. Заодно документ пользователя не
// переписывается на каждое продление.
type MongoSubscriptionRepository struct {
	col *mongo.Collection
}

func NewSubscriptionRepository(db *mongo.Database) *MongoSubscriptionRepository {
	return &MongoSubscriptionRepository{col: db.Collection("subscriptions")}
}

// EnsureIndexes создаёт индексы подписок. Идемпотентно; вызывать при старте.
//
//   - uniq (store, store_ref) — одна запись на покупку. Именно этот индекс не
//     даёт повторной доставке уведомления или гонке двух запросов завести
//     второй документ на ту же подписку.
//   - user_id — резолв тарифа на каждом распознавании.
//   - expires_at — выборка истекающих для фоновой досинхронизации.
//   - ack_state частичный — воркер ищет только pending, а их единицы; полный
//     индекс по полю, где почти везде "done", читался бы почти как скан.
//   - linked_ref — поиск предшественника при смене плана в Google.
func (r *MongoSubscriptionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "store", Value: ascParameter}, {Key: "store_ref", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetName("uniq_store_ref"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: ascParameter}},
			Options: options.Index().SetName("idx_user"),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: ascParameter}},
			Options: options.Index().SetName("idx_expires"),
		},
		{
			Keys: bson.D{{Key: "ack_state", Value: ascParameter}},
			Options: options.Index().
				SetName("idx_pending_ack").
				SetPartialFilterExpression(bson.M{"ack_state": api.AckPending}),
		},
		{
			Keys:    bson.D{{Key: "linked_ref", Value: ascParameter}},
			Options: options.Index().SetSparse(true).SetName("idx_linked_ref"),
		},
	})
	return err
}

// ErrStaleNotification — уведомление старше уже применённого к этой подписке.
//
// Не ошибка обработки: доставка у обоих сторов приходит не по порядку, и
// задержавшийся EXPIRED после DID_RENEW — обычное дело. Вызывающий обязан
// отличить это от сбоя и ответить стору 200, иначе тот будет ретраить вечно.
var ErrStaleNotification = errors.New("уведомление старше уже применённого")

// Upsert записывает состояние подписки, найденной по (store, store_ref).
//
// Откат назад запрещён: если у документа уже применено уведомление НЕ СТАРШЕ
// приходящего, запись отклоняется с ErrStaleNotification. Без этой проверки
// переупорядоченная доставка гасила бы действующую подписку — человек платит, а
// Plus снимается.
//
// Нулевой LastNotifiedAt означает «источник не уведомление, а прямая проверка
// чека» (покупка, фоновая досинхронизация): такая запись применяется всегда,
// потому что она и есть актуальное состояние из стора.
func (r *MongoSubscriptionRepository) Upsert(ctx context.Context, s api.Subscription) error {
	now := time.Now().UTC()
	s.UpdatedAt = now
	if s.CheckedAt.IsZero() {
		s.CheckedAt = now
	}

	filter := bson.M{"store": s.Store, "store_ref": s.StoreRef}
	if !s.LastNotifiedAt.IsZero() {
		filter["$or"] = []bson.M{
			{"last_notified_at": bson.M{"$exists": false}},
			{"last_notified_at": bson.M{"$lte": s.LastNotifiedAt}},
		}
	}

	set := bson.M{
		"user_id":     s.UserId,
		"product_id":  s.ProductId,
		"expires_at":  s.ExpiresAt,
		"auto_renew":  s.AutoRenew,
		"environment": s.Environment,
		"ack_state":   s.AckState,
		"checked_at":  s.CheckedAt,
		"updated_at":  s.UpdatedAt,
	}
	if s.LinkedRef != "" {
		set["linked_ref"] = s.LinkedRef
	}
	if s.BindingToken != "" {
		set["binding_token"] = s.BindingToken
	}
	if !s.LastNotifiedAt.IsZero() {
		set["last_notified_at"] = s.LastNotifiedAt
	}
	// revoked_at и superseded_at здесь НЕ трогаются: их ставят MarkRevoked и
	// Supersede. Возврат денег не должен отменяться очередным «подписка
	// активна» от продления, пришедшим следом.

	update := bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"store": s.Store, "store_ref": s.StoreRef},
	}

	res, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		if IsDuplicateKey(err) {
			// Фильтр не совпал из-за отсечки last_notified_at, и upsert попытался
			// вставить дубль по uniq_store_ref — значит документ есть и он новее.
			return ErrStaleNotification
		}
		return err
	}
	if res.MatchedCount == 0 && res.UpsertedCount == 0 {
		return ErrStaleNotification
	}
	return nil
}

// ActiveByUser возвращает подписки пользователя, не отозванные и не заменённые.
// Истёкшие тоже возвращаются: решение «активна ли» принимает вызывающий, у него
// есть запас на задержку доставки (см. api.Subscription.Active).
func (r *MongoSubscriptionRepository) ActiveByUser(ctx context.Context, userId int) ([]api.Subscription, error) {
	filter := bson.M{
		"user_id":       userId,
		"revoked_at":    bson.M{"$exists": false},
		"superseded_at": bson.M{"$exists": false},
	}
	return r.find(ctx, filter, options.Find().SetSort(bson.D{{Key: "expires_at", Value: descParameter}}))
}

// ByStoreRef ищет подписку по ключу стора. Не найдена — mongo.ErrNoDocuments.
func (r *MongoSubscriptionRepository) ByStoreRef(ctx context.Context, store, ref string) (*api.Subscription, error) {
	var s api.Subscription
	if err := r.col.FindOne(ctx, bson.M{"store": store, "store_ref": ref}).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Supersede помечает подписку заменённой новым ключом стора.
//
// Нужен из-за ротации purchaseToken в Google: смена месяц↔год выдаёт новый
// токен и кладёт старый в linkedPurchaseToken. Без пометки старый документ
// остаётся «активным» и продолжает давать Plus даже после возврата денег по
// новому. Идемпотентен: повторный вызов ничего не меняет.
func (r *MongoSubscriptionRepository) Supersede(ctx context.Context, store, ref string, at time.Time) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"store": store, "store_ref": ref, "superseded_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"superseded_at": at.UTC(), "updated_at": time.Now().UTC()}},
	)
	return err
}

// MarkRevoked отзывает подписку (возврат денег, чарджбек): Plus снимается
// немедленно, не дожидаясь expires_at. Идемпотентен.
func (r *MongoSubscriptionRepository) MarkRevoked(ctx context.Context, store, ref string, at time.Time) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"store": store, "store_ref": ref, "revoked_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revoked_at": at.UTC(), "updated_at": time.Now().UTC()}},
	)
	return err
}

// SetAckState отмечает результат подтверждения покупки в Google.
func (r *MongoSubscriptionRepository) SetAckState(ctx context.Context, store, ref, state string) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"store": store, "store_ref": ref},
		bson.M{"$set": bson.M{"ack_state": state, "updated_at": time.Now().UTC()}},
	)
	return err
}

// PendingAcks возвращает покупки, ожидающие подтверждения.
//
// Google откатывает неподтверждённую покупку через трое суток и возвращает
// деньги, поэтому подтверждение обязано переживать падение сервера сразу после
// валидации — отсюда состояние в документе и фоновый ретрай.
func (r *MongoSubscriptionRepository) PendingAcks(ctx context.Context, limit int64) ([]api.Subscription, error) {
	return r.find(ctx,
		bson.M{"ack_state": api.AckPending, "revoked_at": bson.M{"$exists": false}},
		options.Find().SetLimit(limit),
	)
}

// ExpiringBefore возвращает подписки, у которых истекает срок, для фоновой
// сверки со стором. Отозванные и заменённые не берутся — по ним решение принято.
func (r *MongoSubscriptionRepository) ExpiringBefore(ctx context.Context, deadline time.Time, limit int64) ([]api.Subscription, error) {
	return r.find(ctx,
		bson.M{
			"expires_at":    bson.M{"$lte": deadline.UTC()},
			"revoked_at":    bson.M{"$exists": false},
			"superseded_at": bson.M{"$exists": false},
		},
		options.Find().SetSort(bson.D{{Key: "expires_at", Value: ascParameter}}).SetLimit(limit),
	)
}

// DeleteByUser удаляет подписки пользователя. Зовётся при удалении аккаунта:
// в документах лежат идентификаторы покупок, привязанные к человеку, и без
// чистки повторная регистрация упиралась бы в чужую (свою прошлую) привязку.
func (r *MongoSubscriptionRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"user_id": userId})
	return err
}

func (r *MongoSubscriptionRepository) find(ctx context.Context, filter interface{}, opts *options.FindOptions) ([]api.Subscription, error) {
	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	subs := make([]api.Subscription, 0)
	if err := cur.All(ctx, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}
