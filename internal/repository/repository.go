package repository

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/google/uuid"
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
	// UpsertTelegramUser резолвит telegram-личность: ищет живого пользователя по
	// telegram_id и заводит нового, если такого нет. Возвращает КАНОНИЧЕСКИЙ
	// документ, чей _id — номер Splitty, а не telegram id (совпадают они только
	// у исторических аккаунтов). userLang проставляется при создании и когда в
	// базе пусто; заполненный не затирается
	UpsertTelegramUser(ctx context.Context, tgID int, username, displayName, userLang string) (*api.User, error)
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
	// FindByLoginEmail ищет по адресу входа с паролем (login_email, не email).
	// Адрес нормализуется внутри — вызывающему нормализовать не нужно
	FindByLoginEmail(ctx context.Context, email string) (*api.User, error)
	// SetPasswordHash пишет bcrypt-хеш пароля живому пользователю. Пара
	// login_email+password_hash пишется целиком при регистрации вставкой
	// (CreateIdentityUser), поэтому отдельного писателя пары нет
	SetPasswordHash(ctx context.Context, userId int, hash string) error
	// UpdateAppleProfile дописывает то, что приходит из потока Sign in with
	// Apple: email и имя (Apple отдаёт их ТОЛЬКО при первом входе) и refresh
	// token для последующего отзыва при удалении аккаунта. Пустые значения
	// игнорируются — затирать сохранённое нечем и незачем
	UpdateAppleProfile(ctx context.Context, userId int, email, displayName, refreshToken string) error
	// SetIdentity/ClearIdentity привязывают и отвязывают способ входа (provider —
	// одна из констант Identity*). Оба работают только по ЖИВОМУ документу и
	// никогда его не создают: почему именно так — в комментарии к
	// MongoUserRepository.SetIdentity. Пользователя нет или он удалён —
	// mongo.ErrNoDocuments; личность занята другим — duplicate key
	SetIdentity(ctx context.Context, userId int, provider string, value interface{}) error
	ClearIdentity(ctx context.Context, userId int, provider string) error
	// SoftDeleteUser помечает аккаунт удалённым (tombstone) и вычищает из него
	// PII и личности. Документ НЕ удаляется: пять методов ниже работают через
	// upsert по _id и воскресили бы пользователя пустым документом от первого же
	// запроса со старым токеном. Идемпотентен — повторный вызов дописывает то же
	// самое. Пользователя нет — mongo.ErrNoDocuments
	SoftDeleteUser(ctx context.Context, userId int) error
	SetNotifySettings(ctx context.Context, userId int, s api.NotifySettings) error
	SetNotificationsSeenAt(ctx context.Context, userId int, at time.Time) error
	// SetRoomSeenAt двигает отметку прочитанного ОДНОЙ комнаты (rooms_seen_at.<roomId>)
	SetRoomSeenAt(ctx context.Context, userId int, roomId string, at time.Time) error
	AddAlias(ctx context.Context, userId int, alias string) error
	AddPushToken(ctx context.Context, userId int, token api.PushToken) error
	RemovePushToken(ctx context.Context, userId int, token string) error
	// RevokeTokens ставит отсечку отзыва и убирает push-токены всех устройств:
	// после неё старые JWT не работают, а уведомления не уходят на потерянный
	// телефон
	RevokeTokens(ctx context.Context, userId int, at time.Time) error
	// EnsureBindingToken возвращает purchase_binding_token живого пользователя,
	// заводя его при первом обращении. Идемпотентен и безопасен к гонке:
	// параллельные вызовы получают ОДНО значение (токен вшивается в уже
	// совершённые покупки, второй перетёр бы привязку). Пользователя нет или он
	// удалён — mongo.ErrNoDocuments
	EnsureBindingToken(ctx context.Context, userId int) (string, error)
}

type RoomRepository interface {
	FindById(ctx context.Context, id string) (*api.Room, error)
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	// LeaveRoom возвращает true, если пользователь действительно был в комнате
	LeaveRoom(ctx context.Context, userId int, roomId string) (bool, error)
	SaveRoom(ctx context.Context, r *api.Room) (primitive.ObjectID, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindArchivedRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
	UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error
	// UpdateOperationIfUnchanged записывает правку, только если с момента чтения
	// операции её никто не менял (ErrStaleOperation — меняли)
	UpdateOperationIfUnchanged(ctx context.Context, o *api.Operation, roomId string) error
	// ActivateOperation переводит черновик в действующий расход и требует, чтобы
	// связываемые им люди были участниками комнаты. ErrParticipantLeft — кто-то
	// вышел, пока черновик собирали
	ActivateOperation(ctx context.Context, o *api.Operation, roomId string) error
	SetNotificationSent(ctx context.Context, roomId string, operationId primitive.ObjectID, sent []int) error
	CreateOperation(ctx context.Context, o *api.Operation, roomId string) error
	// CreateOperationIfAbsent атомарно добавляет операцию, если в комнате ещё нет
	// операции с тем же client_op_id (o.ClientOpId обязан быть непустым).
	// Возвращает true, если операция вставлена; false — если дубль уже существует;
	// mongo.ErrNoDocuments — если комнаты нет
	CreateOperationIfAbsent(ctx context.Context, o *api.Operation, roomId string) (bool, error)
	// DeleteOperation переводит операцию в статус archive (мягкое удаление).
	// false — архивировать было нечего: нет комнаты, нет операции или её уже
	// заархивировали
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) (bool, error)
	// PurgeOperation вырезает операцию из документа физически. Только для отката
	// вставки, которой не должно было быть (см. компенсацию переплаты)
	PurgeOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	ArchiveRoom(ctx context.Context, userId int, roomId string) error
	UnArchiveRoom(ctx context.Context, userId int, roomId string) error
	FinishedAddOperation(ctx context.Context, userId int, roomId string) error
	UnFinishedAddOperation(ctx context.Context, userId int, roomId string) error
	PaidOfDebts(ctx context.Context, userIds []int, roomId string) error
	UpdateCurrency(ctx context.Context, roomId string, currency string) error
	// SetRoomScale переводит комнату в другую шкалу, пересчитывая деньги всех её
	// операций одним обновлением документа. ErrRoomBusy — комнату писали, пока
	// шёл пересчёт, и правку нужно повторить
	SetRoomScale(ctx context.Context, roomId string, exp int) (*api.Room, error)
	// SetAvatarFileId ставит ссылку на аву комнаты (пустая строка снимает её) и
	// возвращает ПРЕЖНЮЮ ссылку — ту, которую этот вызов вытеснил
	SetAvatarFileId(ctx context.Context, roomId string, fileId string) (string, error)
	// EachRoomCreatedAfter отдаёт комнаты, созданные после since, ПОРЦИЯМИ:
	// комнаты не помещаются в память все разом (в каждой лежат все её операции)
	EachRoomCreatedAfter(ctx context.Context, since time.Time, batch int, fn func([]api.Room) error) error
	// EnsureRoomIndexes создаёт индексы коллекции room. Идемпотентно
	EnsureRoomIndexes(ctx context.Context) error
	// AnonymizeUser затирает имя пользователя во ВСЕХ встроенных снимках комнат
	// (users[], operations[].donor, operations[].recipients[],
	// operations[].recipients_with_sum[].user) и вычищает оттуда поля личности.
	// Числовые id, суммы, доли и item.shares[].user_id не меняются — расчёт
	// долгов после анонимизации обязан дать тот же результат.
	// Идемпотентен: повторный вызов переписывает те же значения
	AnonymizeUser(ctx context.Context, userId int, placeholder string) error
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
	// DeleteByUserId удаляет коды входа пользователя — нужен удалению аккаунта:
	// живой код продолжал бы выдавать токен на tombstone
	DeleteByUserId(ctx context.Context, userId int) error
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
	// DeleteByUserId удаляет репорты пользователя. Нужен удалению аккаунта:
	// bug_report хранит username, display_name и СВОБОДНЫЙ ТЕКСТ жалобы —
	// это реальный PII, а не технический мусор
	DeleteByUserId(ctx context.Context, userId int) error
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

// DeleteByUserId удаляет все репорты пользователя (чистка PII при удалении аккаунта)
func (br MongoBugReportRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := br.col.DeleteMany(ctx, bson.M{"user_id": userId})
	return err
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

// DeleteByUserId удаляет коды входа пользователя. Вызывается при удалении
// аккаунта: невыданный/неиспользованный код иначе продолжал бы логинить в
// tombstone, а auth-middleware отвергал бы выданный токен — вход выглядел бы
// сломанным вместо «аккаунта нет»
func (lr MongoLoginCodeRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := lr.col.DeleteMany(ctx, bson.M{"user_id": userId})
	return err
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
	// Документ мог быть записан до появления минорных полей — достраиваем их
	// на чтении, чтобы выше по стеку не пришлось знать, какого возраста запись.
	api.FillRoomMoney(rm)
	return rm, nil
}

// JoinToRoom добавляет пользователя в комнату. Идемпотентен: повторный вызов
// для существующего участника — не ошибка, просто ничего не меняет.
//
// Одним атомарным UpdateOne, а не парой «проверить hasUserInRoom → $push»:
// раньше между проверкой и записью было окно, и два одновременных добавления
// одного человека клали в users ДВА снимка. Пока добавить себя мог только сам
// пользователь, окно было практически недостижимо; приглашения создают второй
// путь записи (человек принял приглашение и параллельно прошёл по ссылке), и
// дубль становится реальным. А дубль в users — это дубль участника в расчёте
// долгов, то есть тихо неверные деньги.
//
// Условие "users._id": {$ne: u.ID} проверяется и применяется сервером mongo в
// одной операции, поэтому гонки не остаётся.
func (rr MongoRoomRepository) JoinToRoom(ctx context.Context, u api.User, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": hex, "users._id": bson.M{"$ne": u.ID}}
	update := bson.M{"$push": bson.M{"users": u.Snapshot()}}
	// MatchedCount == 0 значит «комнаты нет ИЛИ пользователь уже участник».
	// Оба случая для вызывающего одинаковы: состояние уже такое, каким он его
	// хотел видеть, либо комнату он проверил раньше (roomForMember/findRoom).
	_, err = rr.col.UpdateOne(ctx, filter, update)
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
// пользователей и достроенными деньгами; id не трогаются.
//
// Деньги достраиваются ЗДЕСЬ, а не у вызывающих: через эту функцию проходят
// все четыре записи операции, и один забытый вызов на стороне бота или REST
// положил бы в базу документ без минорных полей — то есть тихо неполные
// деньги. exp — шкала комнаты, в которую идёт запись.
func sanitizeOperation(o *api.Operation, exp int) *api.Operation {
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
	// Позиции копируются вглубь: FillMoney правит доли на месте, а срез Shares
	// у поверхностной копии тот же самый, и правка уехала бы в операцию
	// вызывающего.
	if c.Items != nil {
		items := make([]api.OperationItem, len(c.Items))
		copy(items, c.Items)
		for i := range items {
			if items[i].Shares != nil {
				sh := make([]api.ItemShare, len(items[i].Shares))
				copy(sh, items[i].Shares)
				items[i].Shares = sh
			}
		}
		c.Items = items
	}
	api.FillMoney(&c, exp)
	return &c
}

// ErrRoomBusy — комнату писали, пока шёл пересчёт шкалы. Пересчёт читает
// операции, пересчитывает их в памяти и кладёт обратно целиком, поэтому чужая
// запись в этот момент была бы затёрта. Вызывающему остаётся повторить.
var ErrRoomBusy = errors.New("room changed during rescale")

// SetRoomScale переводит комнату в шкалу exp: пересчитывает деньги всех
// операций и записывает их ОДНИМ обновлением документа. Одним — потому что
// половина комнаты в одной шкале и половина в другой это деньги, которых нет.
//
// Запись условная: если между чтением и записью в комнате появилась, исчезла
// или изменилась операция, обновление не срабатывает и возвращается
// ErrRoomBusy. Сторожей два, и оба нужны: размер массива ловит добавление и
// удаление, максимальная версия операции — правку существующей.
func (rr MongoRoomRepository) SetRoomScale(ctx context.Context, roomId string, exp int) (*api.Room, error) {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return nil, err
	}
	room, err := rr.FindById(ctx, roomId)
	if err != nil {
		return nil, err
	}
	if api.RoomExponent(room) == exp {
		return room, nil
	}

	var ops []api.Operation
	if room.Operations != nil {
		ops = *room.Operations
	}
	maxVersion := 0
	for _, o := range ops {
		if o.Version > maxVersion {
			maxVersion = o.Version
		}
	}

	before := room.ScaleVersion
	api.RescaleRoom(room, exp)

	filter := bson.M{"$and": bson.A{
		bson.M{"_id": hex},
		bson.M{"scale_version": versionFilter(before)},
		bson.M{"operations": bson.M{"$size": len(ops)}},
		bson.M{"operations": bson.M{"$not": bson.M{"$elemMatch": bson.M{"version": bson.M{"$gt": maxVersion}}}}},
	}}
	sanitized := make([]api.Operation, len(ops))
	for i := range ops {
		sanitized[i] = *sanitizeOperation(&ops[i], exp)
	}
	update := bson.M{
		"$set": bson.M{"display_exponent": exp, "operations": sanitized},
		"$inc": bson.M{"scale_version": 1},
	}
	res, err := rr.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, err
	}
	if res.MatchedCount == 0 {
		return nil, ErrRoomBusy
	}
	return room, nil
}

// roomExponent — шкала комнаты одним лёгким запросом (проекция на два поля).
// Спрашивает её сам репозиторий: так запись операции не может обойтись без
// шкалы, даже если вызывающий про неё не думал.
func (rr MongoRoomRepository) roomExponent(ctx context.Context, id primitive.ObjectID) (int, error) {
	var room api.Room
	err := rr.col.FindOne(ctx, bson.M{"_id": id},
		options.FindOne().SetProjection(bson.M{"currency": 1, "display_exponent": 1})).Decode(&room)
	if err != nil {
		return 0, err
	}
	return api.RoomExponent(&room), nil
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
			ops[i] = *sanitizeOperation(&(*c.Operations)[i], api.RoomExponent(r))
		}
		c.Operations = &ops
	}
	return &c
}

// LeaveRoom убирает пользователя из комнаты и возвращает true, если он в ней
// действительно был.
//
// Условие членства стоит в ФИЛЬТРЕ, а не проверяется отдельным чтением: два
// одновременных выхода ОДНОГО человека (нажал дважды, ретрай сети) иначе оба
// увидели бы себя участником и сняли бы его дважды. Здесь mongo решает это одной
// операцией: matched==0 значит «комнаты нет или пользователь уже не участник».
//
// ⚠️ Гонку ДВУХ РАЗНЫХ участников комнаты на двоих фильтр НЕ закрывает: каждый
// успевает пройти проверку «я не последний» до записи, и комната может остаться
// без участников. Принятый риск: выйти захотели оба, данные комнаты целы,
// теряется только доступ к ней. Закрыть это можно единственным способом —
// условием на размер users прямо в фильтре, — но оно запретило бы выход
// последнему участнику и в боте, где такой сценарий легален.
//
// Заодно вычищается id из room_states: archived/paid_off_debts — это списки
// int, и без чистки вернувшийся по повторному приглашению увидел бы комнату
// сразу «в архиве» у себя, а погашенные долги — помеченными.
//
// ВАЖНО: полное правило «есть ли на человеке операции» этот метод НЕ повторяет —
// оно живёт в rest/боте на нормализованной комнате (api.HasOperations). Здесь
// стоит лишь его УЗКАЯ часть (activeOperationOf), закрывающая гонку записи;
// см. комментарий у activeOperationOf.
func (rr MongoRoomRepository) LeaveRoom(ctx context.Context, userId int, roomId string) (bool, error) {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return false, err
	}
	filter := bson.M{"_id": hex, "users._id": userId, "operations": bson.M{"$not": activeOperationOf(userId)}}
	update := bson.M{
		"$pull": bson.M{
			"users":                              bson.M{"_id": userId},
			"room_states.archived":               userId,
			"room_states.paid_off_debts":         userId,
			"room_states.finished_add_operation": userId,
		},
	}
	res, err := rr.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// activeOperationOf — $elemMatch «в комнате есть действующий расход этого
// человека». Стоит в фильтре LeaveRoom, потому что users[] и operations[] лежат
// в ОДНОМ документе комнаты: mongo применяет условие и $pull одной операцией,
// и расход, заведённый на выходящего между проверкой в rest и этой записью, не
// проскочит. Без него уход стирал бы долг ровно так, как запрещает правило
// «пока на человеке висят расходы, убрать его нельзя»: человек уже не участник,
// комнату не видит, а убрать себя из расхода некому.
//
// Фильтр НАМЕРЕННО уже правила api.HasOperations и не пытается быть его второй
// копией:
//   - только status == active. Легаси эпохи master-2021 (status в базе нет,
//     доли синтезируются в памяти) он не видит — и не должен: такие операции
//     существуют на момент чтения комнаты и отсекаются проверкой в rest/боте,
//     а гонка возможна лишь с НОВОЙ записью, а её оба пути пишут со status;
//   - донора держит только расход с получателями — как в NormalizedOperation,
//     где активная операция без долей понижается до драфта. Иначе брошенный
//     драфт бота снова запирал бы человека в комнате навсегда.
//
// Расхождение поэтому одностороннее: всё, что ловит фильтр, поймала бы и
// проверка в памяти, а значит matched==0 не может отказать в законном выходе.
func activeOperationOf(userId int) bson.M {
	return bson.M{"$elemMatch": bson.M{
		"status": api.StatusActive,
		"$or": bson.A{
			bson.M{"recipients_with_sum.user._id": userId},
			bson.M{"$and": bson.A{
				bson.M{"donor._id": userId},
				bson.M{"$or": bson.A{
					bson.M{"recipients_with_sum.0": bson.M{"$exists": true}},
					bson.M{"recipients.0": bson.M{"$exists": true}},
				}},
			}},
		},
	}}
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

// hasUserInRoom удалён вместе с check-then-act в JoinToRoom: единственным его
// потребителем была та самая небезопасная пара «проверить → записать».
// Проверка членства теперь стоит внутри фильтра UpdateOne, а чтение членства
// вне записи делает isRoomMember по уже загруженной комнате (пакет rest).

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
	for i := range m {
		api.FillRoomMoney(&m[i])
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
	for i := range m {
		api.FillRoomMoney(&m[i])
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
	for i := range m {
		api.FillRoomMoney(&m[i])
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

// Потолок документа в mongo — 16 МБ, и участники с операциями лежат ВНУТРИ
// документа комнаты. Долгоживущая группа однажды упрётся в него, и добавить
// расход станет нельзя вообще — не ошибка сети, а необратимое состояние.
// Смена схемы (вынос операций в отдельную коллекцию) — отдельная работа; здесь
// потолок хотя бы перестаёт быть невидимым.
const (
	// mongoDocumentLimit — жёсткий предел mongo на документ.
	mongoDocumentLimit = 16 * 1024 * 1024
	// roomSizeWarnAt — с какого размера пишем предупреждение в лог (половина).
	roomSizeWarnAt = mongoDocumentLimit / 2
	// roomSizeRejectAt — с какого размера отказываемся дописывать. Запас в 1 МБ
	// от предела: между проверкой и записью в комнату может лечь чужой расход,
	// и упереться в потолок ровно на границе — значит отдать невнятную ошибку
	// mongo вместо понятного текста
	roomSizeRejectAt = mongoDocumentLimit - 1024*1024
)

// ErrRoomTooLarge — документ комнаты у потолка: дописывать в него нельзя.
var ErrRoomTooLarge = errors.New("room document is close to the mongo size limit")

// checkRoomSize смотрит, сколько места занимает комната, и решает, можно ли
// дописывать. Ошибку измерения не превращаем в отказ: не смочь посчитать размер
// — не повод запретить человеку вносить расход.
func (rr MongoRoomRepository) checkRoomSize(ctx context.Context, hex primitive.ObjectID) error {
	size, err := rr.roomSize(ctx, hex)
	if err != nil || size == 0 {
		return nil
	}
	if size >= roomSizeRejectAt {
		log.Error().Str("room", hex.Hex()).Int("bytes", size).
			Msg("документ комнаты у потолка mongo: запись отклонена")
		return ErrRoomTooLarge
	}
	if size >= roomSizeWarnAt {
		log.Warn().Str("room", hex.Hex()).Int("bytes", size).
			Msg("документ комнаты перевалил половину потолка mongo")
	}
	return nil
}

// roomSize — размер документа комнаты в байтах ($bsonSize считает на сервере,
// без пересылки самого документа).
func (rr MongoRoomRepository) roomSize(ctx context.Context, hex primitive.ObjectID) (int, error) {
	cur, err := rr.col.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"_id": hex}},
		{"$project": bson.M{"size": bson.M{"$bsonSize": "$$ROOT"}}},
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var rows []struct {
		Size int `bson:"size"`
	}
	if err := cur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return 0, err
	}
	return rows[0].Size, nil
}

// LogLargestRooms пишет в лог самые крупные комнаты. Зовётся один раз при
// старте: до этого никто не знал, насколько мы близко к потолку.
func (rr MongoRoomRepository) LogLargestRooms(ctx context.Context, top int) {
	cur, err := rr.col.Aggregate(ctx, []bson.M{
		{"$project": bson.M{"name": 1, "size": bson.M{"$bsonSize": "$$ROOT"}}},
		{"$sort": bson.M{"size": -1}},
		{"$limit": top},
	})
	if err != nil {
		log.Warn().Err(err).Msg("не удалось измерить размеры комнат")
		return
	}
	defer func() { _ = cur.Close(ctx) }()
	var rows []struct {
		ID   primitive.ObjectID `bson:"_id"`
		Name string             `bson:"name"`
		Size int                `bson:"size"`
	}
	if err := cur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return
	}
	for _, r := range rows {
		event := log.Info()
		if r.Size >= roomSizeWarnAt {
			event = log.Warn()
		}
		event.Str("room", r.ID.Hex()).Int("bytes", r.Size).
			Float64("percentOfLimit", float64(r.Size)*100/float64(mongoDocumentLimit)).
			Msg("размер документа комнаты")
	}
}

// ErrStaleOperation — расход изменили с тех пор, как его прочитал редактирующий.
// Записать поверх нельзя: чужая правка исчезла бы молча.
var ErrStaleOperation = errors.New("operation was changed by someone else")

// UpdateOperation записывает операцию безусловно, растя версию. Так пишет бот и
// клиенты, которые про версию ещё не знают: у них на руках сборки, где поля нет
// вовсе, и требовать его — значит сломать установленное.
func (rr MongoRoomRepository) UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	return rr.updateOperation(ctx, o, roomId, false)
}

// UpdateOperationIfUnchanged записывает операцию, ТОЛЬКО если с момента её
// чтения никто не писал: o.Version — версия, которую видел редактирующий.
// ErrStaleOperation — писали, и правку нужно пересобрать по свежим данным.
func (rr MongoRoomRepository) UpdateOperationIfUnchanged(ctx context.Context, o *api.Operation, roomId string) error {
	return rr.updateOperation(ctx, o, roomId, true)
}

func (rr MongoRoomRepository) updateOperation(ctx context.Context, o *api.Operation, roomId string, conditional bool) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	elem := bson.M{"_id": o.ID}
	if conditional {
		// versionFilter, а не равенство: version: 0 НЕ находит документ, где поля
		// версии нет вовсе (отсутствующее поле mongo сравнивает только с null)
		elem["version"] = versionFilter(o.Version)
	}
	filter := bson.M{"_id": hex, "operations": bson.M{"$elemMatch": elem}}

	exp, err := rr.roomExponent(ctx, hex)
	if err != nil {
		return err
	}

	next := *o
	next.Version = o.Version + 1
	res, err := rr.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"operations.$": sanitizeOperation(&next, exp)}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		if conditional {
			// операция на месте, не совпала только версия — это конфликт правок,
			// а не пропавшая операция: человеку про них говорят разное
			n, cErr := rr.col.CountDocuments(ctx, bson.M{"_id": hex, "operations._id": o.ID})
			if cErr != nil {
				return cErr
			}
			if n > 0 {
				return ErrStaleOperation
			}
		}
		return mongo.ErrNoDocuments
	}
	// версия ушла вперёд — вызывающий отдаёт её клиенту, иначе следующая правка
	// оказалась бы конфликтной сразу же
	o.Version = next.Version
	return nil
}

// ActivateOperation — тот же UpdateOperation, но с условием состава: все, кого
// расход СВЯЗЫВАЕТ, обязаны быть участниками комнаты.
//
// Переход draft → active это рождение долга, и на него правило «состав важнее
// снимка» распространяется так же, как на вставку (см. boundMembersFilter). Своего
// условия он требует отдельно, потому что черновик живёт долго: бот создаёт его
// на первом экране, а активируют его тапом «Готово» через минуты — и всё это
// время черновик НИКОГО не держит в комнате (api.HasOperations смотрит только
// на активные, LeaveRoom — только на status active). Получатель успевает выйти
// и оказаться должником комнаты, которой уже не видит.
//
// Безусловным его делать нельзя (и UpdateOperation остаётся безусловным
// намеренно): в старых комнатах есть действующие расходы с давно вышедшими
// участниками, и общий фильтр сделал бы их нередактируемыми.
func (rr MongoRoomRepository) ActivateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.M{"_id": hex, "operations._id": o.ID}
	for k, v := range boundMembersFilter(o) {
		filter[k] = v
	}
	// $[op] вместо позиционного $: в фильтре два массива (users и operations),
	// и обычный $ берёт индекс совпадения по ПЕРВОМУ из них — обновление молча
	// уходило бы не в ту операцию. arrayFilters выбирает элемент сам
	exp, err := rr.roomExponent(ctx, hex)
	if err != nil {
		return err
	}
	update := bson.M{"$set": bson.M{"operations.$[op]": sanitizeOperation(o, exp)}}
	opts := options.Update().SetArrayFilters(options.ArrayFilters{
		Filters: []interface{}{bson.M{"op._id": o.ID}},
	})
	res, err := rr.col.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		n, cErr := rr.col.CountDocuments(ctx, bson.M{"_id": hex, "operations._id": o.ID})
		if cErr != nil {
			return cErr
		}
		if n == 0 {
			return mongo.ErrNoDocuments
		}
		return ErrParticipantLeft
	}
	return nil
}

// ErrParticipantLeft — участник новой операции вышел из комнаты, пока запрос
// шёл: вставка отменена. Отдельная ошибка, а не ErrNoDocuments, потому что
// сказать человеку надо разное — «группы нет» и «состав изменился».
var ErrParticipantLeft = errors.New("operation participant is not a room member")

// boundMembersFilter — условие «все, кого расход связывает, сейчас в комнате».
// Набор берётся ПОСЛЕ нормализации (api.NormalizedOperation), то есть ровно
// тот, по которому api.HasOperations не выпускает человека из комнаты:
// плательщик и текущие доли, а при пустых долях — легаси-получатели.
//
// Протухший список recipients при непустых долях НЕ учитывается: бот копирует
// его при правке и никогда не чистит, из-за чего он держит людей, которых
// расход давно не касается (см. TestBotLeaveAllowsWhenLegacyRecipientsAreStale).
// Требуя членства по нему, мы запретили бы активировать правку старого расхода
// в комнате, откуда такой «получатель» вышел, — то есть чинили бы гонку ценой
// неработающего редактирования.
//
// Тем же условием проверяется и ВСТАВКА (CreateOperation, CreateOperationIfAbsent) —
// это вторая половина защиты от гонки «выход × расход», первая в LeaveRoom: rest
// проверяет членство по прочитанному снимку, а между чтением и вставкой человек
// успевает выйти. Прежде у вставки был свой, более широкий набор (плюс легаси-
// recipients целиком), и он ломал правку в боте: черновик правки копирует
// протухший recipients, поэтому старый расход не заводился вовсе — бот молча
// возвращался с пустым экраном. Правило обязано быть ОДНО: кого расход держит в
// комнате, того и требуем при записи.
//
// Черновик и архив не связывают никого, поэтому у них условия нет: черновик
// пройдёт проверку позже, при активации.
func boundMembersFilter(o *api.Operation) bson.M {
	if o == nil {
		return bson.M{}
	}
	norm := api.NormalizedOperation(*o)
	if norm.Status != api.StatusActive {
		// Черновик и архив никого не связывают — условию нечего проверять
		return bson.M{}
	}
	seen := map[int]bool{}
	var ids []int
	add := func(id int) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if norm.Donor != nil {
		add(norm.Donor.ID)
	}
	for _, r := range norm.RecipientsWithSum {
		add(r.User.ID)
	}
	if len(ids) == 0 {
		return bson.M{}
	}
	return bson.M{"users._id": bson.M{"$all": ids}}
}

// CreateOperation добавляет новую операцию одним $push (операции всегда создаются
// с новым ObjectID, прежний $pull был no-op). MatchedCount == 0 — комнаты нет
// (mongo.ErrNoDocuments) или участник операции успел выйти (ErrParticipantLeft)
func (rr MongoRoomRepository) CreateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	if err := rr.checkRoomSize(ctx, hex); err != nil {
		return err
	}
	filter := bson.M{"_id": hex}
	for k, v := range boundMembersFilter(o) {
		filter[k] = v
	}
	exp, err := rr.roomExponent(ctx, hex)
	if err != nil {
		return err
	}
	res, err := rr.col.UpdateOne(ctx, filter, bson.D{{Key: "$push", Value: bson.D{{Key: "operations", Value: sanitizeOperation(o, exp)}}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		n, cErr := rr.col.CountDocuments(ctx, bson.M{"_id": hex})
		if cErr != nil {
			return cErr
		}
		if n == 0 {
			return mongo.ErrNoDocuments
		}
		return ErrParticipantLeft
	}
	return nil
}

// CreateOperationIfAbsent идемпотентная вставка операции по клиентскому ключу
// client_op_id: один UpdateOne с фильтром {_id, "operations.client_op_id": {$ne: id}}
// — $push выполняется, только если ни одна операция комнаты не несёт этот ключ,
// проверка и вставка атомарны (конкурентные повторы не создают дубль).
// MatchedCount == 0 означает «комнаты нет», «дубль уже есть» ЛИБО «участник
// операции вышел» (см. boundMembersFilter) — различаем дополнительным чтением:
// комнаты нет — mongo.ErrNoDocuments, дубль есть — (false, nil) и существующую
// операцию вычитывает вызывающий, иначе — ErrParticipantLeft
func (rr MongoRoomRepository) CreateOperationIfAbsent(ctx context.Context, o *api.Operation, roomId string) (bool, error) {
	if o.ClientOpId == "" {
		return false, errors.New("client_op_id must not be empty")
	}
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return false, err
	}
	if err := rr.checkRoomSize(ctx, hex); err != nil {
		return false, err
	}
	filter := bson.M{"_id": hex, "operations.client_op_id": bson.M{"$ne": o.ClientOpId}}
	for k, v := range boundMembersFilter(o) {
		filter[k] = v
	}
	exp, err := rr.roomExponent(ctx, hex)
	if err != nil {
		return false, err
	}
	res, err := rr.col.UpdateOne(ctx, filter, bson.D{{Key: "$push", Value: bson.D{{Key: "operations", Value: sanitizeOperation(o, exp)}}}})
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
	// Комната есть: либо повтор с тем же ключом (тогда идемпотентный ответ),
	// либо участник вышел — состав важнее, чем ключ, но проверять его надо
	// вторым: повтор из outbox приходит и после чужого выхода.
	dup, err := rr.col.CountDocuments(ctx, bson.M{"_id": hex, "operations.client_op_id": o.ClientOpId})
	if err != nil {
		return false, err
	}
	if dup > 0 {
		return false, nil
	}
	return false, ErrParticipantLeft
}

// DeleteOperation помечает операцию архивной, а не вырезает её из документа:
// физическое удаление невосстановимо, а поводов вернуть расход хватает — от
// промаха по кнопке до чужого удаления в общей группе. Статус archive уже
// понимают и бот, и нормализация (api.ActiveOperations), поэтому скрытие
// получается само собой.
//
// Возвращает false, когда архивировать было нечего: комнаты нет, операции нет
// либо её успели заархивировать конкурентно. Условие живости стоит в фильтре, а
// не проверяется отдельным чтением, — иначе между чтением и записью остаётся
// зазор. Позиционный `$` берёт элемент, найденный $elemMatch: operations —
// единственный массив в фильтре, привязка однозначна.
//
// Легаси-операции без поля status условие проходят: $ne матчит и отсутствующее
// поле
func (rr MongoRoomRepository) DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) (bool, error) {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return false, err
	}
	filter := bson.M{
		"_id": hex,
		"operations": bson.M{"$elemMatch": bson.M{
			"_id":    operationId,
			"status": bson.M{"$ne": api.StatusArchive},
		}},
	}
	update := bson.M{"$set": bson.M{"operations.$.status": api.StatusArchive}}
	res, err := rr.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// PurgeOperation вырезает операцию из документа насовсем. Единственный законный
// повод — откат только что вставленной записи, которой не должно было быть
// (компенсация переплаты в погашении): следа от неё не остаётся нигде, включая
// историю. Для пользовательского удаления есть DeleteOperation
func (rr MongoRoomRepository) PurgeOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.M{"$pull": bson.M{"operations": bson.M{"_id": operationId}}})
	return err
}

// ErrScaleNotSupported — у новой валюты нет дробной части, а комната считает
// копейки. Сменить валюту, не тронув суммы, тут нельзя: комната в шкале 2
// хранит 2080, и в иенах это число уже ничего не значит.
var ErrScaleNotSupported = errors.New("currency does not support room scale")

// UpdateCurrency меняет валюту комнаты. Суммы при этом НЕ пересчитываются —
// меняется обозначение, и это давнее осознанное поведение.
//
// ⚠️ Оно безопасно ровно до тех пор, пока новая валюта допускает шкалу
// комнаты. Комната с копейками хранит 20,80 как 2080; переведи её в иены, где
// дробной части нет, и то же число прочтётся как 2080 иен — деньги вырастут в
// сто раз, не изменившись в базе ни на единицу.
//
// Проверка живёт ЗДЕСЬ, а не в обработчике REST: валюту меняет и бот
// (internal/bot/setting_screen.go), мимо всякого REST.
func (rr MongoRoomRepository) UpdateCurrency(ctx context.Context, roomId string, currency string) error {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return err
	}

	// Проекция на первую операцию, а не чтение комнаты целиком: нужно знать
	// лишь «есть ли в ней хоть один расход».
	var head api.Room
	err = rr.col.FindOne(ctx, bson.M{"_id": hex},
		options.FindOne().SetProjection(bson.M{
			"currency":         1,
			"display_exponent": 1,
			"operations":       bson.M{"$slice": 1},
		})).Decode(&head)
	if err != nil {
		return err
	}

	exp := api.RoomExponent(&head)
	hasOperations := head.Operations != nil && len(*head.Operations) > 0
	newExp, ok := api.ScaleAfterCurrencyChange(exp, hasOperations, currency)
	if !ok {
		return ErrScaleNotSupported
	}
	update := bson.M{"currency": currency}
	if newExp != exp {
		update["display_exponent"] = newExp
	}

	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	_, err = rr.col.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: update}})
	return err
}

// EnsureRoomIndexes создаёт индексы коллекции room. Идемпотентно; вызывать при старте.
//   - по create_at: джоб напоминаний раз в сутки выбирает комнаты за последние
//     два месяца, и без индекса это полный скан коллекции, которая растёт
//     навсегда. Он же обслуживает сортировку списков комнат.
func (rr MongoRoomRepository) EnsureRoomIndexes(ctx context.Context) error {
	_, err := rr.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "create_at", Value: ascParameter}},
			Options: options.Index().SetName("idx_create_at"),
		},
	})
	return err
}

// EachRoomCreatedAfter вызывает fn для каждой порции комнат, созданных после
// since. Порциями, а не одним срезом: документ комнаты содержит ВСЕ её
// операции и подбирается к 16 МБ, поэтому «выбрать все за два месяца» — это
// пиковая память и долгая пауза до первого действия.
func (rr MongoRoomRepository) EachRoomCreatedAfter(ctx context.Context, since time.Time, batch int, fn func([]api.Room) error) error {
	if batch <= 0 {
		batch = 50
	}
	cur, err := rr.col.Find(ctx, bson.M{"create_at": bson.M{"$gte": since}},
		options.Find().SetSort(bson.D{{Key: "create_at", Value: ascParameter}}).SetBatchSize(int32(batch)))
	if err != nil {
		return err
	}
	defer func() { _ = cur.Close(ctx) }()

	chunk := make([]api.Room, 0, batch)
	for cur.Next(ctx) {
		var room api.Room
		if err := cur.Decode(&room); err != nil {
			return err
		}
		chunk = append(chunk, room)
		if len(chunk) == batch {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = chunk[:0]
		}
	}
	if err := cur.Err(); err != nil {
		return err
	}
	if len(chunk) > 0 {
		return fn(chunk)
	}
	return nil
}

// SetAvatarFileId ставит ссылку на аву комнаты и возвращает прежнюю. Пустая
// строка снимает поле целиком ($unset, а не пустая строка в базе): клиент
// отличает «фото нет» по отсутствию ключа, а не по его значению.
//
// Прежнее значение берётся ИЗ ТОГО ЖЕ запроса (FindOneAndUpdate с pre-image), а
// не читается заранее: при двух одновременных загрузках в одну комнату оба
// запроса видели бы один и тот же снимок, удалили бы один и тот же файл, а
// проигравший гонку остался бы в базе навсегда — никем не адресуемый.
func (rr MongoRoomRepository) SetAvatarFileId(ctx context.Context, roomId string, fileId string) (string, error) {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return "", err
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: hex}}}}
	var update bson.D
	if fileId == "" {
		update = bson.D{{Key: "$unset", Value: bson.M{"avatar_file_id": ""}}}
	} else {
		update = bson.D{{Key: "$set", Value: bson.M{"avatar_file_id": fileId}}}
	}

	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.Before).
		SetProjection(bson.M{"avatar_file_id": 1})
	var before struct {
		AvatarFileId *string `bson:"avatar_file_id"`
	}
	if err := rr.col.FindOneAndUpdate(ctx, filter, update, opts).Decode(&before); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil
		}
		return "", err
	}
	if before.AvatarFileId == nil {
		return "", nil
	}
	return *before.AvatarFileId, nil
}

// DeletedUserPlaceholder — имя, которое остаётся от удалённого пользователя: и
// в самом tombstone-документе, и во встроенных снимках комнат. Снимки не
// перечитываются из канонического документа, поэтому имя приходится затирать
// в каждом из них отдельно (см. AnonymizeUser)
const DeletedUserPlaceholder = "Удалённый пользователь"

// snapshotPIIFields — поля снимка, которые вычищаются при анонимизации.
//
// RoomBrief — строка списка комнат для админки. Операций и участников здесь
// нет намеренно: в документе комнаты лежат ВСЕ её расходы (потолок 16 МБ), и
// вытянуть сотню таких ради списка нельзя. Числа считает сам mongo.
type RoomBrief struct {
	ID              primitive.ObjectID `bson:"_id"`
	Name            string             `bson:"name"`
	CreateAt        time.Time          `bson:"create_at"`
	Currency        string             `bson:"currency"`
	MemberCount     int                `bson:"member_count"`
	OperationCount  int                `bson:"operation_count"`
	LastOperationAt *time.Time         `bson:"last_operation_at"`
	SizeBytes       int                `bson:"size_bytes"`
}

// adminSearchLimit — потолок выдачи поиска, сколько бы ни просили: ответ
// собирается в памяти, и «покажи все» не должно означать «прочитай всю базу»
const adminSearchLimit = 100

// SearchRooms ищет комнаты по имени (без учёта регистра) либо по точному id.
// Пустой запрос — последние созданные.
//
// Имя ищется регулярным выражением по НЕиндексированному полю: это полный
// проход по коллекции, поэтому метод и живёт только в админке — на горячем
// пути такому не место
func (rr MongoRoomRepository) SearchRooms(ctx context.Context, query string, limit int) ([]RoomBrief, error) {
	if limit <= 0 || limit > adminSearchLimit {
		limit = adminSearchLimit
	}

	match := bson.M{}
	if q := strings.TrimSpace(query); q != "" {
		if hex, err := primitive.ObjectIDFromHex(q); err == nil {
			match["_id"] = hex
		} else {
			// Экранируем: имя комнаты пишет человек, и «(» из него не должно
			// становиться синтаксисом регулярного выражения
			match["name"] = bson.M{"$regex": regexp.QuoteMeta(q), "$options": "i"}
		}
	}

	cur, err := rr.col.Aggregate(ctx, []bson.M{
		{"$match": match},
		{"$sort": bson.M{"create_at": descParameter}},
		{"$limit": limit},
		{"$project": bson.M{
			"name":              1,
			"create_at":         1,
			"currency":          1,
			"member_count":      bson.M{"$size": bson.M{"$ifNull": bson.A{"$users", bson.A{}}}},
			"operation_count":   bson.M{"$size": bson.M{"$ifNull": bson.A{"$operations", bson.A{}}}},
			"last_operation_at": bson.M{"$max": "$operations.create_at"},
			"size_bytes":        bson.M{"$bsonSize": "$$ROOT"},
		}},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	rooms := []RoomBrief{}
	if err := cur.All(ctx, &rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

// RoomSizeBytes — вес документа комнаты. Наружу нужен админке: приближение к
// потолку mongo видно только так, а узнать о нём хочется до отказа записи
func (rr MongoRoomRepository) RoomSizeBytes(ctx context.Context, roomId string) (int, error) {
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return 0, err
	}
	return rr.roomSize(ctx, hex)
}

// SearchUsers ищет людей для админки: по номеру, @нику или отображаемому имени.
// Пустой запрос — последние заведённые (по убыванию номера: у telegram-аккаунтов
// это порядок регистрации, у остальных — порядок выдачи из аллокатора).
//
// Как и поиск комнат, ходит регулярным выражением по неиндексированным полям —
// это полный проход по коллекции, и место такому только в админке
func (r MongoUserRepository) SearchUsers(ctx context.Context, query string, limit int) ([]api.User, error) {
	if limit <= 0 || limit > adminSearchLimit {
		limit = adminSearchLimit
	}

	filter := bson.M{}
	if q := strings.TrimSpace(query); q != "" {
		if id, err := strconv.Atoi(q); err == nil {
			filter["_id"] = id
		} else {
			like := bson.M{"$regex": regexp.QuoteMeta(strings.TrimPrefix(q, "@")), "$options": "i"}
			filter["$or"] = bson.A{bson.M{"user_name": like}, bson.M{"display_name": like}}
		}
	}

	cur, err := r.col.Find(ctx, filter, options.Find().
		SetSort(bson.M{"_id": descParameter}).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	users := []api.User{}
	if err := cur.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// AllRoomsOfUser отдаёт ВСЕ комнаты человека, включая спрятанные им у себя:
// FindRoomsByUserId такие отфильтровывает, а админке нужно видеть всё —
// «у меня пропала группа» чаще всего означает именно архив
func (rr MongoRoomRepository) AllRoomsOfUser(ctx context.Context, userId int) ([]api.Room, error) {
	cur, err := rr.col.Find(ctx, bson.M{"users._id": userId},
		getOrderOptions("create_at", descParameter))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	rooms := []api.Room{}
	if err := cur.All(ctx, &rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

// user_name — часть отображаемой личности (@ник виден всем участникам).
// Остальные попали бы в снимок только у документов, записанных ДО санитайза
// (см. Snapshot и sanitizeUsers): там telegram_id/google_sub/apple_sub/email и
// push-токены лежат прямо в room, и удаление аккаунта обязано их оттуда убрать
var snapshotPIIFields = []string{
	"user_name", "email", "login_email", "google_sub", "apple_sub", "telegram_id",
	"push_tokens", "aliases", "bank_details",
}

// anonymizeTarget — один путь до снимка пользователя внутри документа room:
// чем отбирать документы, куда спускаться и какие элементы массивов трогать
type anonymizeTarget struct {
	filter       bson.M
	path         string
	arrayFilters []interface{}
}

// anonymizeTargets — пути до снимков пользователя внутри документа room.
//
// ⚠️ Почему четыре независимых UpdateMany, а не один с общими arrayFilters.
// Поля recipients и recipients_with_sum объявлены БЕЗ omitempty
// (api.Operation), а текущий код заполняет только одно из них: бот пишет
// recipients_with_sum, REST явно обнуляет recipients. В базе поэтому лежит
// recipients: null у всех современных документов и recipients_with_sum: null у
// архаичных. Обход пути operations.$[o].recipients.$[r] по null роняет ВЕСЬ
// UpdateMany ошибкой «cannot apply array updates to non-array» — то есть один
// легаси-документ в базе превратил бы DELETE /me в 500 для всех.
// Защита двойная: аррай-фильтр {$type: "array"} не даёт спуститься в null, а
// разбиение на независимые запросы не даёт одному пути уронить остальные
func anonymizeTargets(userId int) []anonymizeTarget {
	return []anonymizeTarget{
		{
			// участники комнаты
			filter:       bson.M{"users": bson.M{"$elemMatch": bson.M{"_id": userId}}},
			path:         "users.$[u]",
			arrayFilters: []interface{}{bson.M{"u._id": userId}},
		},
		{
			// автор расхода
			filter:       bson.M{"operations": bson.M{"$elemMatch": bson.M{"donor._id": userId}}},
			path:         "operations.$[o].donor",
			arrayFilters: []interface{}{bson.M{"o.donor._id": userId}},
		},
		{
			// легаси-получатели (архаичные документы)
			filter: bson.M{"operations": bson.M{"$elemMatch": bson.M{"recipients._id": userId}}},
			path:   "operations.$[o].recipients.$[r]",
			arrayFilters: []interface{}{
				bson.M{"o.recipients": bson.M{"$type": "array"}},
				bson.M{"r._id": userId},
			},
		},
		{
			// получатели с долями (современные документы)
			filter: bson.M{"operations": bson.M{"$elemMatch": bson.M{"recipients_with_sum.user._id": userId}}},
			path:   "operations.$[o].recipients_with_sum.$[r].user",
			arrayFilters: []interface{}{
				bson.M{"o.recipients_with_sum": bson.M{"$type": "array"}},
				bson.M{"r.user._id": userId},
			},
		},
	}
}

// AnonymizeUser затирает имя удалённого пользователя во всех встроенных снимках
// и вычищает оттуда PII.
//
// Меняются РОВНО display_name и поля из snapshotPIIFields. Числовые id, суммы,
// доли, item.shares[].user_id остаются как были: расчёт долгов после удаления
// аккаунта обязан дать тот же результат, что и до него (инвариант плана).
//
// Вызывается ПОСЛЕ SoftDeleteUser и никогда до: если анонимизация пройдёт, а
// tombstone упадёт, аккаунт останется живым с затёртым во всех комнатах именем,
// и восстановить его будет неоткуда — снимки из канонического документа не
// перестраиваются
func (rr MongoRoomRepository) AnonymizeUser(ctx context.Context, userId int, placeholder string) error {
	for _, t := range anonymizeTargets(userId) {
		unset := bson.M{}
		for _, field := range snapshotPIIFields {
			unset[t.path+"."+field] = ""
		}
		update := bson.D{
			{Key: "$set", Value: bson.M{t.path + ".display_name": placeholder}},
			{Key: "$unset", Value: unset},
		}
		opts := options.Update().SetArrayFilters(options.ArrayFilters{Filters: t.arrayFilters})
		if _, err := rr.col.UpdateMany(ctx, t.filter, update, opts); err != nil {
			return errors.Wrapf(err, "анонимизация снимков по пути %s", t.path)
		}
	}
	return nil
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

// NormalizeLoginEmail — канонический вид адреса входа: trim и нижний регистр.
// Единственная точка нормализации на весь проект: запись и поиск обязаны
// приводить адрес одинаково, иначе A@B.com и a@b.com разъедутся в разные аккаунты
func NormalizeLoginEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// FindByLoginEmail ищет живого пользователя по адресу входа с паролем
func (r MongoUserRepository) FindByLoginEmail(ctx context.Context, email string) (*api.User, error) {
	normalized := NormalizeLoginEmail(email)
	return r.findOne(ctx, append(bson.D{{Key: "login_email", Value: bson.D{{Key: "$eq", Value: normalized}}}}, notDeleted...))
}

// SetPasswordHash записывает хеш пароля. Через updateLiveUser (как все мутаторы
// профиля): tombstone не должен получить обратно рабочий способ входа
func (r MongoUserRepository) SetPasswordHash(ctx context.Context, userId int, hash string) error {
	updated, err := r.updateLiveUser(ctx, userId, bson.D{{Key: "$set", Value: bson.M{"password_hash": hash}}})
	if err != nil {
		return err
	}
	if !updated {
		return mongo.ErrNoDocuments
	}
	return nil
}

// EnsureBindingToken выдаёт токен привязки покупок, заводя его при первом
// обращении.
//
// Запись условная — фильтр требует, чтобы поля ЕЩЁ НЕ БЫЛО. Иначе два
// параллельных запроса (а их будет два: экран оплаты и восстановление покупок
// стартуют вместе) записали бы разные значения, и чек, купленный по первому,
// перестал бы сходиться с аккаунтом. Не совпал фильтр — значит либо токен уже
// есть, либо пользователя нет; отличаем чтением.
func (r MongoUserRepository) EnsureBindingToken(ctx context.Context, userId int) (string, error) {
	token := uuid.NewString()
	filter := append(
		bson.D{
			{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}},
			{Key: "purchase_binding_token", Value: bson.D{{Key: "$exists", Value: false}}},
		},
		notDeleted...,
	)
	update := bson.D{{Key: "$set", Value: bson.M{"purchase_binding_token": token}}}

	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return "", err
	}
	if res.MatchedCount > 0 {
		return token, nil
	}

	user, err := r.findOne(ctx, append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...))
	if err != nil {
		return "", err
	}
	if user.PurchaseBindingToken == "" {
		// Пустая строка в документе (а не отсутствие поля) — фильтр $exists её
		// не поймал бы, и метод зациклился бы на «уже есть, но нечего отдать».
		return "", mongo.ErrNoDocuments
	}
	return user.PurchaseBindingToken, nil
}

// UpdateAppleProfile дописывает данные из потока Sign in with Apple.
//
// Пустые аргументы пропускаются: Apple отдаёт email и имя только при ПЕРВОМ
// входе, и при всех последующих $set пустой строкой стёр бы сохранённое.
// Решение «заполнять или не трогать» принимает вызывающий (он видит текущий
// документ), здесь — вторая линия обороны.
//
// Ни upsert, ни фильтр по одному _id: обновляется только ЖИВОЙ документ.
// Иначе гонка «медленный вход через Apple ↔ параллельное удаление аккаунта»
// дописала бы apple_refresh_token в tombstone
func (r MongoUserRepository) UpdateAppleProfile(ctx context.Context, userId int, email, displayName, refreshToken string) error {
	set := bson.M{}
	if email != "" {
		set["email"] = email
	}
	if displayName != "" {
		set["display_name"] = displayName
	}
	if refreshToken != "" {
		set["apple_refresh_token"] = refreshToken
	}
	if len(set) == 0 {
		return nil
	}
	filter := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...)
	_, err := r.col.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: set}})
	return err
}

// Имена способов входа. Одни и те же строки живут в пути REST-эндпоинта
// (/api/v1/me/link/{provider}) и в маппинге на поля документа, поэтому
// объявлены один раз здесь, а не литералами в двух пакетах
const (
	IdentityTelegram = "telegram"
	IdentityGoogle   = "google"
	IdentityApple    = "apple"
	IdentityPassword = "password"
)

// identityFields — поле документа, хранящее личность провайдера. Значения
// совпадают с полями api.User (telegram_id, google_sub, apple_sub), по которым
// построены unique sparse индексы (см. EnsureIndexes).
//
// У пароля «личность» — это password_hash, а НЕ login_email: отвязка обязана
// убрать способ входа, но оставить адрес за аккаунтом. Иначе восстановление
// (войти другим способом и задать новый пароль из профиля) вернуло бы человеку
// пароль без почты, то есть бесполезный
var identityFields = map[string]string{
	IdentityTelegram: "telegram_id",
	IdentityGoogle:   "google_sub",
	IdentityApple:    "apple_sub",
	IdentityPassword: "password_hash",
}

// IsKnownIdentityProvider — поддерживается ли такой способ входа
func IsKnownIdentityProvider(provider string) bool {
	_, ok := identityFields[provider]
	return ok
}

// SetIdentity привязывает личность провайдера к пользователю.
//
// Ни upsert, ни фильтра по одному _id: обновляется ТОЛЬКО живой документ.
// Гонка, ради которой это критично: медленный POST /me/link/google уже прошёл
// auth-middleware, параллельно приходит DELETE /me, ставит tombstone и вычищает
// личности — и upsert (или фильтр без deleted_at) дописал бы google_sub обратно
// НА TOMBSTONE. Поиск по личностям удалённых пропускает, а unique sparse индекс
// значение уже занял: человек не смог бы зарегистрироваться заново тем же
// Google-аккаунтом, а создание падало бы на duplicate key до ручной правки базы.
//
// Пользователя нет или он удалён — mongo.ErrNoDocuments (а не тихий no-op:
// вызывающий обязан отличить «записали» от «записывать было некуда»).
// Личность занята другим документом — duplicate key от unique-индекса
func (r MongoUserRepository) SetIdentity(ctx context.Context, userId int, provider string, value interface{}) error {
	field, ok := identityFields[provider]
	if !ok {
		return errors.Errorf("неизвестный способ входа: %q", provider)
	}
	filter := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...)
	res, err := r.col.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.M{field: value}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// identityExtraFields — что ещё вычищается ВМЕСТЕ с личностью провайдера.
//
// apple_refresh_token принадлежит РОВНО той личности Apple, которую отвязывают.
// Оставить его — значит держать в базе рабочий секрет от личности, которой у
// пользователя больше нет, и при будущем DELETE /me отозвать токены уже
// отвязанного аккаунта. Сам отзыв делает REST-слой строго ДО этого вызова
// (см. handleUnlinkIdentity → revokeAppleTokens)
var identityExtraFields = map[string][]string{
	IdentityApple: {"apple_refresh_token"},
}

// ClearIdentity отвязывает способ входа: $unset, а не запись пустого значения —
// unique sparse индекс не должен видеть ни null, ни "". Фильтр и отсутствие
// upsert — по той же причине, что в SetIdentity
func (r MongoUserRepository) ClearIdentity(ctx context.Context, userId int, provider string) error {
	field, ok := identityFields[provider]
	if !ok {
		return errors.Errorf("неизвестный способ входа: %q", provider)
	}
	unset := bson.M{field: ""}
	for _, extra := range identityExtraFields[provider] {
		unset[extra] = ""
	}
	filter := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...)
	res, err := r.col.UpdateOne(ctx, filter, bson.D{{Key: "$unset", Value: unset}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// tombstoneExtraFields — что чистится при удалении аккаунта СВЕРХ списка PII
// снимка (snapshotPIIFields).
//
// apple_refresh_token в снимок комнаты не попадает никогда, поэтому в
// snapshotPIIFields его нет — но в документе пользователя это рабочий секрет
// от личности Apple, и пережить удаление аккаунта он не должен. Сам отзыв
// токенов делает REST-слой строго ДО этого вызова (см. handleDeleteMe).
//
// password_hash — по той же причине: секрет, в снимки комнат не попадающий.
// Вместе с login_email (он в snapshotPIIFields) это освобождает адрес под
// повторную регистрацию: unique sparse индекс отсутствующего поля не видит
//
// rooms_seen_at — не секрет, но его КЛЮЧИ это список комнат человека, и
// переживать удаление аккаунта такой след не должен: у tombstone нет ни
// карточек групп, ни счётчиков, чтобы отметки кому-то пригодились
var tombstoneExtraFields = []string{"apple_refresh_token", "password_hash", "rooms_seen_at"}

// SoftDeleteUser ставит tombstone: помечает документ удалённым, чистит PII и
// освобождает личности.
//
// Список $unset собирается из snapshotPIIFields плюс tombstoneExtraFields, а не
// пишется руками: новое PII-поле модели иначе пришлось бы добавлять в двух
// местах, и, забыв одно, оно пережило бы удаление аккаунта.
//
// Документ НЕ удаляется намеренно: auth-middleware выдаёт 401 по признаку
// deleted_at, а не по отсутствию документа — отличать «удалён» от «никогда не
// существовал» нужно, чтобы старый токен не проходил. Отсюда зеркальное
// требование к записи: раз документ остаётся, ЛЮБОЙ мутатор профиля обязан
// фильтровать его по deleted_at (см. updateLiveUser), иначе запрос со старым
// токеном вернёт на tombstone только что вычищенную PII.
//
// Личности ($unset telegram_id/google_sub/apple_sub) освобождаются, чтобы
// человек мог зарегистрироваться заново тем же Google/Apple/Telegram: unique
// sparse индекс не видит отсутствующих полей.
//
// Фильтра notDeleted здесь НЕТ и upsert'а тоже: метод обязан быть повторяемым
// (запрос DELETE /me мог упасть после этого шага, и его повторяют), но
// создавать документ из ничего он не должен — нет пользователя, значит
// mongo.ErrNoDocuments
func (r MongoUserRepository) SoftDeleteUser(ctx context.Context, userId int) error {
	unset := bson.M{}
	for _, field := range snapshotPIIFields {
		unset[field] = ""
	}
	for _, field := range tombstoneExtraFields {
		unset[field] = ""
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}
	update := bson.D{
		{Key: "$set", Value: bson.M{
			"deleted_at":   time.Now(),
			"display_name": DeletedUserPlaceholder,
		}},
		{Key: "$unset", Value: unset},
	}
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
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
	// Дата создания ставится здесь, а не «там, где выдаётся номер»: аллокатор
	// NextUserID зовут только google/apple, регистрация по паролю и telegram с
	// занятым _id, то есть мимо обычной telegram-регистрации, бота и dev-входа.
	// Это же место накрывает их все.
	if u.CreatedAt == nil {
		now := time.Now().UTC()
		u.CreatedAt = &now
	}
	_, err := r.col.InsertOne(ctx, u)
	return err
}

// NextUserID выдаёт номер для пользователя, у которого нет пригодного _id
// (вход через Google/Apple, а также telegram-вход, чей telegram id уже занят
// другим аккаунтом). Делегирует в общий счётчик коллекции sequence
func (r MongoUserRepository) NextUserID(ctx context.Context) (int, error) {
	return r.seq.NextUserID(ctx)
}

// upsertTelegramAttempts — сколько раз UpsertTelegramUser переспрашивает базу на
// duplicate key. Двух хватило бы на любую реальную гонку (проигравший видит
// победителя со второй попытки), третья — запас на цепочку «_id занят → взяли
// синтетический → и он уже занят»
const upsertTelegramAttempts = 3

// UpsertTelegramUser — единственная точка входа telegram-личности. Ищет живого
// пользователя по telegram_id и, если не нашёл, заводит нового.
//
// _id нового пользователя по умолчанию равен telegram id: так исторически
// заведены ВСЕ существующие аккаунты, и сохранение этого правила означает, что
// у обычного telegram-пользователя _id остаётся привычным. Если же _id уже
// занят другим документом (например, tombstone удалённого аккаунта или
// google-пользователь, чей синтетический номер совпал), номер берётся из
// аллокатора, а telegram_id остаётся telegram-овским.
//
// Найденному пользователю _id НИКОГДА не меняется — инвариант плана.
func (r MongoUserRepository) UpsertTelegramUser(ctx context.Context, tgID int, username, displayName, userLang string) (*api.User, error) {
	var lastErr error
	for attempt := 0; attempt < upsertTelegramAttempts; attempt++ {
		// Поиск по личности идёт ПЕРВЫМ шагом каждой итерации, а не только
		// первой: на duplicate key это ровно та проверка, которая разрешает
		// гонку двух апдейтов одного нового пользователя — проигравший
		// подбирает документ, созданный победителем. Ветка «занят → сразу
		// аллокатор» вместо этого вставила бы второго пользователя с тем же
		// telegram_id, получила E11000 уже по unique-индексу и потеряла апдейт
		existing, err := r.FindByTelegramID(ctx, tgID)
		if err == nil {
			refreshed, refreshErr := r.refreshTelegramProfile(ctx, existing, username, displayName, userLang)
			if refreshErr == nil {
				return refreshed, nil
			}
			if refreshErr != mongo.ErrNoDocuments {
				return nil, refreshErr
			}
			// пользователя удалили между поиском и записью: tombstone освободил
			// telegram_id, и следующая итерация заведёт человеку новый аккаунт
			lastErr = refreshErr
			continue
		}
		if err != mongo.ErrNoDocuments {
			return nil, err
		}

		id := tgID
		occupied, err := r.idOccupied(ctx, tgID)
		if err != nil {
			return nil, err
		}
		if occupied {
			if id, err = r.NextUserID(ctx); err != nil {
				return nil, err
			}
		}

		tg := tgID
		err = r.CreateIdentityUser(ctx, api.User{
			ID:          id,
			TelegramID:  &tg,
			Username:    username,
			DisplayName: displayName,
			UserLang:    userLang,
		})
		if err == nil {
			return r.FindById(ctx, id)
		}
		if !IsDuplicateKey(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, errors.Wrapf(lastErr, "не удалось создать telegram-пользователя %d за %d попыток", tgID, upsertTelegramAttempts)
}

// idOccupied — существует ли документ с таким _id. Удалённые (tombstone) тоже
// считаются занявшими номер: документ никуда не делся и вставка по нему упадёт
func (r MongoUserRepository) idOccupied(ctx context.Context, id int) (bool, error) {
	_, err := r.FindById(ctx, id)
	if err == nil {
		return true, nil
	}
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	return false, err
}

// refreshTelegramProfile подтягивает изменившийся telegram-профиль найденного
// пользователя. Пишется только то, что реально поменялось:
//   - user_name обновляется всегда (в telegram его можно снять, и пустое
//     значение — тоже значение);
//   - display_name — только непустое: вход через Login Widget без фамилии и
//     имени не должен затирать имя, известное боту;
//   - user_lang — только когда в базе пусто. Заполненный язык принадлежит
//     пользователю (он мог выбрать его руками), апдейты бота его не трогают
//
// ⚠️ Запись идёт через updateLiveUser (фильтр по deleted_at), а не по одному
// _id. Гонка, ради которой это критично: апдейт из Telegram нашёл живого
// пользователя, параллельно прошёл DELETE /me, вычистил user_name/display_name
// и поставил tombstone — и запись по одному _id вернула бы на tombstone
// НАСТОЯЩЕЕ ИМЯ И USERNAME человека уже после чистки PII.
// Не совпало — mongo.ErrNoDocuments: вызывающий (UpsertTelegramUser) на этом
// сигнале уходит на следующую итерацию и заводит человеку новый аккаунт.
// По той же причине перечитывание идёт по живому документу
func (r MongoUserRepository) refreshTelegramProfile(ctx context.Context, u *api.User, username, displayName, userLang string) (*api.User, error) {
	set := bson.M{}
	if username != u.Username {
		set["user_name"] = username
	}
	if strings.TrimSpace(displayName) != "" && displayName != u.DisplayName {
		set["display_name"] = displayName
	}
	if userLang != "" && u.UserLang == "" {
		set["user_lang"] = userLang
	}
	if len(set) == 0 {
		return u, nil
	}
	updated, err := r.updateLiveUser(ctx, u.ID, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, mongo.ErrNoDocuments
	}
	return r.findOne(ctx, append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: u.ID}}}}, notDeleted...))
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
		{
			Keys:    bson.D{{Key: "login_email", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("uniq_login_email"),
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
//
// Только по живому документу: aliases входит в snapshotPIIFields и вычищается
// при удалении аккаунта, а писать сюда может ЛЮБОЙ участник общей комнаты
// (POST /users/{id}/aliases) — то есть чужой запрос дописал бы прозвище
// удалённого человека обратно на его tombstone
func (r MongoUserRepository) AddAlias(ctx context.Context, userId int, alias string) error {
	updated, err := r.updateLiveUser(ctx, userId, bson.D{{Key: "$addToSet", Value: bson.M{"aliases": alias}}})
	if err != nil {
		return err
	}
	// целевого пользователя нет (или он удалён) — не молчаливый no-op, а явная
	// ошибка: хендлер обязан ответить 404, а не 204 «сохранили»
	if !updated {
		return mongo.ErrNoDocuments
	}
	return nil
}

// AddPushToken регистрирует FCM-токен устройства. Идемпотентно и устойчиво к
// гонке: в документе ОДНОГО пользователя токен лежит ровно одной записью.
//
// Токен принадлежит УСТАНОВКЕ приложения, а не сессии, поэтому регистрация
// снимает его со всех прочих аккаунтов. Иначе так: Маша вышла, а запрос на
// отвязку не дошёл (нет сети, приложение убили); телефоном пользуется Петя, и
// пуши Маши — названия её групп и суммы расходов — приходят ему. Само это не
// рассасывалось: телефон живой, доставка успешна, и отбраковка по UNREGISTERED
// (см. push.Worker) не срабатывает, потому что токен не мёртв.
//
// Снимаем ДО записи себе: если наш собственный upsert не удастся, лучше пусть
// пуш не придёт никому, чем придёт не тому.
//
// Узкое место остаётся одно: запрос регистрации прежнего аккаунта, ушедший до
// выхода и доехавший после нашего, вернёт токен обратно ему. Окно — время
// одного запроса.
//
// Было «$pull, потом $push». Два одновременных запроса с одним токеном (а они
// бывают: вход и колбэк FCM дёргают регистрацию разом, и клиентский дедуп
// пропускает оба, пока ни один не ответил) успевали сделать pull-pull-push-push
// и оставляли токен ДВАЖДЫ. С языками это стало заметно: один телефон получал
// два пуша, и на разных языках.
//
// Теперь запись, которая уже есть, правится на месте, а вставка идёт под
// фильтром «такого токена нет» — второй запрос просто не совпадёт.
//
// Обе записи — по живому документу: push_tokens входит в snapshotPIIFields, и
// токен, дописанный на tombstone, вернул бы удалённому аккаунту адрес доставки
// пушей
func (r MongoUserRepository) AddPushToken(ctx context.Context, userId int, token api.PushToken) error {
	if _, err := r.col.UpdateMany(ctx,
		bson.D{
			{Key: "_id", Value: bson.D{{Key: "$ne", Value: userId}}},
			{Key: "push_tokens.token", Value: token.Token},
		},
		bson.M{"$pull": bson.M{"push_tokens": bson.M{"token": token.Token}}},
	); err != nil {
		return err
	}

	live := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...)

	updated, err := r.col.UpdateOne(ctx,
		append(live, bson.E{Key: "push_tokens.token", Value: token.Token}),
		bson.M{"$set": bson.M{
			"push_tokens.$[t].platform": token.Platform,
			"push_tokens.$[t].locale":   token.Locale,
		}},
		options.Update().SetArrayFilters(options.ArrayFilters{
			Filters: []interface{}{bson.M{"t.token": token.Token}},
		}),
	)
	if err != nil {
		return err
	}
	if updated.MatchedCount > 0 {
		return nil
	}

	inserted, err := r.col.UpdateOne(ctx,
		append(live, bson.E{Key: "push_tokens.token", Value: bson.M{"$ne": token.Token}}),
		bson.M{"$push": bson.M{"push_tokens": token}},
	)
	if err != nil {
		return err
	}
	if inserted.MatchedCount > 0 {
		return nil
	}
	// Не совпало ничего: либо аккаунт удалён, либо токен успел добавить
	// параллельный запрос между нашими двумя операциями. Второе не ошибка.
	exists, err := r.col.CountDocuments(ctx, live, options.Count().SetLimit(1))
	if err != nil {
		return err
	}
	if exists == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RemovePushToken убирает токен (logout или отбраковка FCM). Отсутствие токена —
// не ошибка (idempotent): 404 только если самого пользователя нет.
//
// Фильтра notDeleted здесь намеренно НЕТ: это единственная запись в user,
// которая только УБИРАЕТ данные. Отбраковка мёртвого токена приходит из
// воркера пушей уже после удаления аккаунта, и отказать ей — значит оставить
// токен в базе ровно в том случае, ради которого метод и нужен
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

// ErrUserDeleted — запись отвергнута, потому что аккаунт удалён (tombstone).
// Отличается от mongo.ErrNoDocuments намеренно: «пользователя никогда не было»
// и «пользователь удалён, воскрешать его нельзя» — разные ответы клиенту
var ErrUserDeleted = errors.New("пользователь удалён")

// updateLiveUser — единственная точка записи в профиль ЖИВОГО пользователя:
// фильтр {_id, deleted_at: {$exists:false}}, никакого upsert. Возвращает false,
// если документа нет или он — tombstone.
//
// ⚠️ Инвариант удаления аккаунта (Task 13): tombstone НИКОГДА не получает PII
// обратно. Любой новый мутатор профиля обязан идти через этот метод, а не
// писать UpdateOne по одному _id: запрос, впущенный auth-middleware за миг до
// DELETE /me, иначе дописал бы имя, username, язык, настройки уведомлений или
// банковские реквизиты на уже удалённый аккаунт
func (r MongoUserRepository) updateLiveUser(ctx context.Context, userId int, update interface{}) (bool, error) {
	filter := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: userId}}}}, notDeleted...)
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// UpsertUser создаёт или обновляет профиль ЖИВОГО пользователя.
//
// ⚠️ options.SetUpsert здесь БОЛЬШЕ НЕТ, хотя метод так и называется. Причина —
// tombstone: фильтр `{_id: X}` с upsert записал бы display_name и user_name
// прямо на удалённый аккаунт (PATCH /me, впущенный middleware за миг до
// DELETE /me), а `{_id: X, deleted_at: {$exists:false}}` с upsert был бы ещё
// хуже — фильтр не совпал бы с tombstone и mongo попыталась бы ВСТАВИТЬ второй
// документ с тем же _id, то есть отдала duplicate key вместо отказа.
//
// Поэтому создание разведено с обновлением явно:
//  1. UpdateOne по живому документу — обычный путь (PATCH /me, повторный
//     dev-вход);
//  2. ничего не совпало → InsertOne: пользователя либо нет вовсе (первый
//     dev-вход, единственный оставшийся создающий вызов), либо он удалён;
//  3. duplicate key на вставке → документ есть, но живым не нашёлся: либо гонка
//     двух одновременных первых входов (перепроверяем шагом 1), либо
//     tombstone → ErrUserDeleted
func (r MongoUserRepository) UpsertUser(ctx context.Context, u api.User) (*api.User, error) {
	set := bson.M{"_id": u.ID, "user_lang": u.UserLang, "display_name": u.DisplayName, "user_name": u.Username}
	// Только взводим, никогда не снимаем: снять его может лишь тот же аккаунт,
	// пришедший обычным путём, а этого пути у dev-пользователя нет. Флаг нужен
	// бэкфиллу telegram_id, чтобы не принять dev-аккаунт за исторического
	// telegram-пользователя (см. api.User.DevAuth)
	if u.DevAuth {
		set["dev_auth"] = true
	}
	update := bson.D{{Key: "$set", Value: set}}

	updated, err := r.updateLiveUser(ctx, u.ID, update)
	if err != nil {
		return nil, err
	}
	if !updated {
		// Дата создания только на ВСТАВКЕ, и отдельной копией документа.
		// В $set её класть нельзя: UpsertUser зовётся на каждом сохранении
		// профиля (см. handlePatchMe), и дата переписывалась бы при каждой
		// смене имени — то есть «зарегистрировался» означало бы «последний раз
		// правил профиль».
		insert := bson.M{"created_at": time.Now().UTC()}
		for k, v := range set {
			insert[k] = v
		}
		if _, err = r.col.InsertOne(ctx, insert); err != nil {
			if !IsDuplicateKey(err) {
				return nil, err
			}
			if updated, err = r.updateLiveUser(ctx, u.ID, update); err != nil {
				return nil, err
			}
			if !updated {
				return nil, ErrUserDeleted
			}
		}
	}
	return r.FindById(ctx, u.ID)
}

// setLiveUserField — общий путь настроек профиля: $set по живому документу.
//
// Upsert'а здесь больше нет (был сразу у пяти методов). Он не только воскрешал
// бы удалённого пользователя, но и заводил документ на любой незнакомый id — а
// все вызывающие работают с уже существующим пользователем (бот резолвит его
// через UpsertTelegramUser, REST — через currentUser).
//
// Удалённый пользователь — тихий no-op, а не ошибка: настройки tombstone менять
// некому и незачем, а 500 в ответ на гонку с собственным DELETE /me
// пользователю ничего не объясняет
func (r MongoUserRepository) setLiveUserField(ctx context.Context, userId int, field string, value interface{}) error {
	_, err := r.updateLiveUser(ctx, userId, bson.D{{Key: "$set", Value: bson.M{field: value}}})
	return err
}

func (r MongoUserRepository) SetUserLang(ctx context.Context, userId int, lang string) error {
	return r.setLiveUserField(ctx, userId, "selected_lang", lang)
}

func (r MongoUserRepository) SetCountInPage(ctx context.Context, userId int, count int) error {
	return r.setLiveUserField(ctx, userId, "count_in_page", count)
}

func (r MongoUserRepository) SetNotifySettings(ctx context.Context, userId int, s api.NotifySettings) error {
	err := r.setLiveUserField(ctx, userId, "notify", s)
	if err != nil {
		log.Error().Err(err).Msg("set notify settings failed")
	}
	return err
}

// SetNotificationsSeenAt двигает отметку просмотра раздела уведомлений.
//
// Только ВПЕРЁД: запоздавший запрос со старым временем (ретрай, второй экран)
// иначе откатил бы отметку назад, и уже прочитанное всплыло бы снова. Поэтому
// условие $lt стоит в фильтре, а не в коде вызывающего.
// RevokeTokens — «выйти на всех устройствах».
//
// Токен живёт 90 дней и не отзывался ничем, кроме смены общего секрета (то есть
// разлогина ВСЕХ). Украденный телефон означал три месяца доступа к чужим
// расходам. Отсечка по дате выпуска решает это точечно, для одного человека.
//
// Push-токены убираются той же записью: иначе уведомления продолжали бы уходить
// на устройство, доступ которому только что закрыли
func (r MongoUserRepository) RevokeTokens(ctx context.Context, userId int, at time.Time) error {
	filter := bson.M{"_id": userId, "deleted_at": bson.M{"$exists": false}}
	update := bson.M{
		"$set":   bson.M{"tokens_valid_from": at},
		"$unset": bson.M{"push_tokens": ""},
	}
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (r MongoUserRepository) SetNotificationsSeenAt(ctx context.Context, userId int, at time.Time) error {
	filter := bson.M{
		"_id":        userId,
		"deleted_at": bson.M{"$exists": false},
		"$or": bson.A{
			bson.M{"notifications_seen_at": bson.M{"$exists": false}},
			bson.M{"notifications_seen_at": bson.M{"$lt": at}},
		},
	}
	_, err := r.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"notifications_seen_at": at}})
	if err != nil {
		log.Error().Err(err).Msg("set notifications seen failed")
	}
	return err
}

// SetRoomSeenAt двигает отметку прочитанного одной комнаты.
//
// Условия те же, что у SetNotificationsSeenAt (tombstone и «только вперёд»), но
// по вложенному ключу rooms_seen_at.<roomId>: откат назад вернул бы на карточку
// группы уже просмотренные события.
//
// roomId обязан быть валидным hex ObjectId — вызывающий проверяет это, находя
// комнату. Точка в ключе иначе увела бы $set на произвольный вложенный путь
// документа пользователя.
func (r MongoUserRepository) SetRoomSeenAt(ctx context.Context, userId int, roomId string, at time.Time) error {
	if _, err := primitive.ObjectIDFromHex(roomId); err != nil {
		return err
	}
	field := "rooms_seen_at." + roomId
	filter := bson.M{
		"_id":        userId,
		"deleted_at": bson.M{"$exists": false},
		"$or": bson.A{
			bson.M{field: bson.M{"$exists": false}},
			bson.M{field: bson.M{"$lt": at}},
		},
	}
	_, err := r.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{field: at}})
	if err != nil {
		log.Error().Err(err).Msg("set room seen failed")
	}
	return err
}

func (r MongoUserRepository) SetNotificationUser(ctx context.Context, userId int, notification bool) error {
	return r.setLiveUserField(ctx, userId, "notification_on", notification)
}

// SetUserBankDetails — bank_details входит в snapshotPIIFields, то есть удаление
// аккаунта его вычищает. Запись строго по живому документу
func (r MongoUserRepository) SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error {
	return r.setLiveUserField(ctx, userId, "bank_details", bankDerails)
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
