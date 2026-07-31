package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/push"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoPushOutboxRepository — персистентная очередь пушей (коллекция push_outbox),
// реализует push.Store. Доставку из очереди делает push.Worker с ретраями.
type MongoPushOutboxRepository struct {
	col *mongo.Collection
}

func NewPushOutboxRepository(db *mongo.Database) *MongoPushOutboxRepository {
	return &MongoPushOutboxRepository{col: db.Collection("push_outbox")}
}

// pushOutboxDoc — запись очереди. next_attempt_at гейтит доставку (сразу = now),
// attempts растёт при транзиентных сбоях (бэк-офф в push.Worker).
type pushOutboxDoc struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	UserID        int                `bson:"user_id"`
	Title         string             `bson:"title"`
	Body          string             `bson:"body"`
	Data          map[string]string  `bson:"data,omitempty"`
	Attempts      int                `bson:"attempts"`
	NextAttemptAt time.Time          `bson:"next_attempt_at"`
	CreatedAt     time.Time          `bson:"created_at"`
}

func (r *MongoPushOutboxRepository) Enqueue(ctx context.Context, userID int, n push.Notification) error {
	now := time.Now()
	_, err := r.col.InsertOne(ctx, pushOutboxDoc{
		UserID:        userID,
		Title:         n.Title,
		Body:          n.Body,
		Data:          n.Data,
		NextAttemptAt: now,
		CreatedAt:     now,
	})
	return err
}

func (r *MongoPushOutboxRepository) Due(ctx context.Context, now time.Time, limit int) ([]push.PendingPush, error) {
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "next_attempt_at", Value: 1}})
	cur, err := r.col.Find(ctx, bson.M{"next_attempt_at": bson.M{"$lte": now}}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []pushOutboxDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]push.PendingPush, 0, len(docs))
	for _, d := range docs {
		out = append(out, push.PendingPush{
			ID:     d.ID.Hex(),
			UserID: d.UserID,
			Notification: push.Notification{
				Title: d.Title,
				Body:  d.Body,
				Data:  d.Data,
			},
			Attempts: d.Attempts,
		})
	}
	return out, nil
}

func (r *MongoPushOutboxRepository) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.col.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// DeleteByUserId выбрасывает из очереди все неотправленные пуши пользователя.
//
// Нужен удалению аккаунта, и это не «технический мусор»: в очереди лежат уже
// ОТРЕНДЕРЕННЫЕ title/body, а тексты уведомлений содержат имя автора и описание
// расхода (см. bot.Notifier). Без чистки человеку после удаления аккаунта
// доставился бы пуш со старым именем — уже затёртым во всех комнатах.
func (r *MongoPushOutboxRepository) DeleteByUserId(ctx context.Context, userID int) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func (r *MongoPushOutboxRepository) Reschedule(ctx context.Context, id string, nextAt time.Time, attempts int) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.col.UpdateOne(ctx, bson.M{"_id": oid},
		bson.M{"$set": bson.M{"next_attempt_at": nextAt, "attempts": attempts}})
	return err
}
