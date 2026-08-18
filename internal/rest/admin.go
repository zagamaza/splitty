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
}

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
