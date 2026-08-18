package rest

// Админский API: чтение чужих комнат для панели администратора.
//
// Живёт на ОТДЕЛЬНОМ слушателе (AdminHandler), который наружу не публикуется —
// до него дотягивается только соседний контейнер по сети docker. Это не
// перестраховка: здесь отдаются имена, суммы и долги живых людей, и на
// публичном домене такому эндпоинту делать нечего, каким бы токеном он ни был
// закрыт. Токен всё равно обязателен — в одной сети живут и другие контейнеры.

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
)

// adminRoomStore — то, что админскому API нужно сверх обычных репозиториев.
// Интерфейс узкий и объявлен здесь (как Notifier и inviteStore): тестам нужен
// фейк, а не живой mongo. Реализация — repository.MongoRoomRepository
type adminRoomStore interface {
	SearchRooms(ctx context.Context, query string, limit int) ([]repository.RoomBrief, error)
	RoomSizeBytes(ctx context.Context, roomId string) (int, error)
	// AllRoomsOfUser — включая спрятанные человеком у себя: «у меня пропала
	// группа» чаще всего означает именно архив
	AllRoomsOfUser(ctx context.Context, userId int) ([]api.Room, error)
}

// adminUserStore — поиск людей для админки. Отдельно от adminRoomStore: это
// другая коллекция и другая реализация (repository.MongoUserRepository)
type adminUserStore interface {
	SearchUsers(ctx context.Context, query string, limit int) ([]api.User, error)
}

// SetAdminUsers включает поиск людей. nil — поиск отвечает 503, карточка
// человека работает: ей хватает обычного userRepo
func (s *Server) SetAdminUsers(store adminUserStore) { s.adminUsers = store }

// SetAdminRooms включает поиск комнат в админском API. nil (метод не вызван) —
// поиск отвечает 503, карточка комнаты работает: она обходится FindById
func (s *Server) SetAdminRooms(store adminRoomStore) { s.adminRooms = store }

// adminRoomBriefDto строка списка комнат. Денег здесь нет намеренно: чтобы
// посчитать траты, пришлось бы прочитать все операции каждой комнаты — список
// стоил бы мегабайты ради колонки, которую видно и в карточке
type adminRoomBriefDto struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	CreatedAt       time.Time  `json:"createdAt"`
	Currency        string     `json:"currency"`
	MemberCount     int        `json:"memberCount"`
	OperationCount  int        `json:"operationCount"`
	LastOperationAt *time.Time `json:"lastOperationAt,omitempty"`
	SizeBytes       int        `json:"sizeBytes"`
}

// adminMemberDto участник комнаты глазами админки: сколько на нём расходов и
// куда его вывел расчёт долгов
type adminMemberDto struct {
	userDto
	// Archived — человек спрятал группу У СЕБЯ. Свойство пары «человек ×
	// комната», а не комнаты: у остальных она осталась на виду
	Archived bool `json:"archived"`
	Spent    int  `json:"spent"`
	// Balance >0 — должны ему, <0 — должен он
	Balance int `json:"balance"`
}

// adminOperationDto расход глазами админки: те же поля, что видит приложение,
// плюс статус. Удалённые расходы показываем — вопрос «куда делись деньги»
// чаще всего именно про них
type adminOperationDto struct {
	operationDto
	Status string `json:"status"`
}

type adminRoomDto struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"createdAt"`
	Currency     string    `json:"currency"`
	AvatarFileId string    `json:"avatarFileId,omitempty"`
	// SizeBytes вес документа комнаты. Потолок mongo — 16 МБ, и упереться в
	// него комната может молча: запись просто перестанет проходить
	SizeBytes  int              `json:"sizeBytes"`
	Members    []adminMemberDto `json:"members"`
	TotalSpent int              `json:"totalSpent"`
	// OperationCount — действующие расходы; Deleted/Draft — сколько ещё лежит
	// в документе, но в расчёт не идёт
	OperationCount int        `json:"operationCount"`
	DeletedCount   int        `json:"deletedCount"`
	DraftCount     int        `json:"draftCount"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	Debts          []debtDto  `json:"debts"`
	// DebtsUnavailable true — долги на этих данных не считаются
	// (см. roomDebtsSafe); остальное отдаётся как обычно
	DebtsUnavailable bool                `json:"debtsUnavailable,omitempty"`
	Operations       []adminOperationDto `json:"operations"`
	InviteUrl        string              `json:"inviteUrl,omitempty"`
}

// AdminHandler — обработчик админского API для внутреннего слушателя.
// В Handler() эти маршруты НЕ входят: публичный порт про них знать не должен
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /admin/rooms", s.adminAuth(s.handleAdminRooms))
	mux.Handle("GET /admin/rooms/{roomId}", s.adminAuth(s.handleAdminRoom))
	mux.Handle("GET /admin/users", s.adminAuth(s.handleAdminUsers))
	mux.Handle("GET /admin/users/{userId}", s.adminAuth(s.handleAdminUser))

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
	})

	return recoverMiddleware(maxBodyMiddleware(mux))
}

// adminAuth пускает по общему токену и пишет в лог КАЖДЫЙ пропущенный запрос.
//
// Журнал здесь не для отладки: админка читает переписку людей о деньгах, и
// «кто и что смотрел» обязано остаться следом. Отказы логируются тоже — иначе
// перебор токена ничем себя не выдаёт
func (s *Server) adminAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminToken == "" {
			// Сюда не должно доходить: без токена слушатель не поднимается.
			// Но если дошло — молчим, а не пускаем всех
			writeError(w, http.StatusServiceUnavailable, "unavailable", "админский api выключен")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminToken)) != 1 {
			log.Warn().Str("ip", s.clientIP(r)).Str("path", r.URL.Path).Msg("админский api: отказ")
			writeError(w, http.StatusUnauthorized, "unauthorized", "нужен токен")
			return
		}
		log.Info().
			Str("ip", s.clientIP(r)).
			Str("path", r.URL.Path).
			Str("query", r.URL.RawQuery).
			Msg("админский api")
		next(w, r)
	})
}

// handleAdminRooms GET /admin/rooms?q=&limit= — поиск комнаты по имени или id
func (s *Server) handleAdminRooms(w http.ResponseWriter, r *http.Request) {
	if s.adminRooms == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "поиск комнат недоступен")
		return
	}

	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		limit = 30
	}

	rooms, err := s.adminRooms.SearchRooms(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		log.Error().Err(err).Msg("админский api: поиск комнат")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось найти комнаты")
		return
	}

	items := make([]adminRoomBriefDto, 0, len(rooms))
	for _, room := range rooms {
		currency := room.Currency
		if currency == "" {
			currency = api.DefaultCurrency
		}
		items = append(items, adminRoomBriefDto{
			ID:              room.ID.Hex(),
			Name:            room.Name,
			CreatedAt:       room.CreateAt,
			Currency:        currency,
			MemberCount:     room.MemberCount,
			OperationCount:  room.OperationCount,
			LastOperationAt: room.LastOperationAt,
			SizeBytes:       room.SizeBytes,
		})
	}

	writeJSON(w, http.StatusOK, items)
}

// handleAdminRoom GET /admin/rooms/{roomId} — карточка комнаты целиком
func (s *Server) handleAdminRoom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	room, hErr := s.findRoom(ctx, r.PathValue("roomId"))
	if hErr != nil {
		hErr.write(w)
		return
	}

	card := s.buildAdminRoom(room)
	if s.adminRooms != nil {
		// Вес — единственное, чего нет в самом документе. Не посчитался —
		// отдаём карточку без него: ради одной цифры терять экран незачем
		if size, err := s.adminRooms.RoomSizeBytes(ctx, room.ID.Hex()); err != nil {
			log.Warn().Err(err).Msg("админский api: не посчитал вес комнаты")
		} else {
			card.SizeBytes = size
		}
	}

	writeJSON(w, http.StatusOK, card)
}

// adminUserBriefDto строка списка людей
type adminUserBriefDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Deleted     bool   `json:"deleted,omitempty"`
}

// adminUserRoomDto туса человека глазами админки: только то, что о ней нужно
// знать в его карточке
type adminUserRoomDto struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Balance  int    `json:"balance"`
	Spent    int    `json:"spent"`
	Members  int    `json:"members"`
	Archived bool   `json:"archived"`
	// LastAt — nil у тусы без единого расхода. Нулевое время уехало бы наружу
	// как «1 января 1 года» и выглядело бы данными
	LastAt *time.Time `json:"lastActivityAt,omitempty"`
	// DebtsUnavailable — долги этой комнаты не считаются, баланс отдан нулём
	DebtsUnavailable bool `json:"debtsUnavailable,omitempty"`
}

// adminUserDto карточка человека. Никаких секретов: sub-ы провайдеров, хэш
// пароля и сами push-токены наружу не отдаются — по ним человеку помочь нельзя,
// а утечь они могут. Отдаём ФАКТЫ: чем входит, сколько устройств, жив ли
type adminUserDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Lang        string `json:"lang,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
	// Logins — чем человек входит: telegram, google, apple, password
	Logins     []string `json:"logins"`
	LoginEmail string   `json:"loginEmail,omitempty"`
	// Devices — сколько устройств ждёт пуши, и на каких платформах
	Devices   int      `json:"devices"`
	Platforms []string `json:"platforms,omitempty"`
	// PushOff — человек выключил уведомления целиком
	PushOff bool `json:"pushOff,omitempty"`
	// TokensRevokedAt — когда он в последний раз выходил на всех устройствах
	TokensRevokedAt *time.Time         `json:"tokensRevokedAt,omitempty"`
	Rooms           []adminUserRoomDto `json:"rooms"`
}

// handleAdminUsers GET /admin/users?q=&limit= — поиск человека
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.adminUsers == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "поиск людей недоступен")
		return
	}

	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		limit = 30
	}

	users, err := s.adminUsers.SearchUsers(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		log.Error().Err(err).Msg("админский api: поиск людей")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось найти людей")
		return
	}

	items := make([]adminUserBriefDto, 0, len(users))
	for i := range users {
		items = append(items, adminUserBriefDto{
			ID:          users[i].ID,
			Username:    users[i].Username,
			DisplayName: users[i].DisplayName,
			Deleted:     users[i].DeletedAt != nil,
		})
	}

	writeJSON(w, http.StatusOK, items)
}

// handleAdminUser GET /admin/users/{userId} — карточка человека и его тусы
func (s *Server) handleAdminUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	user, err := s.userRepo.FindById(ctx, userId)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "not_found", "нет такого человека")
		return
	}

	card := adminUserDto{
		ID:              user.ID,
		Username:        user.Username,
		DisplayName:     user.DisplayName,
		Lang:            user.UserLang,
		Deleted:         user.DeletedAt != nil,
		Logins:          userLogins(user),
		LoginEmail:      user.LoginEmail,
		Devices:         len(user.PushTokens),
		Platforms:       devicePlatforms(user),
		PushOff:         user.NotificationOn != nil && !*user.NotificationOn,
		TokensRevokedAt: user.TokensValidFrom,
		Rooms:           []adminUserRoomDto{},
	}

	// Тусы читаем, только если есть чем: без них карточка всё равно полезна —
	// «чем входит» и «сколько устройств» отвечают на большую часть вопросов
	if s.adminRooms != nil {
		rooms, err := s.adminRooms.AllRoomsOfUser(ctx, userId)
		if err != nil {
			log.Error().Err(err).Msg("админский api: тусы человека")
		} else {
			for i := range rooms {
				card.Rooms = append(card.Rooms, s.userRoomLine(&rooms[i], userId))
			}
		}
	}

	writeJSON(w, http.StatusOK, card)
}

func (s *Server) userRoomLine(room *api.Room, userId int) adminUserRoomDto {
	debts, ok := s.roomDebtsSafe(room)
	active := activeOperations(room)

	var last *time.Time
	for i := range active {
		if last == nil || active[i].CreateAt.After(*last) {
			at := active[i].CreateAt
			last = &at
		}
	}

	return adminUserRoomDto{
		ID:               room.ID.Hex(),
		Name:             room.Name,
		Currency:         roomCurrencyCode(room),
		Balance:          balanceFromDebts(debts, userId),
		Spent:            userSpentSum(active, userId),
		Members:          len(roomMembers(room)),
		Archived:         isRoomArchived(room, userId),
		LastAt:           last,
		DebtsUnavailable: !ok,
	}
}

// userLogins — чем человек может войти. Значения личностей (sub, telegram id)
// наружу не идут: помочь по ним нельзя, а утечь они могут
func userLogins(u *api.User) []string {
	logins := []string{}
	if u.TelegramID != nil {
		logins = append(logins, "telegram")
	}
	if u.GoogleSub != "" {
		logins = append(logins, "google")
	}
	if u.AppleSub != "" {
		logins = append(logins, "apple")
	}
	if u.PasswordHash != "" {
		logins = append(logins, "password")
	}
	return logins
}

func devicePlatforms(u *api.User) []string {
	seen := map[string]bool{}
	platforms := []string{}
	for _, token := range u.PushTokens {
		name := token.Platform
		if name == "" {
			name = "неизвестно"
		}
		if !seen[name] {
			seen[name] = true
			platforms = append(platforms, name)
		}
	}
	return platforms
}

func (s *Server) buildAdminRoom(room *api.Room) *adminRoomDto {
	debts, debtsOK := s.roomDebtsSafe(room)
	active := activeOperations(room)

	members := make([]adminMemberDto, 0, len(roomMembers(room)))
	for _, member := range roomMembers(room) {
		members = append(members, adminMemberDto{
			userDto:  toUserDto(&member),
			Archived: isRoomArchived(room, member.ID),
			Spent:    userSpentSum(active, member.ID),
			Balance:  balanceFromDebts(debts, member.ID),
		})
	}

	all := roomOperations(room)
	operations := make([]adminOperationDto, 0, len(all))
	var deleted, draft int
	var lastActivity *time.Time
	for i := range all {
		normalized := normalizedOperation(all[i])
		switch normalized.Status {
		case api.StatusArchive:
			deleted++
		case api.StatusDraft:
			draft++
		}
		if lastActivity == nil || normalized.CreateAt.After(*lastActivity) {
			at := normalized.CreateAt
			lastActivity = &at
		}
		operations = append(operations, adminOperationDto{
			operationDto: toOperationDto(&normalized),
			Status:       string(normalized.Status),
		})
	}

	return &adminRoomDto{
		ID:               room.ID.Hex(),
		Name:             room.Name,
		CreatedAt:        room.CreateAt,
		Currency:         roomCurrencyCode(room),
		AvatarFileId:     roomAvatarFileId(room),
		Members:          members,
		TotalSpent:       roomTotalSpent(active),
		OperationCount:   len(active),
		DeletedCount:     deleted,
		DraftCount:       draft,
		LastActivityAt:   lastActivity,
		Debts:            toDebtDtos(debts),
		DebtsUnavailable: !debtsOK,
		Operations:       operations,
		InviteUrl:        s.inviteURL(room.ID.Hex()),
	}
}
