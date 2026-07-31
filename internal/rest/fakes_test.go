package rest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/oidc"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/golang-jwt/jwt/v5"
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

// DeleteByUserId как mongo-реализация: DeleteMany по user_id
func (f *fakeLoginCodeRepo) DeleteByUserId(_ context.Context, userId int) error {
	for code, c := range f.codes {
		if c.UserId == userId {
			delete(f.codes, code)
		}
	}
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

// fakeUserIDAllocator in-memory реализация userIDAllocator: раздаёт номера с
// того же значения, что и настоящий аллокатор (repository.firstSyntheticUserID
// == 10^12), но без mongo. Тесты входа через Google/Apple проверяют, что
// созданному пользователю достался синтетический номер, поэтому стартовое
// значение обязано совпадать с боевым
type fakeUserIDAllocator struct {
	next int
	err  error
}

func newFakeUserIDAllocator() *fakeUserIDAllocator {
	return &fakeUserIDAllocator{next: 1_000_000_000_000}
}

func (f *fakeUserIDAllocator) NextUserID(context.Context) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	id := f.next
	f.next++
	return id, nil
}

// fakeOIDCVerifier — oidc.Verifier без сети и без ключей: знакомый токен
// отдаёт заранее заданные claims, любой другой — ошибку. Тесты входа через
// провайдеров проверяют поведение хендлера, а не разбор JWT (он покрыт в
// internal/oidc)
type fakeOIDCVerifier struct {
	tokens map[string]*oidc.Claims
	calls  int
}

func newFakeVerifier() *fakeOIDCVerifier {
	return &fakeOIDCVerifier{tokens: map[string]*oidc.Claims{}}
}

// with регистрирует валидный токен с claims провайдера
func (f *fakeOIDCVerifier) with(idToken, sub, email, name string) *fakeOIDCVerifier {
	f.tokens[idToken] = &oidc.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Email:            email,
		Name:             name,
	}
	return f
}

// withNonce проставляет claim nonce уже зарегистрированному токену: Apple
// кладёт туда хеш сырого nonce, присланного клиентом
func (f *fakeOIDCVerifier) withNonce(idToken, nonce string) *fakeOIDCVerifier {
	if claims, ok := f.tokens[idToken]; ok {
		claims.Nonce = nonce
	}
	return f
}

// fakeAppleTokens — oidc.AppleTokens без сети: отдаёт заданный refresh token
// либо заданную ошибку и запоминает, с какими кодами и токенами его звали
type fakeAppleTokens struct {
	refreshToken string
	err          error
	codes        []string
	// revoked — токены, для которых вызывали RevokeToken; revokeErr имитирует
	// недоступность Apple при удалении аккаунта
	revoked   []string
	revokeErr error
}

func (f *fakeAppleTokens) ExchangeCode(_ context.Context, code string) (string, error) {
	f.codes = append(f.codes, code)
	if f.err != nil {
		return "", f.err
	}
	return f.refreshToken, nil
}

func (f *fakeAppleTokens) RevokeToken(_ context.Context, refreshToken string) error {
	f.revoked = append(f.revoked, refreshToken)
	return f.revokeErr
}

func (f *fakeOIDCVerifier) Verify(_ context.Context, idToken string) (*oidc.Claims, error) {
	f.calls++
	claims, ok := f.tokens[idToken]
	if !ok {
		return nil, errors.New("подпись не проверена")
	}
	return claims, nil
}

// fakeUserRepo in-memory реализация repository.UserRepository для тестов
type fakeUserRepo struct {
	users map[int]*api.User
	// alloc — собственный аллокатор номеров: настоящий MongoUserRepository тоже
	// владеет им сам, потому что UpsertTelegramUser вызывается из графа бота
	alloc *fakeUserIDAllocator
	// findErr имитирует НЕДОСТУПНУЮ базу (не ErrNoDocuments, а транспортную
	// ошибку). Без этого шва невозможно проверить главное решение
	// accountAlive: ошибка чтения обязана давать 500, а не пропускать запрос —
	// иначе лежащая mongo превращается в обход инвалидации токена
	findErr error
	// softDeleteErr имитирует падение НА САМОМ tombstone. Нужен, чтобы отличить
	// сбой ДО него (аккаунт цел — клиент обязан сохранить сессию и очередь
	// офлайн-расходов) от сбоя ПОСЛЕ (аккаунт удалён, чистка не доделана):
	// снаружи это два одинаковых 500, различает их только код ошибки
	softDeleteErr error
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

// errDuplicateKey имитирует E11000 unique-индекса. Именно WriteException
// драйвера, а не errors.New: боевой код узнаёт дубликат через
// repository.IsDuplicateKey, и на плоской ошибке retry входа через
// Google/Apple в тестах молча превращался бы в 500
var errDuplicateKey error = mongo.WriteException{
	WriteErrors: []mongo.WriteError{{Code: 11000, Message: "E11000 duplicate key error"}},
}

// UpsertTelegramUser повторяет логику mongo-реализации: поиск по telegram_id,
// иначе создание с _id == telegram id, а при занятом _id — с синтетическим
// номером из аллокатора. user_lang проставляется только когда в базе пусто
func (f *fakeUserRepo) UpsertTelegramUser(ctx context.Context, tgID int, username, displayName, userLang string) (*api.User, error) {
	existing, err := f.FindByTelegramID(ctx, tgID)
	if err == nil {
		stored := f.users[existing.ID]
		stored.Username = username
		if strings.TrimSpace(displayName) != "" {
			stored.DisplayName = displayName
		}
		if userLang != "" && stored.UserLang == "" {
			stored.UserLang = userLang
		}
		user := *stored
		return &user, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, err
	}

	id := tgID
	if _, occupied := f.users[tgID]; occupied {
		if id, err = f.allocator().NextUserID(ctx); err != nil {
			return nil, err
		}
	}
	tg := tgID
	if err = f.CreateIdentityUser(ctx, api.User{
		ID: id, TelegramID: &tg, Username: username, DisplayName: displayName, UserLang: userLang,
	}); err != nil {
		return nil, err
	}
	return f.FindById(ctx, id)
}

// allocator — ленивый аллокатор фейка: номера обязаны начинаться с того же
// значения, что и боевые (10^12), иначе тест не отличит синтетический номер от
// telegram id
func (f *fakeUserRepo) allocator() *fakeUserIDAllocator {
	if f.alloc == nil {
		f.alloc = newFakeUserIDAllocator()
	}
	return f.alloc
}

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

// UpdateAppleProfile как mongo-реализация: пустые значения не пишутся (Apple
// присылает email и имя только при первом входе), tombstone не трогается
func (f *fakeUserRepo) UpdateAppleProfile(_ context.Context, userId int, email, displayName, refreshToken string) error {
	u, ok := f.users[userId]
	if !ok || u.IsDeleted() {
		return nil
	}
	if email != "" {
		u.Email = email
	}
	if displayName != "" {
		u.DisplayName = displayName
	}
	if refreshToken != "" {
		u.AppleRefreshToken = refreshToken
	}
	return nil
}

// SetIdentity как mongo-реализация: пишет только в ЖИВОЙ существующий документ
// (никакого upsert — иначе гонка с удалением аккаунта посадила бы личность на
// tombstone), а занятая кем-то ещё личность даёт duplicate key unique-индекса.
// Индекс глобален, поэтому занятость проверяется и по удалённым документам
func (f *fakeUserRepo) SetIdentity(_ context.Context, userId int, provider string, value interface{}) error {
	u, ok := f.users[userId]
	if !ok || u.IsDeleted() {
		return mongo.ErrNoDocuments
	}
	for id, other := range f.users {
		if id == userId {
			continue
		}
		switch provider {
		case repository.IdentityTelegram:
			if tg, isInt := value.(int); isInt && other.TelegramID != nil && *other.TelegramID == tg {
				return errDuplicateKey
			}
		case repository.IdentityGoogle:
			if sub, isStr := value.(string); isStr && other.GoogleSub == sub {
				return errDuplicateKey
			}
		case repository.IdentityApple:
			if sub, isStr := value.(string); isStr && other.AppleSub == sub {
				return errDuplicateKey
			}
		}
	}
	switch provider {
	case repository.IdentityTelegram:
		tg, isInt := value.(int)
		if !isInt {
			return errors.New("telegram_id должен быть int")
		}
		u.TelegramID = &tg
	case repository.IdentityGoogle:
		sub, isStr := value.(string)
		if !isStr {
			return errors.New("google_sub должен быть строкой")
		}
		u.GoogleSub = sub
	case repository.IdentityApple:
		sub, isStr := value.(string)
		if !isStr {
			return errors.New("apple_sub должен быть строкой")
		}
		u.AppleSub = sub
	default:
		return errors.New("неизвестный способ входа")
	}
	return nil
}

// ClearIdentity как mongo-реализация: $unset по живому документу
func (f *fakeUserRepo) ClearIdentity(_ context.Context, userId int, provider string) error {
	u, ok := f.users[userId]
	if !ok || u.IsDeleted() {
		return mongo.ErrNoDocuments
	}
	switch provider {
	case repository.IdentityTelegram:
		u.TelegramID = nil
	case repository.IdentityGoogle:
		u.GoogleSub = ""
	case repository.IdentityApple:
		u.AppleSub = ""
		// как MongoUserRepository.ClearIdentity: refresh token принадлежит
		// отвязываемой личности и уходит вместе с ней (identityExtraFields)
		u.AppleRefreshToken = ""
	default:
		return errors.New("неизвестный способ входа")
	}
	return nil
}

// SoftDeleteUser как mongo-реализация: tombstone вместо удаления документа,
// PII и личности вычищаются, display_name заменяется плейсхолдером.
// Идемпотентен — повторный вызов пишет то же самое
func (f *fakeUserRepo) SoftDeleteUser(_ context.Context, userId int) error {
	if f.softDeleteErr != nil {
		return f.softDeleteErr
	}
	u, ok := f.users[userId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	now := time.Now()
	u.DeletedAt = &now
	u.DisplayName = repository.DeletedUserPlaceholder
	u.Username = ""
	u.Email = ""
	u.GoogleSub = ""
	u.AppleSub = ""
	u.TelegramID = nil
	u.AppleRefreshToken = ""
	u.PushTokens = nil
	u.Aliases = nil
	u.BankDetails = ""
	return nil
}

// fakeChatStates — userDataCleaner без mongo: запоминает, по каким id его звали
type fakeChatStates struct {
	deleted []int
	err     error
}

func (f *fakeChatStates) DeleteByUserId(_ context.Context, userId int) error {
	f.deleted = append(f.deleted, userId)
	return f.err
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
	if f.findErr != nil {
		return nil, f.findErr
	}
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
	// anonymizeErr — одноразовый сбой AnonymizeUser: имитирует падение
	// удаления аккаунта ПОСЛЕ tombstone, чтобы проверить, что аккаунт уже
	// недоступен, а повторный вызов доводит анонимизацию до конца
	anonymizeErr error
	// anonymized — id, по которым анонимизация действительно отработала
	anonymized []int
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

// AnonymizeUser как mongo-реализация: во всех встроенных снимках заменяет
// display_name плейсхолдером и вычищает PII, не трогая id, суммы и доли.
// anonymizeErr имитирует сбой посреди удаления аккаунта (одна попытка)
func (f *fakeRoomRepo) AnonymizeUser(_ context.Context, userId int, placeholder string) error {
	if err := f.anonymizeErr; err != nil {
		f.anonymizeErr = nil
		return err
	}
	f.anonymized = append(f.anonymized, userId)
	anonymize := func(u *api.User) {
		if u == nil || u.ID != userId {
			return
		}
		u.DisplayName = placeholder
		u.Username = ""
		u.Email = ""
		u.GoogleSub = ""
		u.AppleSub = ""
		u.TelegramID = nil
		u.PushTokens = nil
		u.Aliases = nil
		u.BankDetails = ""
	}
	for _, room := range f.rooms {
		members := roomMembers(room)
		for i := range members {
			anonymize(&members[i])
		}
		room.Members = &members

		ops := roomOperations(room)
		for i := range ops {
			anonymize(ops[i].Donor)
			if ops[i].Recipients != nil {
				recipients := *ops[i].Recipients
				for j := range recipients {
					anonymize(&recipients[j])
				}
			}
			for j := range ops[i].RecipientsWithSum {
				anonymize(&ops[i].RecipientsWithSum[j].User)
			}
		}
		room.Operations = &ops
	}
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
