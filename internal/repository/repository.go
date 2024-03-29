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

const descParameter = -1
const ascParameter = 1

type UserRepository interface {
	UpsertUser(ctx context.Context, u api.User) (*api.User, error)
	SetUserLang(ctx context.Context, userId int, lang string) error
	SetNotificationUser(ctx context.Context, userId int, notification bool) error
	SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error
	SetCountInPage(ctx context.Context, userId int, count int) error
	FindById(ctx context.Context, id int) (*api.User, error)
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*api.Room, error)
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	LeaveRoom(ctx context.Context, userId int, roomId string) error
	SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindArchivedRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
	UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	ArchiveRoom(ctx context.Context, userId int, roomId string) error
	UnArchiveRoom(ctx context.Context, userId int, roomId string) error
	FinishedAddOperation(ctx context.Context, userId int, roomId string) error
	UnFinishedAddOperation(ctx context.Context, userId int, roomId string) error
	PaidOfDebts(ctx context.Context, userIds []int, roomId string) error
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

func (rr MongoRoomRepository) LeaveRoom(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{"_id", bson.D{{"$eq", hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"users": bson.M{"_id": userId}}})
	if err != nil {
		return err
	}
	return nil
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

func (rr MongoRoomRepository) ArchiveRoom(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex, "users._id": userId}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$addToSet": bson.M{"room_states.archived": userId}})
	return err
}

func (rr MongoRoomRepository) UnArchiveRoom(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex, "users._id": userId}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"room_states.archived": userId}})
	log.Error().Err(err).Msg("")
	return err
}

func (rr MongoRoomRepository) FinishedAddOperation(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex, "users._id": userId}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$addToSet": bson.M{"room_states.finished_add_operation": userId}})
	return err
}

func (rr MongoRoomRepository) UnFinishedAddOperation(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex, "users._id": userId}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"room_states.finished_add_operation": userId}})
	log.Error().Err(err).Msg("")
	return err
}

func (rr MongoRoomRepository) PaidOfDebts(ctx context.Context, userIds []int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex}
	update := bson.D{{"$set", bson.M{"room_states.paid_off_debts": userIds}}}
	_, err = rr.col.UpdateOne(ctx, filter, update)
	log.Error().Err(err).Msg("")
	return err
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

func (rr MongoRoomRepository) FindRoomsByUserId(ctx context.Context, userId int) (*[]api.Room, error) {
	cur, err := rr.col.Find(ctx, bson.M{
		"users._id":            bson.M{"$eq": userId},
		"room_states.archived": bson.M{"$ne": userId},
	}, getOrderOptions("create_at", descParameter))
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

func (rr MongoRoomRepository) FindArchivedRoomsByUserId(ctx context.Context, userId int) (*[]api.Room, error) {
	cur, err := rr.col.Find(ctx, bson.M{
		"users._id":            bson.M{"$eq": userId},
		"room_states.archived": bson.M{"$eq": userId},
	}, getOrderOptions("create_at", descParameter))
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
		"users":                bson.M{"$elemMatch": bson.M{"_id": userId}},
		"name":                 bson.M{"$regex": ".*" + name + ".*"},
		"room_states.archived": bson.M{"$ne": userId},
	}, getOrderOptions("create_at", descParameter))
	if err != nil {
		return nil, err
	}
	var m []api.Room
	if err = cur.All(ctx, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func getOrderOptions(field string, orderParameter int) *options.FindOptions {
	findOptions := options.Find()
	findOptions.SetSort(bson.D{{field, orderParameter}})
	return findOptions
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
	if cs.CountInPage == 0 {
		cs.CountInPage = 5
	}
	if cs.NotificationOn == nil {
		cs.NotificationOn = func() *bool { b := true; return &b }()
	}
	return cs, nil
}

func (r MongoUserRepository) UpsertUser(ctx context.Context, u api.User) (*api.User, error) {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", u.ID}}}}
	update := bson.D{{"$set", bson.M{"_id": u.ID, "user_lang": u.UserLang, "display_name": u.DisplayName, "user_name": u.Username}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return nil, err
	}
	return r.FindById(ctx, u.ID)
}

func (r MongoUserRepository) SetUserLang(ctx context.Context, userId int, lang string) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", userId}}}}
	update := bson.D{{"$set", bson.M{"selected_lang": lang}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetCountInPage(ctx context.Context, userId int, count int) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", userId}}}}
	update := bson.D{{"$set", bson.M{"count_in_page": count}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetNotificationUser(ctx context.Context, userId int, notification bool) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", userId}}}}
	update := bson.D{{"$set", bson.M{"notification_on": notification}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{"_id", bson.D{{"$eq", userId}}}}
	update := bson.D{{"$set", bson.M{"bank_details": bankDerails}}}
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
