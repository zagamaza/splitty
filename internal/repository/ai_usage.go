package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoAiUsageRepository счётчик обращений к AI-парсингу для rate-limit.
// Документ на ключ окна (userId+минута / userId+сутки) с TTL-полем expires_at,
// по которому mongo сам удаляет протухшие окна.
type MongoAiUsageRepository struct {
	col *mongo.Collection
}

func NewAiUsageRepository(db *mongo.Database) *MongoAiUsageRepository {
	return &MongoAiUsageRepository{col: db.Collection("ai_usage")}
}

// EnsureIndexes создаёт TTL-индекс на expires_at (expireAfterSeconds=0 — mongo
// удаляет документ, когда expires_at наступил). Идемпотентно; вызывать при старте.
func (r *MongoAiUsageRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires_at"),
	})
	return err
}

// Incr атомарно увеличивает счётчик окна key и возвращает новое значение.
// expires_at выставляется только при вставке ($setOnInsert), чтобы TTL считался
// от первого запроса в окне.
func (r *MongoAiUsageRepository) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	update := bson.M{
		"$inc":         bson.M{"count": 1},
		"$setOnInsert": bson.M{"expires_at": time.Now().Add(ttl)},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc struct {
		Count int64 `bson:"count"`
	}
	if err := r.col.FindOneAndUpdate(ctx, bson.M{"_id": key}, update, opts).Decode(&doc); err != nil {
		return 0, err
	}
	return doc.Count, nil
}

// Get читает счётчик окна, не изменяя его: остаток показывается в интерфейсе и
// отдаётся в GET /me/ai-quota, и просмотр остатка не должен его расходовать.
//
// Отсутствующее окно — это ноль, а не ошибка: до первого распознавания за
// сутки документа просто нет, и это нормальное состояние, а не сбой.
func (r *MongoAiUsageRepository) Get(ctx context.Context, key string) (int64, error) {
	var doc struct {
		Count int64 `bson:"count"`
	}
	if err := r.col.FindOne(ctx, bson.M{"_id": key}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return 0, nil
		}
		return 0, err
	}
	return doc.Count, nil
}
