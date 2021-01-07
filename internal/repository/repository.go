package repository

import (
	"context"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type Mine struct {
	//ID           string `json:"_id" bson:"_id"`
	Name         string `json:"name" bson:"name"`
	DiamondCount int    `json:"diamond_count" bson:"diamond_count"`
}

type MineRepository interface {
	UpdateMine(ctx context.Context, mineName string, newCount int) error
	FindByName(ctx context.Context, mineName string) (*Mine, error)
	GetAllMines(ctx context.Context) ([]Mine, error)
	AddDiamondMine(ctx context.Context, m *Mine) (*Mine, error)
	EmptyMine(ctx context.Context, mineName string) (diamondCount int, err error)
}

type MongoMineRepository struct {
	col *mongo.Collection
}

func New(col *mongo.Database) *MongoMineRepository {
	return &MongoMineRepository{col: col.Collection("mine")}
}

func (r MongoMineRepository) FindByName(ctx context.Context, mineName string) (*Mine, error) {
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

func (r MongoMineRepository) UpdateMine(ctx context.Context, mineName string, newCount int) error {
	f := bson.D{{"name", bson.D{{"$eq", mineName}}}}
	_, err := r.col.UpdateOne(ctx, f, bson.D{{"$set", bson.D{{"diamond_count", newCount}}}})
	return err
}

func (r MongoMineRepository) GetAllMines(ctx context.Context) ([]Mine, error) {
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

func (r MongoMineRepository) AddDiamondMine(ctx context.Context, m *Mine) (*Mine, error) {
	a, err := r.col.InsertOne(ctx, m)
	id := a.InsertedID
	res := r.col.FindOne(ctx, bson.D{{"_id", bson.D{{"$eq", id}}}})
	q := &Mine{}
	if err1 := res.Decode(q); err != nil {
		return nil, err1
	}
	return q, nil
}

func (r MongoMineRepository) EmptyMine(ctx context.Context, mineName string) (diamondCount int, err error) {
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
