package repository

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UserRepository interface {
	UpsertUser(ctx context.Context, u api.User) error
	FindById(ctx context.Context, id int) (*api.User, error)
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*api.Room, error)
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
	UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
}

type ChatStateRepository interface {
	Save(ctx context.Context, u *api.ChatState) error
	FindById(ctx context.Context, id int) (*api.ChatState, error)
	FindByUserId(ctx context.Context, userId int) (*api.ChatState, error)
	DeleteById(ctx context.Context, id primitive.ObjectID) error
	DeleteByUserId(ctx context.Context, id int) error
}

type ButtonRepository interface {
	Save(ctx context.Context, b *api.Button) (primitive.ObjectID, error)
	SaveAll(ctx context.Context, b ...*api.Button) ([]*api.Button, error)
	FindById(ctx context.Context, id string) (*api.Button, error)
}

type MongoUserRepository struct {
	col *mongo.Collection
}

type MongoRoomRepository struct {
	col *mongo.Collection
}

type MongoChatStateRepository struct {
	col *mongo.Collection
}
type MongoButtonRepository struct {
	col *mongo.Collection
}

func NewUserRepository(col *mongo.Database) *MongoUserRepository {
	return &MongoUserRepository{col: col.Collection("user")}
}

func NewRoomRepository(col *mongo.Database) *MongoRoomRepository {
	return &MongoRoomRepository{col: col.Collection("room")}
}

func NewChatStateRepository(col *mongo.Database) *MongoChatStateRepository {
	return &MongoChatStateRepository{col: col.Collection("chat_state")}
}

func NewButtonRepository(col *mongo.Database) *MongoButtonRepository {
	return &MongoButtonRepository{col: col.Collection("button")}
}

func (rr MongoRoomRepository) FindById(ctx context.Context, id string) (*api.Room, error) {
	hex, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	res := rr.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", hex}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	rm := &api.Room{}
	if err := res.Decode(rm); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rr MongoRoomRepository) JoinToRoom(ctx context.Context, u api.User, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	hasUserInRoom, err := rr.hasUserInRoom(ctx, u.ID, hex)
	if err != nil || hasUserInRoom {
		return err
	}

	filter := bson.D{{"_id", bson.D{{"$eq", hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.D{{"$push", bson.D{{"users", u}}}})
	return err
}

func (rr MongoRoomRepository) SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error) {
	res, err := rr.col.InsertOne(ctx, r)
	if err != nil {
		log.Error().Err(err).Msg("insert failed")
	}
	if res != nil && res.InsertedID == nil {
		return primitive.NewObjectID(), errors.New("insert failed")
	}
	return res.InsertedID.(primitive.ObjectID), err
}

func (rr MongoRoomRepository) hasRoom(ctx context.Context, u *api.User) (bool, error) {
	resp, err := rr.col.CountDocuments(ctx, bson.D{{"_id", bson.D{{"$eq", u.ID}}}})
	return resp > 0, err
}

func (rr MongoRoomRepository) hasUserInRoom(ctx context.Context, uId int, roomId primitive.ObjectID) (bool, error) {
	resp, err := rr.col.CountDocuments(ctx, bson.D{{"_id", bson.D{{"$eq", roomId}}},
		{"users._id", bson.D{{"$eq", uId}}}})
	return resp > 0, err
}

func (rr MongoRoomRepository) FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error) {
	cur, err := rr.col.Find(ctx, bson.D{{"users._id", bson.D{{"$eq", id}}}})
	if err != nil {
		return nil, err
	}
	var m []api.Room
	err = cur.All(ctx, &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (rr MongoRoomRepository) FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error) {
	cur, err := rr.col.Find(ctx, bson.M{
		"users": bson.M{"$elemMatch": bson.M{"_id": userId}},
		"name":  bson.M{"$regex": ".*" + name + ".*"},
	})
	if err != nil {
		return nil, err
	}
	var m []api.Room
	if err = cur.All(ctx, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (rr MongoRoomRepository) UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{"_id", bson.D{{"$eq", hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"operations": bson.M{"_id": o.ID}}})
	if err != nil {
		return err
	}

	_, err = rr.col.UpdateOne(ctx, filter, bson.D{{"$push", bson.D{{"operations", o}}}})
	return err
}

func (rr MongoRoomRepository) DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{"_id", bson.D{{"$eq", hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"operations": bson.M{"_id": operationId}}})
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) FindById(ctx context.Context, id int) (*api.User, error) {
	res := r.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	cs := &api.User{}
	if err := res.Decode(cs); err != nil {
		return nil, err
	}
	return cs, nil
}

func (r MongoUserRepository) UpsertUser(ctx context.Context, u api.User) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", u.ID}}}}
	update := bson.D{{"$set", u}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)

	if err != nil {
		return err
	}
	return nil
}

func (csr MongoChatStateRepository) Save(ctx context.Context, cs *api.ChatState) error {
	res, err := csr.col.InsertOne(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("insert failed")
	}
	if res != nil && res.InsertedID == nil {
		return errors.New("insert failed")
	}
	return err
}

func (csr MongoChatStateRepository) FindById(ctx context.Context, id int) (*api.ChatState, error) {
	res := csr.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	if res.Err() == mongo.ErrNoDocuments {
		log.Warn().Err(res.Err()).Msgf("chat_state not found by id %v", id)
		return nil, nil
	}
	if res.Err() != nil {
		return nil, res.Err()
	}
	cs := &api.ChatState{}
	if err := res.Decode(cs); err != nil {
		return nil, err
	}
	return cs, nil
}

func (csr MongoChatStateRepository) FindByUserId(ctx context.Context, userId int) (*api.ChatState, error) {
	res := csr.col.FindOne(ctx, bson.D{{"user_id", bson.D{{"$eq", userId}}}})
	if res.Err() == mongo.ErrNoDocuments {
		log.Debug().Err(res.Err()).Msgf("chat_state not found by user_id %v", userId)
		return nil, nil
	}
	if res.Err() != nil {
		return nil, res.Err()
	}
	cs := &api.ChatState{}
	if err := res.Decode(cs); err != nil {
		return nil, err
	}
	return cs, nil
}

func (csr MongoChatStateRepository) DeleteById(ctx context.Context, id primitive.ObjectID) error {
	_, err := csr.col.DeleteOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	if err != nil {
		log.Error().Err(err).Msg("delete failed")
		return err
	}
	return nil
}

func (csr MongoChatStateRepository) DeleteByUserId(ctx context.Context, id int) error {
	if _, err := csr.col.DeleteMany(ctx, bson.M{"user_id": id}); err != nil {
		log.Error().Err(err).Msg("delete failed")
		return err
	}
	return nil
}

func (br MongoButtonRepository) Save(ctx context.Context, b *api.Button) (primitive.ObjectID, error) {
	res, err := br.col.InsertOne(ctx, b)
	if err != nil || res == nil || res.InsertedID == nil {
		log.Error().Err(err).Stack().Msg("insert failed")
		return primitive.NilObjectID, err
	}
	return res.InsertedID.(primitive.ObjectID), nil
}

func (br MongoButtonRepository) SaveAll(ctx context.Context, b ...*api.Button) ([]*api.Button, error) {
	i := make([]interface{}, len(b))
	for idx, btn := range b {
		i[idx] = btn
	}
	res, err := br.col.InsertMany(ctx, i)
	if err != nil || res == nil || res.InsertedIDs == nil {
		log.Error().Err(err).Stack().Msg("insert failed")
		return b, err
	}
	for idx, id := range res.InsertedIDs {
		b[idx].ID = id.(primitive.ObjectID)
	}

	return b, nil
}

func (br MongoButtonRepository) FindById(ctx context.Context, id string) (*api.Button, error) {
	hex, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	res := br.col.FindOne(ctx, bson.M{"_id": hex})
	if res.Err() != nil {
		return nil, res.Err()
	}
	btn := &api.Button{}
	if err = res.Decode(btn); err != nil {
		return nil, err
	}
	return btn, nil
}
