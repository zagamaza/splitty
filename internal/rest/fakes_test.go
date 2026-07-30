package rest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// fakeLoginCodeRepo in-memory реализация repository.LoginCodeRepository для тестов
type fakeLoginCodeRepo struct {
	codes map[string]*api.LoginCode
}

func newFakeLoginCodeRepo(codes ...api.LoginCode) *fakeLoginCodeRepo {
	repo := &fakeLoginCodeRepo{codes: map[string]*api.LoginCode{}}
	for _, c := range codes {
		code := c
		repo.codes[c.Code] = &code
	}
	return repo
}

func (f *fakeLoginCodeRepo) SaveLoginCode(_ context.Context, c *api.LoginCode) error {
	code := *c
	f.codes[c.Code] = &code
	return nil
}

// UseLoginCode как атомарная mongo-реализация: живой код ({code, used:false,
// expires_at > now}) помечается использованным, иначе mongo.ErrNoDocuments
func (f *fakeLoginCodeRepo) UseLoginCode(_ context.Context, code string, now time.Time) (*api.LoginCode, error) {
	c, ok := f.codes[code]
	if !ok || c.Used || !c.ExpiresAt.After(now) {
		return nil, mongo.ErrNoDocuments
	}
	c.Used = true
	used := *c
	return &used, nil
}

// fakeUserRepo in-memory реализация repository.UserRepository для тестов
type fakeUserRepo struct {
	users map[int]*api.User
}

func newFakeUserRepo(users ...api.User) *fakeUserRepo {
	repo := &fakeUserRepo{users: map[int]*api.User{}}
	for _, u := range users {
		user := u
		repo.users[u.ID] = &user
	}
	return repo
}

func (f *fakeUserRepo) UpsertUser(_ context.Context, u api.User) (*api.User, error) {
	existing, ok := f.users[u.ID]
	if !ok {
		user := u
		f.users[u.ID] = &user
	} else {
		// как в mongo-реализации: $set только _id, user_lang, display_name, user_name
		existing.UserLang = u.UserLang
		existing.DisplayName = u.DisplayName
		existing.Username = u.Username
	}
	user := *f.users[u.ID]
	return &user, nil
}

// CreateIdentityUser как mongo-реализация: чистая вставка, занятый _id или
// занятая личность — duplicate key (в тестах достаточно самого факта ошибки)
func (f *fakeUserRepo) CreateIdentityUser(_ context.Context, u api.User) error {
	if _, ok := f.users[u.ID]; ok {
		return errDuplicateKey
	}
	for _, existing := range f.users {
		if u.TelegramID != nil && existing.TelegramID != nil && *u.TelegramID == *existing.TelegramID {
			return errDuplicateKey
		}
		if u.GoogleSub != "" && existing.GoogleSub == u.GoogleSub {
			return errDuplicateKey
		}
		if u.AppleSub != "" && existing.AppleSub == u.AppleSub {
			return errDuplicateKey
		}
	}
	user := u
	f.users[u.ID] = &user
	return nil
}

// errDuplicateKey имитирует E11000 unique-индекса
var errDuplicateKey = errors.New("E11000 duplicate key error")

func (f *fakeUserRepo) FindByTelegramID(_ context.Context, tgID int) (*api.User, error) {
	return f.findLive(func(u *api.User) bool { return u.TelegramID != nil && *u.TelegramID == tgID })
}

func (f *fakeUserRepo) FindByGoogleSub(_ context.Context, sub string) (*api.User, error) {
	return f.findLive(func(u *api.User) bool { return u.GoogleSub != "" && u.GoogleSub == sub })
}

func (f *fakeUserRepo) FindByAppleSub(_ context.Context, sub string) (*api.User, error) {
	return f.findLive(func(u *api.User) bool { return u.AppleSub != "" && u.AppleSub == sub })
}

// findLive — поиск по личности: удалённые (tombstone) не находятся, как и в
// mongo-реализации с фильтром deleted_at: {$exists: false}
func (f *fakeUserRepo) findLive(match func(*api.User) bool) (*api.User, error) {
	for _, u := range f.users {
		if u.IsDeleted() {
			continue
		}
		if match(u) {
			copied := *u
			return &copied, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeUserRepo) FindByUsername(_ context.Context, username string) (*api.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			copied := *u
			return &copied, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeUserRepo) SetNotifySettings(_ context.Context, userId int, s api.NotifySettings) error {
	if u, ok := f.users[userId]; ok {
		settings := s
		u.Notify = &settings
	}
	return nil
}

func (f *fakeUserRepo) SetUserLang(_ context.Context, userId int, lang string) error {
	if u, ok := f.users[userId]; ok {
		u.SelectedLang = lang
	}
	return nil
}

func (f *fakeUserRepo) SetNotificationUser(_ context.Context, userId int, notification bool) error {
	if u, ok := f.users[userId]; ok {
		u.NotificationOn = &notification
	}
	return nil
}

func (f *fakeUserRepo) SetUserBankDetails(_ context.Context, userId int, bankDetails string) error {
	if u, ok := f.users[userId]; ok {
		u.BankDetails = bankDetails
	}
	return nil
}

func (f *fakeUserRepo) SetCountInPage(_ context.Context, userId int, count int) error {
	if u, ok := f.users[userId]; ok {
		u.CountInPage = count
	}
	return nil
}

func (f *fakeUserRepo) FindById(_ context.Context, id int) (*api.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	user := *u
	return &user, nil
}

func (f *fakeUserRepo) FindByIds(_ context.Context, ids []int) ([]api.User, error) {
	var out []api.User
	for _, id := range ids {
		if u, ok := f.users[id]; ok {
			out = append(out, *u)
		}
	}
	return out, nil
}

func (f *fakeUserRepo) AddPushToken(_ context.Context, userId int, token api.PushToken) error {
	u, ok := f.users[userId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	for i, t := range u.PushTokens {
		if t.Token == token.Token {
			u.PushTokens[i] = token
			return nil
		}
	}
	u.PushTokens = append(u.PushTokens, token)
	return nil
}

func (f *fakeUserRepo) RemovePushToken(_ context.Context, userId int, token string) error {
	u, ok := f.users[userId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	kept := u.PushTokens[:0]
	for _, t := range u.PushTokens {
		if t.Token != token {
			kept = append(kept, t)
		}
	}
	u.PushTokens = kept
	return nil
}

func (f *fakeUserRepo) AddAlias(_ context.Context, userId int, alias string) error {
	u, ok := f.users[userId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	for _, a := range u.Aliases {
		if a == alias {
			return nil
		}
	}
	u.Aliases = append(u.Aliases, alias)
	return nil
}

// fakeRoomRepo in-memory реализация repository.RoomRepository для тестов
type fakeRoomRepo struct {
	rooms map[string]*api.Room
	// afterCreate вызывается после успешного CreateOperation —
	// позволяет симулировать конкурентную запись между вставкой и перепроверкой
	afterCreate func(roomId string)
	// beforeCreateIfAbsent вызывается в начале CreateOperationIfAbsent, до
	// атомарной проверки+вставки — симулирует конкурентный запрос, успевший
	// вставить операцию с тем же client_op_id раньше нас
	beforeCreateIfAbsent func(roomId string)
}

func newFakeRoomRepo(rooms ...*api.Room) *fakeRoomRepo {
	repo := &fakeRoomRepo{rooms: map[string]*api.Room{}}
	for _, r := range rooms {
		repo.rooms[r.ID.Hex()] = r
	}
	return repo
}

func (f *fakeRoomRepo) FindById(_ context.Context, id string) (*api.Room, error) {
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return nil, err
	}
	room, ok := f.rooms[id]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return room, nil
}

func (f *fakeRoomRepo) JoinToRoom(_ context.Context, u api.User, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if isRoomMember(room, u.ID) {
		return nil
	}
	members := append(roomMembers(room), u)
	room.Members = &members
	return nil
}

func (f *fakeRoomRepo) LeaveRoom(_ context.Context, userId int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	var members []api.User
	for _, m := range roomMembers(room) {
		if m.ID != userId {
			members = append(members, m)
		}
	}
	room.Members = &members
	return nil
}

func (f *fakeRoomRepo) SaveRoom(_ context.Context, r *api.Room) (primitive.ObjectID, error) {
	id := primitive.NewObjectID()
	r.ID = id
	f.rooms[id.Hex()] = r
	return id, nil
}

func (f *fakeRoomRepo) FindRoomsByUserId(_ context.Context, id int) (*[]api.Room, error) {
	return f.findRooms(id, false), nil
}

func (f *fakeRoomRepo) FindArchivedRoomsByUserId(_ context.Context, id int) (*[]api.Room, error) {
	return f.findRooms(id, true), nil
}

func (f *fakeRoomRepo) findRooms(userId int, archived bool) *[]api.Room {
	var rooms []api.Room
	for _, r := range f.rooms {
		if isRoomMember(r, userId) && isRoomArchived(r, userId) == archived {
			rooms = append(rooms, *r)
		}
	}
	return &rooms
}

func (f *fakeRoomRepo) FindRoomsByLikeName(_ context.Context, userId int, name string) (*[]api.Room, error) {
	var rooms []api.Room
	for _, r := range f.rooms {
		if isRoomMember(r, userId) && strings.Contains(r.Name, name) {
			rooms = append(rooms, *r)
		}
	}
	return &rooms, nil
}

// SetNotificationSent как mongo-реализация: точечный $set одного поля,
// остальные поля операции не трогает
func (f *fakeRoomRepo) SetNotificationSent(_ context.Context, roomId string, operationId primitive.ObjectID, sent []int) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	ops := roomOperations(room)
	for i := range ops {
		if ops[i].ID == operationId {
			ops[i].NotificationSent = sent
			room.Operations = &ops
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// UpdateOperation как атомарная mongo-реализация: заменяет операцию по _id,
// mongo.ErrNoDocuments если комнаты или операции нет
func (f *fakeRoomRepo) UpdateOperation(_ context.Context, o *api.Operation, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	ops := roomOperations(room)
	for i := range ops {
		if ops[i].ID == o.ID {
			ops[i] = *o
			room.Operations = &ops
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// CreateOperation как mongo-реализация: $push новой операции,
// mongo.ErrNoDocuments если комнаты нет
func (f *fakeRoomRepo) CreateOperation(_ context.Context, o *api.Operation, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	ops := append(roomOperations(room), *o)
	room.Operations = &ops
	if f.afterCreate != nil {
		f.afterCreate(roomId)
	}
	return nil
}

// CreateOperationIfAbsent как атомарная mongo-реализация (UpdateOne с фильтром
// {_id, "operations.client_op_id": {$ne: id}}): проверка дубля и $push — одно
// действие; дубль по client_op_id — (false, nil), нет комнаты — mongo.ErrNoDocuments
func (f *fakeRoomRepo) CreateOperationIfAbsent(_ context.Context, o *api.Operation, roomId string) (bool, error) {
	if hook := f.beforeCreateIfAbsent; hook != nil {
		f.beforeCreateIfAbsent = nil
		hook(roomId)
	}
	room, ok := f.rooms[roomId]
	if !ok {
		return false, mongo.ErrNoDocuments
	}
	for _, op := range roomOperations(room) {
		if op.ClientOpId != "" && op.ClientOpId == o.ClientOpId {
			return false, nil
		}
	}
	ops := append(roomOperations(room), *o)
	room.Operations = &ops
	return true, nil
}

func (f *fakeRoomRepo) DeleteOperation(_ context.Context, roomId string, operationId primitive.ObjectID) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	ops := make([]api.Operation, 0)
	for _, op := range roomOperations(room) {
		if op.ID != operationId {
			ops = append(ops, op)
		}
	}
	room.Operations = &ops
	return nil
}

func (f *fakeRoomRepo) ArchiveRoom(_ context.Context, userId int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if !isRoomArchived(room, userId) {
		room.RoomStates.Archived = append(room.RoomStates.Archived, userId)
	}
	return nil
}

func (f *fakeRoomRepo) UnArchiveRoom(_ context.Context, userId int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	var archived []int
	for _, id := range room.RoomStates.Archived {
		if id != userId {
			archived = append(archived, id)
		}
	}
	room.RoomStates.Archived = archived
	return nil
}

func (f *fakeRoomRepo) FinishedAddOperation(_ context.Context, userId int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	room.RoomStates.FinishedAddOperation = append(room.RoomStates.FinishedAddOperation, userId)
	return nil
}

func (f *fakeRoomRepo) UnFinishedAddOperation(_ context.Context, userId int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	var finished []int
	for _, id := range room.RoomStates.FinishedAddOperation {
		if id != userId {
			finished = append(finished, id)
		}
	}
	room.RoomStates.FinishedAddOperation = finished
	return nil
}

func (f *fakeRoomRepo) PaidOfDebts(_ context.Context, userIds []int, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	room.RoomStates.PaidOffDebt = userIds
	return nil
}

func (f *fakeRoomRepo) UpdateCurrency(_ context.Context, roomId string, currency string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	room.Currency = currency
	return nil
}

// notifierCall одно зафиксированное уведомление fakeNotifier
type notifierCall struct {
	event  string // created | updated | deleted | repayment
	roomId string
	op     api.Operation
	oldOp  api.Operation // только для updated
	author api.User
}

// fakeNotifier реализация Notifier для тестов: пишет вызовы в буферизованный
// канал, потому что сервер уведомляет из фоновой горутины (см. notifyAsync)
type fakeNotifier struct {
	calls chan notifierCall
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{calls: make(chan notifierCall, 16)}
}

func (f *fakeNotifier) NotifyOperationCreated(_ context.Context, room api.Room, op api.Operation, author api.User) {
	f.calls <- notifierCall{event: "created", roomId: room.ID.Hex(), op: op, author: author}
}

func (f *fakeNotifier) NotifyOperationUpdated(_ context.Context, room api.Room, oldOp api.Operation, newOp api.Operation, author api.User) {
	f.calls <- notifierCall{event: "updated", roomId: room.ID.Hex(), op: newOp, oldOp: oldOp, author: author}
}

func (f *fakeNotifier) NotifyOperationDeleted(_ context.Context, room api.Room, op api.Operation, author api.User) {
	f.calls <- notifierCall{event: "deleted", roomId: room.ID.Hex(), op: op, author: author}
}

func (f *fakeNotifier) NotifyRepaymentCreated(_ context.Context, room api.Room, op api.Operation, author api.User) {
	f.calls <- notifierCall{event: "repayment", roomId: room.ID.Hex(), op: op, author: author}
}
