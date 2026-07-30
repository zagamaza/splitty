package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// firstSyntheticUserID — первый номер, который аллокатор выдаёт пользователю,
// пришедшему НЕ из telegram (Google/Apple). 10^12 выбрано осознанно: реальные
// telegram id сейчас порядка 10^10, поэтому синтетические номера гарантированно
// не пересекаются с историческими _id (== telegram id), а до 2^53 (предел
// точного целого в JSON/JS-клиентах) остаётся три порядка запаса.
const firstSyntheticUserID = 1_000_000_000_000

// userIDSequenceKey — _id документа-счётчика в коллекции sequence
const userIDSequenceKey = "user_id"

// MongoSequenceRepository — аллокатор монотонно растущих номеров поверх
// коллекции sequence. Счётчик один на инсталляцию, инкремент атомарен на
// стороне mongo, поэтому несколько экземпляров репозитория (и несколько
// процессов приложения) могут выдавать номера одновременно.
type MongoSequenceRepository struct {
	col *mongo.Collection
}

func NewSequenceRepository(db *mongo.Database) *MongoSequenceRepository {
	return &MongoSequenceRepository{col: db.Collection("sequence")}
}

// NextUserID выдаёт следующий свободный номер пользователя Splitty.
//
// Реализация — рецепт (а) из плана: в документе хранится СМЕЩЕНИЕ (сколько
// номеров уже выдано), а не сам номер; вызывающему возвращается
// firstSyntheticUserID + смещение. Наивный вариант «$inc value + $setOnInsert
// value: firstSyntheticUserID» в mongo невозможен в принципе: $inc и
// $setOnInsert на одном поле дают конфликт («updating the path 'value' would
// create a conflict»). Вариант с предварительным InsertOne стартового
// документа работает, но добавляет гонку на старте и лишний запрос — здесь он
// не нужен, потому что $inc по отсутствующему полю сам создаёт его со
// значением инкремента.
//
// Первый вызов на пустой коллекции: upsert создаёт {_id:"user_id", value:1},
// и метод возвращает ровно firstSyntheticUserID (value-1 == 0 выданных до нас).
func (r MongoSequenceRepository) NextUserID(ctx context.Context) (int, error) {
	res := r.col.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: userIDSequenceKey}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "value", Value: int64(1)}}}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	)
	if res.Err() != nil {
		return 0, res.Err()
	}
	var doc struct {
		Value int64 `bson:"value"`
	}
	if err := res.Decode(&doc); err != nil {
		return 0, err
	}
	return firstSyntheticUserID + int(doc.Value) - 1, nil
}
