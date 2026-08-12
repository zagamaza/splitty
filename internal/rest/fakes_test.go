package rest

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
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

// UpsertUser как mongo-реализация: пишет только в ЖИВОЙ документ, создаёт
// отсутствующего, а на tombstone отвечает repository.ErrUserDeleted (upsert'а в
// настоящем репозитории больше нет — иначе PATCH /me, впущенный за миг до
// DELETE /me, вернул бы имя и username на удалённый аккаунт)
func (f *fakeUserRepo) UpsertUser(_ context.Context, u api.User) (*api.User, error) {
	existing, ok := f.users[u.ID]
	switch {
	case !ok:
		user := u
		f.users[u.ID] = &user
	case existing.IsDeleted():
		return nil, repository.ErrUserDeleted
	default:
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
		if u.LoginEmail != "" && existing.LoginEmail == u.LoginEmail {
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

// FindByLoginEmail как mongo-реализация: адрес нормализуется внутри репозитория
func (f *fakeUserRepo) FindByLoginEmail(_ context.Context, email string) (*api.User, error) {
	normalized := repository.NormalizeLoginEmail(email)
	return f.findLive(func(u *api.User) bool { return u.LoginEmail != "" && u.LoginEmail == normalized })
}

// SetPasswordHash как mongo-реализация: только живой документ, без upsert
func (f *fakeUserRepo) SetPasswordHash(_ context.Context, userId int, hash string) error {
	u, ok := f.liveUser(userId)
	if !ok {
		return mongo.ErrNoDocuments
	}
	u.PasswordHash = hash
	return nil
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
	case repository.IdentityPassword:
		// как identityFields: у пароля личность — хеш, адрес остаётся за аккаунтом
		u.PasswordHash = ""
	default:
		return errors.New("неизвестный способ входа")
	}
	return nil
}

// SoftDeleteUser как mongo-реализация: tombstone вместо удаления документа,
// PII и личности вычищаются, display_name заменяется плейсхолдером.
// Идемпотентен — повторный вызов пишет то же самое.
//
// Список полей обязан совпадать с $unset настоящего репозитория
// (repository.snapshotPIIFields + tombstoneExtraFields): новое PII-поле модели
// добавляется в оба места, иначе тесты удаления аккаунта его не заметят
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
	u.LoginEmail = ""
	u.PasswordHash = ""
	u.GoogleSub = ""
	u.AppleSub = ""
	u.TelegramID = nil
	u.AppleRefreshToken = ""
	u.PushTokens = nil
	u.Aliases = nil
	u.BankDetails = ""
	u.RoomsSeenAt = nil
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

// liveUser — как MongoUserRepository.updateLiveUser: настройки профиля пишутся
// только в живой документ. Удалённый — тихий no-op, upsert'а нет
func (f *fakeUserRepo) liveUser(userId int) (*api.User, bool) {
	u, ok := f.users[userId]
	if !ok || u.IsDeleted() {
		return nil, false
	}
	return u, true
}

func (f *fakeUserRepo) SetNotifySettings(_ context.Context, userId int, s api.NotifySettings) error {
	if u, ok := f.liveUser(userId); ok {
		settings := s
		u.Notify = &settings
	}
	return nil
}

// SetNotificationsSeenAt повторяет семантику mongo: отметка двигается только
// вперёд, иначе запоздавший запрос вернул бы уже прочитанное в непрочитанные.
func (f *fakeUserRepo) SetNotificationsSeenAt(_ context.Context, userId int, at time.Time) error {
	if u, ok := f.liveUser(userId); ok {
		if u.NotificationsSeenAt == nil || u.NotificationsSeenAt.Before(at) {
			t := at
			u.NotificationsSeenAt = &t
		}
	}
	return nil
}

// SetRoomSeenAt повторяет семантику mongo: отметка по комнате двигается только
// вперёд и только на живом аккаунте.
func (f *fakeUserRepo) SetRoomSeenAt(_ context.Context, userId int, roomId string, at time.Time) error {
	u, ok := f.liveUser(userId)
	if !ok {
		return nil
	}
	if prev, seen := u.RoomsSeenAt[roomId]; seen && !prev.Before(at) {
		return nil
	}
	if u.RoomsSeenAt == nil {
		u.RoomsSeenAt = map[string]time.Time{}
	}
	u.RoomsSeenAt[roomId] = at
	return nil
}

func (f *fakeUserRepo) SetUserLang(_ context.Context, userId int, lang string) error {
	if u, ok := f.liveUser(userId); ok {
		u.SelectedLang = lang
	}
	return nil
}

func (f *fakeUserRepo) SetNotificationUser(_ context.Context, userId int, notification bool) error {
	if u, ok := f.liveUser(userId); ok {
		u.NotificationOn = &notification
	}
	return nil
}

func (f *fakeUserRepo) SetUserBankDetails(_ context.Context, userId int, bankDetails string) error {
	if u, ok := f.liveUser(userId); ok {
		u.BankDetails = bankDetails
	}
	return nil
}

func (f *fakeUserRepo) SetCountInPage(_ context.Context, userId int, count int) error {
	if u, ok := f.liveUser(userId); ok {
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

// AddPushToken как mongo-реализация: push_tokens входит в snapshotPIIFields,
// поэтому токен на tombstone не сядет — mongo.ErrNoDocuments
func (f *fakeUserRepo) AddPushToken(_ context.Context, userId int, token api.PushToken) error {
	u, ok := f.liveUser(userId)
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

// RevokeTokens как боевая: ставит отсечку и снимает push-токены всех устройств
func (f *fakeUserRepo) RevokeTokens(_ context.Context, userId int, at time.Time) error {
	u, ok := f.users[userId]
	if !ok || u.IsDeleted() {
		return mongo.ErrNoDocuments
	}
	moment := at
	u.TokensValidFrom = &moment
	u.PushTokens = nil
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

// AddAlias как mongo-реализация: aliases входит в snapshotPIIFields, и чужой
// запрос не должен возвращать прозвище удалённого человека на его tombstone
func (f *fakeUserRepo) AddAlias(_ context.Context, userId int, alias string) error {
	u, ok := f.liveUser(userId)
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
	// beforeDelete вызывается в начале DeleteOperation — симулирует конкурента,
	// успевшего удалить ту же операцию между чтением комнаты и записью
	beforeDelete func(roomId string, operationId primitive.ObjectID)
	// anonymizeErr — одноразовый сбой AnonymizeUser: имитирует падение
	// удаления аккаунта ПОСЛЕ tombstone, чтобы проверить, что аккаунт уже
	// недоступен, а повторный вызов доводит анонимизацию до конца
	anonymizeErr error
	// anonymized — id, по которым анонимизация действительно отработала
	anonymized []int
	// onFindRooms вызывается в начале FindRoomsByUserId — позволяет засечь
	// момент чтения ленты относительно остальных шагов хендлера
	onFindRooms func()
	// beforeLeave вызывается в начале LeaveRoom, до условий членства и расходов —
	// симулирует запись, легшую между проверками хендлера и самим выходом
	beforeLeave func(roomId string)
	// beforeCreate вызывается в начале CreateOperation, до условия на состав —
	// симулирует выход участника между валидацией хендлера и вставкой расхода
	beforeCreate func(roomId string)
	// joinErr — сбой JoinToRoom: проверяем, что принятое приглашение, не
	// доведённое до членства, не запирает человека в статусе added
	joinErr error
	// afterFindById вызывается ПОСЛЕ чтения комнаты и сбрасывает сам себя, то
	// есть срабатывает один раз: так симулируется чужой запрос, легший между
	// двумя чтениями комнаты в одном хендлере
	afterFindById func(roomId string)
	// findByIdCalls — сколько раз читали комнату поимённо. Списку групп такие
	// чтения запрещены: он обязан считаться по уже загруженным документам
	findByIdCalls int
}

func newFakeRoomRepo(rooms ...*api.Room) *fakeRoomRepo {
	repo := &fakeRoomRepo{rooms: map[string]*api.Room{}}
	for _, r := range rooms {
		repo.rooms[r.ID.Hex()] = r
	}
	return repo
}

// delete — «комнату успели удалить»: нужно тестам, где приглашение или
// карточка ссылаются на исчезнувшую комнату.
func (f *fakeRoomRepo) delete(roomId string) {
	delete(f.rooms, roomId)
}

// FindById отдаёт СНИМОК комнаты, а не живой указатель: mongo возвращает
// раскодированную копию, и хендлер, читающий комнату дважды, обязан видеть
// разное состояние. С живым указателем «устаревший снимок» был бы невоспроизводим,
// а именно на нём строятся гонки примирения записи приглашения.
func (f *fakeRoomRepo) FindById(_ context.Context, id string) (*api.Room, error) {
	f.findByIdCalls++
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return nil, err
	}
	room, ok := f.rooms[id]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	snapshot := snapshotRoom(room)
	if hook := f.afterFindById; hook != nil {
		f.afterFindById = nil
		hook(id)
	}
	return snapshot, nil
}

// snapshotRoom копирует комнату вместе с участниками и операциями.
func snapshotRoom(r *api.Room) *api.Room {
	c := *r
	if r.Members != nil {
		members := append([]api.User(nil), *r.Members...)
		c.Members = &members
	}
	if r.Operations != nil {
		ops := append([]api.Operation(nil), *r.Operations...)
		c.Operations = &ops
	}
	return &c
}

func (f *fakeRoomRepo) JoinToRoom(_ context.Context, u api.User, roomId string) error {
	if f.joinErr != nil {
		return f.joinErr
	}
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

// LeaveRoom повторяет семантику mongo-реализации: false, если пользователя в
// комнате не было ИЛИ на нём висит действующий расход (оба условия стоят в
// фильтре, а не в отдельном чтении), плюс чистка room_states — иначе
// вернувшийся увидел бы комнату «в архиве».
func (f *fakeRoomRepo) LeaveRoom(_ context.Context, userId int, roomId string) (bool, error) {
	if f.beforeLeave != nil {
		f.beforeLeave(roomId)
	}
	room, ok := f.rooms[roomId]
	if !ok {
		return false, mongo.ErrNoDocuments
	}
	// Условие расходов в фильтре mongo уже правила api.HasOperations (см.
	// activeOperationOf), но всё, что ловит фильтр, ловит и оно: для фейка этой
	// стороны достаточно, легаси до записи не доходит — его отсекает хендлер.
	if api.HasOperations(room, userId) {
		return false, nil
	}
	var (
		members []api.User
		found   bool
	)
	for _, m := range roomMembers(room) {
		if m.ID == userId {
			found = true
			continue
		}
		members = append(members, m)
	}
	if !found {
		return false, nil
	}
	room.Members = &members
	room.RoomStates.Archived = withoutInt(room.RoomStates.Archived, userId)
	room.RoomStates.PaidOffDebt = withoutInt(room.RoomStates.PaidOffDebt, userId)
	room.RoomStates.FinishedAddOperation = withoutInt(room.RoomStates.FinishedAddOperation, userId)
	return true, nil
}

func withoutInt(ids []int, drop int) []int {
	var out []int
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func (f *fakeRoomRepo) SaveRoom(_ context.Context, r *api.Room) (primitive.ObjectID, error) {
	id := primitive.NewObjectID()
	r.ID = id
	f.rooms[id.Hex()] = r
	return id, nil
}

func (f *fakeRoomRepo) FindRoomsByUserId(_ context.Context, id int) (*[]api.Room, error) {
	if f.onFindRooms != nil {
		f.onFindRooms()
	}
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
func (f *fakeRoomRepo) UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	return f.updateOperation(ctx, o, roomId, false)
}

func (f *fakeRoomRepo) UpdateOperationIfUnchanged(ctx context.Context, o *api.Operation, roomId string) error {
	return f.updateOperation(ctx, o, roomId, true)
}

// updateOperation повторяет боевую семантику: версия растёт на каждой записи, а
// условная правка не проходит по чужой версии
func (f *fakeRoomRepo) updateOperation(_ context.Context, o *api.Operation, roomId string, conditional bool) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	ops := roomOperations(room)
	for i := range ops {
		if ops[i].ID != o.ID {
			continue
		}
		if conditional && ops[i].Version != o.Version {
			return repository.ErrStaleOperation
		}
		next := *o
		next.Version = o.Version + 1
		ops[i] = next
		room.Operations = &ops
		o.Version = next.Version
		return nil
	}
	return mongo.ErrNoDocuments
}

// ActivateOperation как mongo-реализация: та же замена, что в UpdateOperation,
// но с условием состава — все связываемые расходом люди обязаны быть в комнате
func (f *fakeRoomRepo) ActivateOperation(ctx context.Context, o *api.Operation, roomId string) error {
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if !allParticipantsAreMembers(room, o) {
		return repository.ErrParticipantLeft
	}
	return f.UpdateOperation(ctx, o, roomId)
}

// CreateOperation как mongo-реализация: $push новой операции,
// mongo.ErrNoDocuments если комнаты нет, repository.ErrParticipantLeft если
// кто-то из участников операции успел выйти (условие стоит в фильтре вставки)
func (f *fakeRoomRepo) CreateOperation(_ context.Context, o *api.Operation, roomId string) error {
	if hook := f.beforeCreate; hook != nil {
		f.beforeCreate = nil
		hook(roomId)
	}
	room, ok := f.rooms[roomId]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if !allParticipantsAreMembers(room, o) {
		return repository.ErrParticipantLeft
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
	if !allParticipantsAreMembers(room, o) {
		return false, repository.ErrParticipantLeft
	}
	ops := append(roomOperations(room), *o)
	room.Operations = &ops
	return true, nil
}

// allParticipantsAreMembers — то же условие, что стоит в фильтре вставки
// (repository.membersFilter): плательщик и получатели новой операции обязаны
// быть участниками комнаты В МОМЕНТ ЗАПИСИ, иначе долг ложится на того, кто
// комнату уже не видит.
func allParticipantsAreMembers(room *api.Room, o *api.Operation) bool {
	if o.Donor != nil && !isRoomMember(room, o.Donor.ID) {
		return false
	}
	for _, r := range o.RecipientsWithSum {
		if !isRoomMember(room, r.User.ID) {
			return false
		}
	}
	return true
}

// DeleteOperation повторяет мягкое удаление боевого репозитория: операция
// остаётся в документе со статусом archive. Тесты обязаны видеть ту же
// семантику, иначе они проверяют не то, что работает на проде
func (f *fakeRoomRepo) DeleteOperation(_ context.Context, roomId string, operationId primitive.ObjectID) (bool, error) {
	if f.beforeDelete != nil {
		f.beforeDelete(roomId, operationId)
	}
	room, ok := f.rooms[roomId]
	if !ok {
		return false, mongo.ErrNoDocuments
	}
	ops := roomOperations(room)
	for i := range ops {
		if ops[i].ID != operationId || ops[i].Status == api.StatusArchive {
			continue
		}
		ops[i].Status = api.StatusArchive
		room.Operations = &ops
		return true, nil
	}
	return false, nil
}

func (f *fakeRoomRepo) PurgeOperation(_ context.Context, roomId string, operationId primitive.ObjectID) error {
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
	event  string // created | updated | deleted | repayment | invited
	roomId string
	op     api.Operation
	oldOp  api.Operation // только для updated
	author api.User
	// invitee и isReturn — только для invited
	invitee  api.User
	isReturn bool
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

func (f *fakeNotifier) NotifyInvited(_ context.Context, room api.Room, invitee api.User, inviter api.User, isReturn bool) {
	f.calls <- notifierCall{
		event: "invited", roomId: room.ID.Hex(),
		author: inviter, invitee: invitee, isReturn: isReturn,
	}
}

// fakeInviteStore — in-memory реализация inviteStore. Ключ карты — пара
// (комната, приглашённый): та же уникальность, что даёт unique-индекс в mongo.
type fakeInviteStore struct {
	mu      sync.Mutex
	invites map[string]api.RoomInvite
	// upsertErr — сбой записи отношения: проверяем, что незаписанный left не
	// оставляет человека вне комнаты без следа
	upsertErr error
	// afterFind вызывается ПОСЛЕ чтения записи и сбрасывает сам себя: так
	// симулируется чужое решение, легшее между чтением записи и её примирением
	afterFind func()
}

func newFakeInviteStore() *fakeInviteStore {
	return &fakeInviteStore{invites: map[string]api.RoomInvite{}}
}

func inviteKey(roomID primitive.ObjectID, inviteeID int) string {
	return roomID.Hex() + ":" + strconv.Itoa(inviteeID)
}

func (f *fakeInviteStore) Upsert(_ context.Context, roomID primitive.ObjectID, inviteeID, inviterID int, status api.InviteStatus, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	key := inviteKey(roomID, inviteeID)
	f.invites[key] = api.RoomInvite{
		RoomID: roomID, InviteeID: inviteeID, InviterID: inviterID,
		Status: status, CreatedAt: now, Version: f.invites[key].Version + 1,
	}
	return nil
}

// UpsertIfUnchanged как mongo-реализация: условие по версии стоит в фильтре, то
// есть проверка «запись не менялась» и запись — одно действие
func (f *fakeInviteStore) UpsertIfUnchanged(_ context.Context, roomID primitive.ObjectID, inviteeID, inviterID int,
	status api.InviteStatus, since *api.RoomInvite, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return false, f.upsertErr
	}
	key := inviteKey(roomID, inviteeID)
	inv, ok := f.invites[key]
	if since == nil {
		if ok {
			return false, nil
		}
	} else if !ok || inv.Version != since.Version {
		return false, nil
	}
	f.invites[key] = api.RoomInvite{
		RoomID: roomID, InviteeID: inviteeID, InviterID: inviterID,
		Status: status, CreatedAt: now, Version: inv.Version + 1,
	}
	return true, nil
}

func (f *fakeInviteStore) Find(_ context.Context, roomID primitive.ObjectID, inviteeID int) (*api.RoomInvite, error) {
	f.mu.Lock()
	inv, ok := f.invites[inviteKey(roomID, inviteeID)]
	hook := f.afterFind
	f.afterFind = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return &inv, nil
}

func (f *fakeInviteStore) ListForUser(_ context.Context, userID int) ([]api.RoomInvite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.RoomInvite
	for _, inv := range f.invites {
		if inv.InviteeID != userID {
			continue
		}
		if inv.Status == api.InvitePending || inv.Status == api.InviteAdded {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (f *fakeInviteStore) SetStatusIfCurrent(_ context.Context, roomID primitive.ObjectID, inviteeID int, from, to api.InviteStatus, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := inviteKey(roomID, inviteeID)
	inv, ok := f.invites[key]
	if !ok || inv.Status != from {
		return false, nil
	}
	inv.Status = to
	inv.CreatedAt = now
	inv.Version++
	f.invites[key] = inv
	return true, nil
}

func (f *fakeInviteStore) DeleteByUserId(_ context.Context, userId int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, inv := range f.invites {
		if inv.InviteeID == userId || inv.InviterID == userId {
			delete(f.invites, key)
		}
	}
	return nil
}
