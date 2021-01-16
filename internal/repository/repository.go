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

type Transaction struct {
	ID    primitive.ObjectID `json:"id" bson:"id"`
	Donor *api.User          `json:"donor" bson:"donor"`
	//цель покупки
	Sum        float32     `json:"sum" bson:"sum"`
	Recipients *[]api.User `json:"recipients" bson:"recipients"`
}

type User struct {
	ID          int    `json:"id" bson:"_id"`
	Username    string `json:"userName" bson:"user_name"`
	DisplayName string `json:"displayName" bson:"display_name"`
}

type Room struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Name         string             `json:"name" bson:"name"`
	Users        *[]api.User        `json:"users" bson:"users"`
	Transactions *[]Transaction
}

type UserRepository interface {
	UpsertUser(ctx context.Context, u api.User) error
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*Room, error)
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	SaveRoom(ctx context.Context, r *Room) (primitive.ObjectID, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]Room, error)
}

type MongoUserRepository struct {
	col *mongo.Collection
}

type MongoRoomRepository struct {
	col *mongo.Collection
}

func NewUserRepository(col *mongo.Database) *MongoUserRepository {
	return &MongoUserRepository{col: col.Collection("user")}
}

func NewRoomRepository(col *mongo.Database) *MongoRoomRepository {
	return &MongoRoomRepository{col: col.Collection("room")}
}

func (rr MongoRoomRepository) FindById(ctx context.Context, id string) (*Room, error) {
	hex, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	res := rr.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", hex}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	rm := &Room{}
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

func (rr MongoRoomRepository) SaveRoom(ctx context.Context, r *Room) (primitive.ObjectID, error) {
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

func (rr MongoRoomRepository) FindRoomsByUserId(ctx context.Context, id int) (*[]Room, error) {
	cur, err := rr.col.Find(ctx, bson.D{{"users._id", bson.D{{"$eq", id}}}})
	if err != nil {
		return nil, err
	}
	var m []Room
	err = cur.All(ctx, &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
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
