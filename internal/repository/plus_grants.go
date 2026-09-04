package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoPlusGrantRepository — Plus, выданный из панели (коллекция plus_grant).
//
// Отдельная коллекция, а не поле пользователя: строки append-only, отзыв не
// удаляет, а помечает, и «выдали заново после отзыва» остаётся отличимым от
// «продлили».
type MongoPlusGrantRepository struct {
	col *mongo.Collection
}

func NewPlusGrantRepository(db *mongo.Database) *MongoPlusGrantRepository {
	return &MongoPlusGrantRepository{col: db.Collection("plus_grant")}
}

// EnsureIndexes создаёт индексы грантов. Идемпотентно; вызывать при старте.
//
// Индекс один: (user_id, revoked_at) — горячий путь резолва тарифа. По
// expires_at индекса НЕТ намеренно: коллекция на десятки строк, и он не даёт
// ничего, кроме записи на каждой выдаче.
func (r *MongoPlusGrantRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: ascParameter}, {Key: "revoked_at", Value: ascParameter}},
			Options: options.Index().SetName("idx_user_revoked"),
		},
	})
	return err
}

// LiveByUser — самый поздний срок среди живых грантов человека, или nil.
//
// Имя не ActiveByUser намеренно: у подписок так называется метод, который
// истёкшие как раз ВОЗВРАЩАЕТ (см. MongoSubscriptionRepository.ActiveByUser), и
// одно имя с двумя разными контрактами рано или поздно даст фильтр в обоих
// местах или ни в одном.
//
// Максимум, а не первая попавшаяся строка: уникальность живого гранта не
// гарантируется (см. Grant), и две строки должны читаться как одна.
func (r *MongoPlusGrantRepository) LiveByUser(ctx context.Context, userId int, now time.Time) (*api.PlusGrant, error) {
	var g api.PlusGrant
	err := r.col.FindOne(ctx,
		liveFilter(userId, now),
		options.FindOne().SetSort(bson.D{{Key: "expires_at", Value: descParameter}}),
	).Decode(&g)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Grant выдаёт Plus до expiresAt: продлевает живой грант человека, а если
// живого нет — заводит новую строку.
//
// ⚠️ Уникальность живого гранта НЕ гарантируется и не может быть гарантирована
// фильтрованным upsert'ом: mongo обещает единственность только через уникальный
// индекс, а частичный уникальный индекс по user_id запретил бы выдачу заново
// после истечения — она здесь нужна. Два одновременных запроса на одного
// человека вставят две строки. Цена этого не в дубле, а в отзыве, поэтому
// Revoke ходит по ВСЕМ живым строкам, а LiveByUser берёт максимум.
func (r *MongoPlusGrantRepository) Grant(ctx context.Context, userId int, expiresAt time.Time, reason string, now time.Time) error {
	set := bson.M{
		"expires_at": expiresAt.UTC(),
		"updated_at": now.UTC(),
	}
	// Причина затирается только новой непустой: продление без причины не должно
	// стирать ту, ради которой грант и выдали.
	if reason != "" {
		set["reason"] = reason
	}

	_, err := r.col.UpdateOne(ctx,
		liveFilter(userId, now),
		bson.M{
			"$set": set,
			"$setOnInsert": bson.M{
				"user_id":    userId,
				"source":     api.GrantSourcePanel,
				"created_at": now.UTC(),
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

// Revoke снимает Plus со ВСЕХ живых грантов человека.
//
// UpdateMany, а не UpdateOne: живых строк может оказаться две (см. Grant), и
// вторая молча продолжила бы раздавать Plus после «отозвано» — ровно тот
// молчаливый провал, который отзыв и должен исключать.
func (r *MongoPlusGrantRepository) Revoke(ctx context.Context, userId int, reason string, at time.Time) error {
	set := bson.M{"revoked_at": at.UTC(), "updated_at": at.UTC()}
	if reason != "" {
		set["revoked_reason"] = reason
	}
	_, err := r.col.UpdateMany(ctx,
		bson.M{"user_id": userId, "revoked_at": bson.M{"$exists": false}},
		bson.M{"$set": set},
	)
	return err
}

// ListLive — кому Plus выдан прямо сейчас, поздний срок первым.
func (r *MongoPlusGrantRepository) ListLive(ctx context.Context, now time.Time) ([]api.PlusGrant, error) {
	cur, err := r.col.Find(ctx,
		bson.M{
			"revoked_at": bson.M{"$exists": false},
			"expires_at": bson.M{"$gt": now.UTC()},
		},
		options.Find().SetSort(bson.D{{Key: "expires_at", Value: descParameter}}),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	grants := make([]api.PlusGrant, 0)
	if err := cur.All(ctx, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

// DeleteByUserId — реализация userDataCleaner: гранты уходят вместе с аккаунтом.
func (r *MongoPlusGrantRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"user_id": userId})
	return err
}

// liveFilter — «живой грант этого человека»: не отозван и ещё не истёк.
// Один на резолв и на продление: разойдись они — продление обновляло бы не ту
// строку, которую отдаёт резолв.
func liveFilter(userId int, now time.Time) bson.M {
	return bson.M{
		"user_id":    userId,
		"revoked_at": bson.M{"$exists": false},
		"expires_at": bson.M{"$gt": now.UTC()},
	}
}
