package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoFileRepository хранит картинки, загруженные из приложения (коллекция
// files). Отдельная коллекция, а не поле комнаты: см. api.StoredFile.
type MongoFileRepository struct {
	col *mongo.Collection
}

func NewFileRepository(db *mongo.Database) *MongoFileRepository {
	return &MongoFileRepository{col: db.Collection("files")}
}

// EnsureIndexes создаёт индексы коллекции files. Идемпотентно; вызывать при старте.
//   - по room_id: удаление комнаты сносит её файлы одним запросом, без индекса
//     это полный скан коллекции, которая растёт с каждой загруженной авой.
func (r MongoFileRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "room_id", Value: ascParameter}},
			Options: options.Index().SetName("idx_room"),
		},
	})
	return err
}

// Save кладёт файл и возвращает его id. Время ставит сервер: клиентским часам
// тут верить нечему.
func (r MongoFileRepository) Save(ctx context.Context, f *api.StoredFile) (string, error) {
	f.ID = primitive.NilObjectID
	f.Size = len(f.Data)
	f.CreatedAt = time.Now()

	res, err := r.col.InsertOne(ctx, f)
	if err != nil {
		log.Error().Err(err).Msg("insert file failed")
		return "", err
	}
	id, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		return "", mongo.ErrNilDocument
	}
	return id.Hex(), nil
}

// Get возвращает файл по id. Ненайденный файл — не ошибка (nil, nil): вызов
// отличает «нет такого» от «база сломалась».
func (r MongoFileRepository) Get(ctx context.Context, id string) (*api.StoredFile, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, nil
	}
	var f api.StoredFile
	if err := r.col.FindOne(ctx, bson.M{"_id": oid}).Decode(&f); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		log.Error().Err(err).Msg("find file failed")
		return nil, err
	}
	return &f, nil
}

// Delete удаляет файл по id. Отсутствие файла ошибкой не считается: удаление
// идёт следом за заменой авы и обязано быть повторяемым.
func (r MongoFileRepository) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil
	}
	if _, err := r.col.DeleteOne(ctx, bson.M{"_id": oid}); err != nil {
		log.Error().Err(err).Msg("delete file failed")
		return err
	}
	return nil
}

// DeleteByRoom сносит все файлы комнаты — зовётся при удалении самой комнаты,
// иначе байты остались бы в базе навсегда, никем не адресуемые.
func (r MongoFileRepository) DeleteByRoom(ctx context.Context, roomId string) error {
	oid, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return nil
	}
	if _, err := r.col.DeleteMany(ctx, bson.M{"room_id": oid}); err != nil {
		log.Error().Err(err).Msg("delete room files failed")
		return err
	}
	return nil
}
