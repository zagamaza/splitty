package repository

import (
	"context"
	"time"

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
	// CreateIdentityUser вставляет нового пользователя целиком (InsertOne, а не
	// upsert): только так записываются поля личности, которых частичный $set в
	// UpsertUser не касается. Занятый _id или занятая личность — duplicate key
	// (см. IsDuplicateKey), на этом строится retry входа через Google/Apple
	CreateIdentityUser(ctx context.Context, u api.User) error
	SetUserLang(ctx context.Context, userId int, lang string) error
	SetNotificationUser(ctx context.Context, userId int, notification bool) error
	SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error
	SetCountInPage(ctx context.Context, userId int, count int) error
	FindById(ctx context.Context, id int) (*api.User, error)
	FindByIds(ctx context.Context, ids []int) ([]api.User, error)
	FindByUsername(ctx context.Context, username string) (*api.User, error)
	// FindByTelegramID, FindByGoogleSub, FindByAppleSub ищут пользователя по
	// личности. Удалённые (tombstone с deleted_at) не находятся — иначе после
	// удаления аккаунта повторная регистрация с той же личностью упиралась бы в
	// собственный труп. mongo.ErrNoDocuments — не найден
	FindByTelegramID(ctx context.Context, tgID int) (*api.User, error)
	FindByGoogleSub(ctx context.Context, sub string) (*api.User, error)
	FindByAppleSub(ctx context.Context, sub string) (*api.User, error)
	SetNotifySettings(ctx context.Context, userId int, s api.NotifySettings) error
	AddAlias(ctx context.Context, userId int, alias string) error
	AddPushToken(ctx context.Context, userId int, token api.PushToken) error
	RemovePushToken(ctx context.Context, userId int, token string) error
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*api.Room, error)
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	LeaveRoom(ctx context.Context, userId int, roomId string) error
	SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindArchivedRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
	UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error
	SetNotificationSent(ctx context.Context, roomId string, operationId primitive.ObjectID, sent []int) error
	CreateOperation(ctx context.Context, o *api.Operation, roomId string) error
	// CreateOperationIfAbsent атомарно добавляет операцию, если в комнате ещё нет
	// операции с тем же client_op_id (o.ClientOpId обязан быть непустым).
	// Возвращает true, если операция вставлена; false — если дубль уже существует;
	// mongo.ErrNoDocuments — если комнаты нет
	CreateOperationIfAbsent(ctx context.Context, o *api.Operation, roomId string) (bool, error)
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	ArchiveRoom(ctx context.Context, userId int, roomId string) error
	UnArchiveRoom(ctx context.Context, userId int, roomId string) error
	FinishedAddOperation(ctx context.Context, userId int, roomId string) error
	UnFinishedAddOperation(ctx context.Context, userId int, roomId string) error
	PaidOfDebts(ctx context.Context, userIds []int, roomId string) error
	UpdateCurrency(ctx context.Context, roomId string, currency string) error
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

type LoginCodeRepository interface {
	SaveLoginCode(ctx context.Context, c *api.LoginCode) error
	// UseLoginCode атомарно помечает код использованным; если код не найден,
	// просрочен или уже использован — mongo.ErrNoDocuments
	UseLoginCode(ctx context.Context, code string, now time.Time) (*api.LoginCode, error)
}

// MongoUserRepository владеет собственным аллокатором номеров (seq), а не
// получает его снаружи. Причина: UpsertTelegramUser — метод UserRepository, и
// вызывается он из графа БОТА (internal/events/telegram.go), который про
// rest.Server ничего не знает. Аллокатор, проброшенный только в rest.Server,
// был бы там недоступен, а менять сигнатуру NewUserRepository пришлось бы в
// обоих графах, включая сгенерированный wire_gen.go
type MongoUserRepository struct {
	col *mongo.Collection
	seq *MongoSequenceRepository
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

type MongoLoginCodeRepository struct {
	col *mongo.Collection
}

type BugReportRepository interface {
	SaveBugReport(ctx context.Context, r *api.BugReport) error
}

type MongoBugReportRepository struct {
	col *mongo.Collection
}

func NewBugReportRepository(col *mongo.Database) *MongoBugReportRepository {
	return &MongoBugReportRepository{col: col.Collection("bug_report")}
}

func (br MongoBugReportRepository) SaveBugReport(ctx context.Context, r *api.BugReport) error {
	res, err := br.col.InsertOne(ctx, r)
	if err != nil {
		log.Error().Err(err).Msg("insert bug report failed")
		return err
	}
	if res != nil && res.InsertedID == nil {
		return errors.New("insert bug report failed")
	}
	return nil
}

// NewUserRepository. Сигнатура намеренно не меняется: она вызывается из двух
// графов (cmd/splitty/main.go и сгенерированный wire_gen.go), и добавление
// параметра потребовало бы перегенерации wire. База уже передаётся — коллекцию
// sequence поднимаем прямо здесь
func NewUserRepository(col *mongo.Database) *MongoUserRepository {
	return &MongoUserRepository{col: col.Collection("user"), seq: NewSequenceRepository(col)}
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

func NewLoginCodeRepository(col *mongo.Database) *MongoLoginCodeRepository {
	return &MongoLoginCodeRepository{col: col.Collection("login_code")}
}

// EnsureIndexes создаёт индексы коллекции login_code. Идемпотентно; вызывать при старте.
//   - уникальный по code: UseLoginCode искал код полным сканом коллекции, которая
//     росла безвозвратно (каждый /login вставляет документ, никто не удаляет), —
//     логин деградировал линейно от числа входов за всё время жизни инсталляции;
//   - TTL по expires_at: протухшие коды удаляет сам mongo (expireAfterSeconds=0).
func (lr MongoLoginCodeRepository) EnsureIndexes(ctx context.Context) error {
	_, err := lr.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "code", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetName("uniq_code"),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: ascParameter}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires_at"),
		},
	})
	return err
}

func (lr MongoLoginCodeRepository) SaveLoginCode(ctx context.Context, c *api.LoginCode) error {
	res, err := lr.col.InsertOne(ctx, c)
	if err != nil {
		log.Error().Err(err).Msg("insert login code failed")
		return err
	}
	if res != nil && res.InsertedID == nil {
		return errors.New("insert login code failed")
	}
	return nil
}

// UseLoginCode одним FindOneAndUpdate находит живой код ({code, used:false,
// expires_at > now}) и помечает его использованным — атомарность исключает
// повторный вход по одному коду при конкурентных запросах.
// Не найден/просрочен/использован — mongo.ErrNoDocuments
func (lr MongoLoginCodeRepository) UseLoginCode(ctx context.Context, code string, now time.Time) (*api.LoginCode, error) {
	res := lr.col.FindOneAndUpdate(ctx,
		bson.M{"code": code, "used": false, "expires_at": bson.M{"$gt": now}},
		bson.M{"$set": bson.M{"used": true}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	lc := &api.LoginCode{}
	if err := res.Decode(lc); err != nil {
		return nil, err
	}
	return lc, nil
}

func (rr MongoRoomRepository) FindById(ctx context.Context, id string) (*api.Room, error) {
	hex, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	res := rr.col.FindOne(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}})
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

	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.D{{Key: "$push", Value: bson.D{{Key: "users", Value: u.Snapshot()}}}})
	return err
}

// Санитайз встроенных снимков.
//
// Документы room хранят пользователей целиком: users[], operations[].donor,
// operations[].recipients[], operations[].recipients_with_sum[].user. Без
// очистки туда навсегда осели бы telegram_id/google_sub/apple_sub/email и
// push-токены — их там никто не обновляет и не удаляет, а при удалении аккаунта
// они пережили бы сам аккаунт. Санитайз стоит на границе репозитория, а не у
// вызывающих: так его нельзя забыть в новом месте записи.
//
// Все три функции возвращают КОПИЮ и не трогают аргумент: вызывающий код
// продолжает работать с полным объектом (например, шлёт уведомления по нему).

// sanitizeUsers возвращает копию среза с обнулёнными полями личности
func sanitizeUsers(users *[]api.User) *[]api.User {
	if users == nil {
		return nil
	}
	out := make([]api.User, len(*users))
	for i, u := range *users {
		out[i] = u.Snapshot()
	}
	return &out
}

// sanitizeOperation возвращает копию операции с санитайзнутыми снимками
// пользователей; суммы, доли и id не трогаются
func sanitizeOperation(o *api.Operation) *api.Operation {
	if o == nil {
		return nil
	}
	c := *o
	if c.Donor != nil {
		d := c.Donor.Snapshot()
		c.Donor = &d
	}
	c.Recipients = sanitizeUsers(c.Recipients)
	if c.RecipientsWithSum != nil {
		rws := make([]api.RecipientWithSum, len(c.RecipientsWithSum))
		copy(rws, c.RecipientsWithSum)
		for i := range rws {
			rws[i].User = rws[i].User.Snapshot()
		}
		c.RecipientsWithSum = rws
	}
	return &c
}

// sanitizeRoom возвращает копию комнаты с санитайзнутыми участниками и
// операциями
func sanitizeRoom(r *api.Room) *api.Room {
	if r == nil {
		return nil
	}
	c := *r
	c.Members = sanitizeUsers(c.Members)
	if c.Operations != nil {
		ops := make([]api.Operation, len(*c.Operations))
		for i := range *c.Operations {
			ops[i] = *sanitizeOperation(&(*c.Operations)[i])
		}
		c.Operations = &ops
	}
	return &c
}

func (rr MongoRoomRepository) LeaveRoom(ctx context.Context, userId int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"users": bson.M{"_id": userId}}})
	if err != nil {
		return err
	}
	return nil
}

func (rr MongoRoomRepository) SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error) {
	res, err := rr.col.InsertOne(ctx, sanitizeRoom(r))
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
	if err != nil {
		log.Error().Err(err).Msg("get all debts failed")
		return err
	}
	return nil
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
	if err != nil {
		log.Error().Err(err).Msg("UpdateOne failed")
		return err
	}
	return nil
}

func (rr MongoRoomRepository) PaidOfDebts(ctx context.Context, userIds []int, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex}
	update := bson.D{{Key: "$set", Value: bson.M{"room_states.paid_off_debts": userIds}}}
	_, err = rr.col.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Error().Err(err).Msg("PaidOfDebts")
		return err
	}
	return nil
}

func (rr MongoRoomRepository) hasRoom(ctx context.Context, u *api.User) (bool, error) {
	resp, err := rr.col.CountDocuments(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: u.ID}}}})
	return resp > 0, err
}

func (rr MongoRoomRepository) hasUserInRoom(ctx context.Context, uId int, roomId primitive.ObjectID) (bool, error) {
	resp, err := rr.col.CountDocuments(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: roomId}}},
		{Key: "users._id", Value: bson.D{{Key: "$eq", Value: uId}}}})
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
	findOptions.SetSort(bson.D{{Key: field, Value: orderParameter}})
	return findOptions
}

// UpdateOperation атомарно заменяет операцию комнаты одним UpdateOne с positional $.
// Раньше здесь были два неатомарных вызова ($pull, затем $push): падение между ними
// теряло операцию, а конкурентные обновления дублировали её или «воскрешали» удалённую.
// MatchedCount == 0 (нет комнаты или операции) — mongo.ErrNoDocuments
// SetNotificationSent пишет ТОЛЬКО notification_sent, а не всю операцию.
// Нотификатор работает в отдельной горутине и держит копию операции с момента
// запроса: полная замена через UpdateOperation откатывала правку, если
// пользователь успевал отредактировать расход до отправки уведомлений.
func (rr MongoRoomRepository) SetNotificationSent(ctx context.Context, roomId string, operationId primitive.ObjectID, sent []int) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.M{"_id": hex, "operations._id": operationId}
	res, err := rr.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"operations.$.notification_sent": sent}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (rr MongoRoomRepository) UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.M{"_id": hex, "operations._id": o.ID}
	res, err := rr.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"operations.$": sanitizeOperation(o)}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// CreateOperation добавляет новую операцию одним $push (операции всегда создаются
// с новым ObjectID, прежний $pull был no-op). MatchedCount == 0 (комнаты нет) —
// mongo.ErrNoDocuments вместо прежнего молчаливого успеха
func (rr MongoRoomRepository) CreateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	res, err := rr.col.UpdateOne(ctx, bson.M{"_id": hex}, bson.D{{Key: "$push", Value: bson.D{{Key: "operations", Value: sanitizeOperation(o)}}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// CreateOperationIfAbsent идемпотентная вставка операции по клиентскому ключу
// client_op_id: один UpdateOne с фильтром {_id, "operations.client_op_id": {$ne: id}}
// — $push выполняется, только если ни одна операция комнаты не несёт этот ключ,
// проверка и вставка атомарны (конкурентные повторы не создают дубль).
// MatchedCount == 0 означает «комнаты нет» ЛИБО «дубль уже есть» — различаем
// дополнительным CountDocuments по _id: комнаты нет — mongo.ErrNoDocuments,
// комната есть — (false, nil), существующую операцию вычитывает вызывающий
func (rr MongoRoomRepository) CreateOperationIfAbsent(ctx context.Context, o *api.Operation, roomId string) (bool, error) {
	if o.ClientOpId == "" {
		return false, errors.New("client_op_id must not be empty")
	}
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return false, err
	}
	filter := bson.M{"_id": hex, "operations.client_op_id": bson.M{"$ne": o.ClientOpId}}
	res, err := rr.col.UpdateOne(ctx, filter, bson.D{{Key: "$push", Value: bson.D{{Key: "operations", Value: sanitizeOperation(o)}}}})
	if err != nil {
		return false, err
	}
	if res.MatchedCount > 0 {
		return true, nil
	}
	n, err := rr.col.CountDocuments(ctx, bson.M{"_id": hex})
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, mongo.ErrNoDocuments
	}
	return false, nil
}

func (rr MongoRoomRepository) DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"operations": bson.M{"_id": operationId}}})
	if err != nil {
		return err
	}
	return nil
}

func (rr MongoRoomRepository) UpdateCurrency(ctx context.Context, roomId string, currency string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"currency": currency}}}
	_, err = rr.col.UpdateOne(ctx, filter, update)
	return err
}

func (r MongoUserRepository) FindById(ctx context.Context, id int) (*api.User, error) {
	return r.findOne(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: id}}}})
}

// notDeleted — фильтр «живой аккаунт»: у tombstone-документов (см. удаление
// аккаунта) поле deleted_at выставлено, и по личности они находиться не должны
var notDeleted = bson.D{{Key: "deleted_at", Value: bson.D{{Key: "$exists", Value: false}}}}

// FindByTelegramID ищет живого пользователя по telegram-личности. Именно этот
// метод (а не FindById) — точка входа telegram: _id равен telegram id только у
// исторических аккаунтов, у пришедших через Google/Apple он синтетический
func (r MongoUserRepository) FindByTelegramID(ctx context.Context, tgID int) (*api.User, error) {
	return r.findOne(ctx, append(bson.D{{Key: "telegram_id", Value: bson.D{{Key: "$eq", Value: tgID}}}}, notDeleted...))
}

// FindByGoogleSub ищет живого пользователя по sub из id-токена Google
func (r MongoUserRepository) FindByGoogleSub(ctx context.Context, sub string) (*api.User, error) {
	return r.findOne(ctx, append(bson.D{{Key: "google_sub", Value: bson.D{{Key: "$eq", Value: sub}}}}, notDeleted...))
}

// FindByAppleSub ищет живого пользователя по sub из id-токена Apple
func (r MongoUserRepository) FindByAppleSub(ctx context.Context, sub string) (*api.User, error) {
	return r.findOne(ctx, append(bson.D{{Key: "apple_sub", Value: bson.D{{Key: "$eq", Value: sub}}}}, notDeleted...))
}

// findOne — общий путь чтения пользователя: декодирование плюс дефолты, которые
// исторически проставлял FindById (count_in_page и notification_on отсутствуют у
// старых документов). Поиск по личности обязан отдавать точно такой же объект,
// как поиск по _id, иначе поведение зависело бы от способа входа
func (r MongoUserRepository) findOne(ctx context.Context, filter interface{}) (*api.User, error) {
	res := r.col.FindOne(ctx, filter)
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

// CreateIdentityUser вставляет пользователя целиком. Именно InsertOne, а не
// upsert: UpsertUser пишет частичный $set из четырёх полей и записать через него
// google_sub/apple_sub/telegram_id/email невозможно. Вставка также даёт честный
// duplicate key на занятом _id или занятой личности — вызывающий (вход через
// Google/Apple/Telegram) на нём строит повторный поиск, разрешающий гонку двух
// первых входов одного человека
func (r MongoUserRepository) CreateIdentityUser(ctx context.Context, u api.User) error {
	_, err := r.col.InsertOne(ctx, u)
	return err
}

// NextUserID выдаёт номер для пользователя, у которого нет пригодного _id
// (вход через Google/Apple, а также telegram-вход, чей telegram id уже занят
// другим аккаунтом). Делегирует в общий счётчик коллекции sequence
func (r MongoUserRepository) NextUserID(ctx context.Context) (int, error) {
	return r.seq.NextUserID(ctx)
}

// IsDuplicateKey — ошибка уникального индекса (E11000). В драйвере 1.4.4 нет
// mongo.IsDuplicateKeyError (появился в 1.5), поэтому код разбираем сами
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	isDupCode := func(code int) bool {
		return code == 11000 || code == 11001 || code == 12582
	}
	var we mongo.WriteException
	if errors.As(err, &we) {
		for _, e := range we.WriteErrors {
			if isDupCode(e.Code) {
				return true
			}
		}
	}
	var bwe mongo.BulkWriteException
	if errors.As(err, &bwe) {
		for _, e := range bwe.WriteErrors {
			if isDupCode(e.Code) {
				return true
			}
		}
	}
	var ce mongo.CommandError
	if errors.As(err, &ce) {
		return isDupCode(int(ce.Code))
	}
	return false
}

// EnsureIndexes создаёт unique sparse индексы по полям личности. Идемпотентно;
// вызывать при старте, до бэкфилла telegram_id.
//
// sparse обязателен: без него unique-индекс считает отсутствующее поле значением
// null и второй же пользователь без google_sub упал бы на duplicate key.
// unique обязателен: без него гонка двух первых входов одного человека создаёт
// два аккаунта с одной личностью, и повторный вход становится лотереей
func (r MongoUserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "telegram_id", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("uniq_telegram_id"),
		},
		{
			Keys:    bson.D{{Key: "google_sub", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("uniq_google_sub"),
		},
		{
			Keys:    bson.D{{Key: "apple_sub", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("uniq_apple_sub"),
		},
	})
	return err
}

// FindByUsername ищет пользователя по telegram-username (для нотификаций
// суперюзерам из /report); mongo.ErrNoDocuments — пользователь не писал боту
func (r MongoUserRepository) FindByUsername(ctx context.Context, username string) (*api.User, error) {
	res := r.col.FindOne(ctx, bson.D{{Key: "user_name", Value: bson.D{{Key: "$eq", Value: username}}}})
	if res.Err() != nil {
		return nil, res.Err()
	}
	cs := &api.User{}
	if err := res.Decode(cs); err != nil {
		return nil, err
	}
	return cs, nil
}

// FindByIds возвращает пользователей по списку id одним запросом ($in). Порядок
// результата не гарантируется; отсутствующие id просто не попадают в выборку.
// Нужен для AI-парсинга: канонические профили участников (с алиасами) берутся
// из коллекции user, а не из embedded-снимков в комнате.
func (r MongoUserRepository) FindByIds(ctx context.Context, ids []int) ([]api.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cur, err := r.col.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var users []api.User
	if err := cur.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// AddAlias добавляет прозвище пользователю ($addToSet — без дублей). Алиас
// нормализуется вызывающим кодом (trim/lower).
func (r MongoUserRepository) AddAlias(ctx context.Context, userId int, alias string) error {
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{{Key: "$addToSet", Value: bson.M{"aliases": alias}}}
	res, err := r.col.UpdateOne(ctx, f, update)
	if err != nil {
		return err
	}
	// целевого пользователя нет — не молчаливый no-op, а явная ошибка (404 в хендлере)
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// AddPushToken регистрирует FCM-токен устройства (идемпотентно): сначала убираем
// прежнюю запись с тем же token (мог сменить платформу/пользователя), затем
// добавляем. Дубли токенов недопустимы — один токен = одно устройство.
func (r MongoUserRepository) AddPushToken(ctx context.Context, userId int, token api.PushToken) error {
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	if _, err := r.col.UpdateOne(ctx, f, bson.M{"$pull": bson.M{"push_tokens": bson.M{"token": token.Token}}}); err != nil {
		return err
	}
	res, err := r.col.UpdateOne(ctx, f, bson.M{"$push": bson.M{"push_tokens": token}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RemovePushToken убирает токен (logout или отбраковка FCM). Отсутствие токена —
// не ошибка (idempotent): 404 только если самого пользователя нет.
func (r MongoUserRepository) RemovePushToken(ctx context.Context, userId int, token string) error {
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	res, err := r.col.UpdateOne(ctx, f, bson.M{"$pull": bson.M{"push_tokens": bson.M{"token": token}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (r MongoUserRepository) UpsertUser(ctx context.Context, u api.User) (*api.User, error) {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: u.ID}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"_id": u.ID, "user_lang": u.UserLang, "display_name": u.DisplayName, "user_name": u.Username}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return nil, err
	}
	return r.FindById(ctx, u.ID)
}

func (r MongoUserRepository) SetUserLang(ctx context.Context, userId int, lang string) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"selected_lang": lang}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetCountInPage(ctx context.Context, userId int, count int) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"count_in_page": count}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetNotifySettings(ctx context.Context, userId int, s api.NotifySettings) error {
	filter := bson.D{{Key: "_id", Value: userId}}
	update := bson.D{{Key: "$set", Value: bson.M{"notify": s}}}
	_, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Error().Err(err).Msg("set notify settings failed")
	}
	return err
}

func (r MongoUserRepository) SetNotificationUser(ctx context.Context, userId int, notification bool) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"notification_on": notification}}}
	_, err := r.col.UpdateOne(ctx, f, update, opts)
	if err != nil {
		return err
	}
	return nil
}

func (r MongoUserRepository) SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error {
	opts := options.Update().SetUpsert(true)
	f := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{{Key: "$set", Value: bson.M{"bank_details": bankDerails}}}
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
	res := csr.col.FindOne(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: id}}}})
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
	res := csr.col.FindOne(ctx, bson.D{{Key: "user_id", Value: bson.D{{Key: "$eq", Value: userId}}}})
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
	_, err := csr.col.DeleteOne(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: id}}}})
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
