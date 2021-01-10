package repository

import (
	"context"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Mine struct {
	//ID           string `json:"_id" bson:"_id"`
	Name         string `json:"name" bson:"name"`
	DiamondCount int    `json:"diamond_count" bson:"diamond_count"`
}

type Transaction struct {
	ID    string `json:"id" bson:"id"`
	Donor User   `json:"donor" bson:"donor"`
	//цель покупки
	Sum        float32 `json:"sum" bson:"sum"`
	Recipients []User  `json:"recipients" bson:"recipients"`
}

type User struct {
	ID          int    `json:"id" bson:"_id"`
	Username    string `json:"userName" bson:"user_name"`
	DisplayName string `json:"displayName" bson:"display_name"`
}

type Room struct {
	ID           string `json:"id" bson:"_id"`
	Name         string `json:"name" bson:"name"`
	Users        []User `json:"users" bson:"users"`
	Transactions []Transaction
}

type UserRepository interface {
	UpdateMine(ctx context.Context, mineName string, newCount int) error
	FindByName(ctx context.Context, mineName string) (*Mine, error)
	GetAllMines(ctx context.Context) ([]Mine, error)
	AddDiamondMine(ctx context.Context, m *Mine) (*Mine, error)
	EmptyMine(ctx context.Context, mineName string) (diamondCount int, err error)
	UpsertUser(ctx context.Context, u *User) error
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*Room, error)
	JoinToRoom(ctx context.Context, u *User, roomId string) error
	SaveRoom(ctx context.Context, r *Room) (string, error)
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

func (r MongoRoomRepository) FindById(ctx context.Context, id string) (*Room, error) {
	res := r.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	rm := &Room{}
	if err := res.Decode(rm); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rr MongoRoomRepository) JoinToRoom(ctx context.Context, u *User, roomId string) error {
	filter := bson.D{{"_id", bson.D{{"$eq", roomId}}}}
	_, err := rr.col.UpdateOne(ctx, filter, bson.D{{"$pull", bson.D{{"users", u}}}})
	return err
}

func (rr MongoRoomRepository) SaveRoom(ctx context.Context, r *Room) (string, error) {
	res, err := rr.col.InsertOne(ctx, r)
	if res == nil || res.InsertedID == nil {
		return "", errors.New("insert failed")
	}
	return res.InsertedID.(primitive.ObjectID).String(), err
}

func (rr MongoRoomRepository) hasRoom(ctx context.Context, u *User) (bool, error) {
	resp, err := rr.col.CountDocuments(ctx, bson.D{{"_id", bson.D{{"$eq", u.ID}}}})
	return resp > 0, err
}

func (r MongoUserRepository) UpsertUser(ctx context.Context, u *User) error {
	opts := options.Update().SetUpsert(true)
	filter := r.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", u.ID}}}})
	update := bson.D{{"$set", bson.D{{"_id", u.ID}, {"user_name", u.Username}, {"display_name", u.DisplayName}}}}
	_, err := r.col.UpdateOne(ctx, filter, update, opts)

	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) FindByName(ctx context.Context, mineName string) (*Mine, error) {
	res := r.col.FindOne(ctx, bson.D{{"name", bson.D{{"$eq", mineName}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	m := &Mine{}
	if err := res.Decode(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (r MongoUserRepository) UpdateMine(ctx context.Context, mineName string, newCount int) error {
	f := bson.D{{"name", bson.D{{"$eq", mineName}}}}
	_, err := r.col.UpdateOne(ctx, f, bson.D{{"$set", bson.D{{"diamond_count", newCount}}}})
	return err
}

func (r MongoUserRepository) GetAllMines(ctx context.Context) ([]Mine, error) {
	cur, err := r.col.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var m []Mine
	err = cur.All(ctx, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r MongoUserRepository) AddDiamondMine(ctx context.Context, m *Mine) (*Mine, error) {
	a, err := r.col.InsertOne(ctx, m)
	id := a.InsertedID
	res := r.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	q := &Mine{}
	if err1 := res.Decode(q); err != nil {
		return nil, err1
	}
	return q, nil
}

func (r MongoUserRepository) EmptyMine(ctx context.Context, mineName string) (diamondCount int, err error) {
	f := bson.D{{"name", bson.D{{"$eq", mineName}}}}
	res := r.col.FindOneAndDelete(ctx, f)
	if res.Err() != nil {
		return 0, res.Err()
	}
	m := &Mine{}
	err = res.Decode(m)
	if err != nil {
		return 0, err
	}
	return m.DiamondCount, nil
}
