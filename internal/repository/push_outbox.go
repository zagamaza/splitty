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
	// SentAt заполняется, когда доставка закончилась (успехом или отказом):
	// запись перестаёт быть очередью и становится следом, который через
	// retention уносит TTL. Пока поля нет — пуш ждёт отправки.
	SentAt *time.Time `bson:"sent_at,omitempty"`
	// Outcome/Tokens — что ответил FCM. Ради них всё и затевалось: раньше
	// успешная отправка и «ушло в никуда» выглядели одинаково — пустой очередью.
	Outcome string             `bson:"outcome,omitempty"`
	Tokens  []tokenOutcomeDoc  `bson:"tokens,omitempty"`
}

type tokenOutcomeDoc struct {
	Token string `bson:"token"`
	Error string `bson:"error,omitempty"`
}

// sentRetention — сколько держим след доставки. Неделя: этого хватает разобрать
// жалобу «мне не пришло», и коллекция не растёт.
const sentRetention = 7 * 24 * time.Hour

// EnsureIndexes создаёт индексы очереди. Идемпотентно; вызывать при старте.
//   - по next_attempt_at: воркер каждые 5 секунд фильтрует и сортирует по нему.
//     Без индекса это скан и сортировка всей очереди, а суточная рассылка
//     напоминаний кладёт в неё пачку записей разом.
//
// Имя idx_next_attempt — не опечатка и не выдумка: ровно такой индекс уже
// заведён на проде руками. Любое другое имя даёт IndexOptionsConflict и warn в
// логе на каждом старте, а предупреждения, которые всегда горят, перестают
// читать.
func (r *MongoPushOutboxRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "next_attempt_at", Value: ascParameter}},
			Options: options.Index().SetName("idx_next_attempt"),
		},
		{
			// TTL по sent_at — вместо отдельной джобы чистки: mongod сам
			// обходит коллекцию раз в минуту. Записи БЕЗ sent_at (то есть
			// ждущие отправки) TTL не трогает — он удаляет только документы,
			// где поле есть и это дата.
			Keys:    bson.D{{Key: "sent_at", Value: ascParameter}},
			Options: options.Index().SetName("idx_sent_ttl").SetExpireAfterSeconds(int32(sentRetention.Seconds())),
		},
	})
	return err
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
	// sent_at: nil обязателен — иначе воркер брал бы уже доставленные записи
	// и слал их по кругу, пока их не унесёт TTL. В Mongo $eq: nil совпадает и
	// с отсутствующим полем, поэтому старые записи очереди тоже подходят.
	cur, err := r.col.Find(ctx, bson.M{
		"next_attempt_at": bson.M{"$lte": now},
		"sent_at":         bson.M{"$eq": nil},
	}, opts)
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

// MarkSent закрывает запись следом доставки: запись остаётся в коллекции и
// уходит по TTL (idx_sent_ttl), а не удаляется сразу.
func (r *MongoPushOutboxRepository) MarkSent(ctx context.Context, id string, result push.DeliveryResult) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	tokens := make([]tokenOutcomeDoc, 0, len(result.Tokens))
	for _, t := range result.Tokens {
		tokens = append(tokens, tokenOutcomeDoc{Token: t.Token, Error: t.Error})
	}
	_, err = r.col.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"sent_at": time.Now(),
		"outcome": result.Outcome,
		"tokens":  tokens,
	}})
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
