package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Счётчик непрочитанного на карточке группы: GET /rooms → unreadCount и
// POST /rooms/{id}/notifications-seen.

func unreadOp(donor api.User, recipient api.User, at time.Time) api.Operation {
	d := donor
	return api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             &d,
		RecipientsWithSum: []api.RecipientWithSum{{User: recipient, Sum: 100}},
		Status:            statusActive, SplitType: splitByExactAmount,
		CreateAt: at,
	}
}

// unreadFixture — комната с двумя расходами user1 на user2: один старее общей
// отметки прочитанного user2, второй новее. Отметки по комнате нет — ровно
// состояние всех пользователей сразу после выкатки.
func unreadFixture(t *testing.T) (*Server, *api.Room, time.Time) {
	t.Helper()
	now := time.Now()
	seen := now.Add(-time.Hour)

	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{
			unreadOp(testUser1, testUser2, now.Add(-2*time.Hour)),
			unreadOp(testUser1, testUser2, now.Add(-time.Minute)),
		},
		CreateAt: now.Add(-3 * time.Hour),
	}

	reader := testUser2
	reader.NotificationsSeenAt = &seen
	// testUser3 — живой аккаунт вне комнаты: на нём проверяется членство
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, reader, testUser3), newFakeRoomRepo(room))
	return srv, room, seen
}

// roomUnread читает счётчик комнаты из списка групп.
func roomUnread(t *testing.T, s *Server, userId int, roomId string) int {
	t.Helper()
	token := mustToken(t, s, userId)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /rooms: ожидался 200, получен %d (%s)", rec.Code, rec.Body.String())
	}
	var summaries []roomSummaryDto
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("не удалось разобрать список групп: %v", err)
	}
	for _, sum := range summaries {
		if sum.ID == roomId {
			return sum.UnreadCount
		}
	}
	t.Fatalf("комнаты %s нет в списке групп", roomId)
	return 0
}

// fetchRoom — деталь комнаты (нужен её seenThrough для отметки).
func fetchRoom(t *testing.T, s *Server, userId int, roomId string) roomDetailDto {
	t.Helper()
	token := mustToken(t, s, userId)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+roomId, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /rooms/{id}: ожидался 200, получен %d (%s)", rec.Code, rec.Body.String())
	}
	var out roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("не удалось разобрать комнату: %v", err)
	}
	return out
}

func markRoomSeen(t *testing.T, s *Server, userId int, roomId string, through time.Time) int {
	t.Helper()
	token := mustToken(t, s, userId)
	body := fmt.Sprintf(`{"seenThrough":%q}`, through.Format(time.RFC3339Nano))
	return doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+roomId+"/notifications-seen", token, body).Code
}

// TestRoomUnreadFallsBackToGlobalSeenMark — отметки по комнате ещё нет ни у
// кого (её ставит только открытие группы), поэтому читатель обязан откатываться
// на общую notifications_seen_at. Без фоллбэка в день выкатки загорелись бы
// разом ВСЕ карточки: расходы старше выкатки считались бы непрочитанными.
func TestRoomUnreadFallsBackToGlobalSeenMark(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != 1 {
		t.Fatalf("unreadCount = %d, want 1 (старый расход прочитан по общей отметке)", got)
	}
}

// TestRoomUnreadIgnoresOwnExpenses — правило «событие адресовано мне» то же
// самое, что у бейджа раздела (notifiesUser): свой расход счётчик не поднимает.
func TestRoomUnreadIgnoresOwnExpenses(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	if got := roomUnread(t, srv, testUser1.ID, room.ID.Hex()); got != 0 {
		t.Fatalf("unreadCount донора = %d, want 0", got)
	}
}

// TestRoomUnreadClearedByOpeningRoom — открыли группу, отметили её прочитанной,
// счётчик погас.
func TestRoomUnreadClearedByOpeningRoom(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	detail := fetchRoom(t, srv, testUser2.ID, room.ID.Hex())
	if detail.SeenThrough.IsZero() {
		t.Fatal("seenThrough не заполнен — клиенту нечего вернуть при отметке")
	}
	if code := markRoomSeen(t, srv, testUser2.ID, room.ID.Hex(), detail.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка комнаты: ожидался 204, получен %d", code)
	}

	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != 0 {
		t.Fatalf("после открытия группы unreadCount = %d, want 0", got)
	}
}

// TestRoomUnreadSurvivesNotificationsSection — раздел «Уведомления» НЕ гасит
// счётчик группы: отметка по комнате сильнее общей. Иначе счётчики умирали бы
// от захода в раздел, куда человека и ведёт бейдж, — фича стала бы украшением.
func TestRoomUnreadSurvivesNotificationsSection(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	// человек открыл группу — всё прочитано
	detail := fetchRoom(t, srv, testUser2.ID, room.ID.Hex())
	if code := markRoomSeen(t, srv, testUser2.ID, room.ID.Hex(), detail.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка комнаты: ожидался 204, получен %d", code)
	}
	// потом пришёл новый расход
	ops := append(*room.Operations, unreadOp(testUser1, testUser2, time.Now()))
	room.Operations = &ops
	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != 1 {
		t.Fatalf("новый расход не поднял счётчик: %d, want 1", got)
	}

	// и человек зашёл в раздел «Уведомления»
	if code := markSeen(t, srv, testUser2.ID, time.Now()); code != http.StatusNoContent {
		t.Fatalf("общая отметка: ожидался 204, получен %d", code)
	}

	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != 1 {
		t.Fatalf("раздел «Уведомления» погасил счётчик группы: %d, want 1", got)
	}
}

// TestMarkRoomSeenRequiresMembership — маршрут комнаты обязан проверять
// членство: иначе посторонний писал бы себе отметки по чужим id и заодно узнавал
// бы, существует ли комната.
func TestMarkRoomSeenRequiresMembership(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	token := mustToken(t, srv, testUser3.ID)
	body := fmt.Sprintf(`{"seenThrough":%q}`, time.Now().Format(time.RFC3339Nano))
	rec := doRequest(t, srv, http.MethodPost,
		"/api/v1/rooms/"+room.ID.Hex()+"/notifications-seen", token, body)
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	rec = doRequest(t, srv, http.MethodPost,
		"/api/v1/rooms/"+primitive.NewObjectID().Hex()+"/notifications-seen",
		mustToken(t, srv, testUser2.ID), body)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

// TestMarkRoomSeenValidatesBody — тело то же, что у общей отметки, и проверки
// обязаны совпадать: пустое время и время из будущего отвергаются.
func TestMarkRoomSeenValidatesBody(t *testing.T) {
	srv, room, _ := unreadFixture(t)
	token := mustToken(t, srv, testUser2.ID)
	path := "/api/v1/rooms/" + room.ID.Hex() + "/notifications-seen"

	rec := doRequest(t, srv, http.MethodPost, path, token, `{}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	future := time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)
	rec = doRequest(t, srv, http.MethodPost, path, token, fmt.Sprintf(`{"seenThrough":%q}`, future))
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// TestMarkRoomSeenIsForwardOnly — запоздавший запрос со старым временем
// (ретрай, второй экран) не должен возвращать прочитанное в непрочитанные.
func TestMarkRoomSeenIsForwardOnly(t *testing.T) {
	srv, room, _ := unreadFixture(t)

	detail := fetchRoom(t, srv, testUser2.ID, room.ID.Hex())
	if code := markRoomSeen(t, srv, testUser2.ID, room.ID.Hex(), detail.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка комнаты: ожидался 204, получен %d", code)
	}
	// повтор со старым временем — для клиента идемпотентный, а не сбой
	if code := markRoomSeen(t, srv, testUser2.ID, room.ID.Hex(), detail.SeenThrough.Add(-time.Hour)); code != http.StatusNoContent {
		t.Fatalf("запоздавшая отметка: ожидался 204, получен %d", code)
	}

	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != 0 {
		t.Fatalf("отметку откатили назад: unreadCount = %d, want 0", got)
	}
}

// TestRoomUnreadIsPerRoom — открытие ОДНОЙ группы не гасит счётчик соседней.
func TestRoomUnreadIsPerRoom(t *testing.T) {
	now := time.Now()
	seen := now.Add(-time.Hour)
	first := &api.Room{
		ID: primitive.NewObjectID(), Name: "Первая",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{unreadOp(testUser1, testUser2, now.Add(-time.Minute))},
		CreateAt:   now.Add(-2 * time.Hour),
	}
	second := &api.Room{
		ID: primitive.NewObjectID(), Name: "Вторая",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{unreadOp(testUser1, testUser2, now.Add(-time.Minute))},
		CreateAt:   now.Add(-2 * time.Hour),
	}
	reader := testUser2
	reader.NotificationsSeenAt = &seen
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, reader), newFakeRoomRepo(first, second))

	detail := fetchRoom(t, srv, testUser2.ID, first.ID.Hex())
	if code := markRoomSeen(t, srv, testUser2.ID, first.ID.Hex(), detail.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка комнаты: ожидался 204, получен %d", code)
	}

	if got := roomUnread(t, srv, testUser2.ID, first.ID.Hex()); got != 0 {
		t.Fatalf("открытая группа: unreadCount = %d, want 0", got)
	}
	if got := roomUnread(t, srv, testUser2.ID, second.ID.Hex()); got != 1 {
		t.Fatalf("соседняя группа погасла заодно: unreadCount = %d, want 1", got)
	}
}

// TestRoomUnreadCostsNoExtraQueries — счётчики считаются по документам комнат,
// уже загруженным для самого списка. Чтение комнаты поимённо на карточку
// превратило бы самый посещаемый экран в N+1.
func TestRoomUnreadCostsNoExtraQueries(t *testing.T) {
	now := time.Now()
	seen := now.Add(-time.Hour)
	rooms := make([]*api.Room, 0, 3)
	for i := 0; i < 3; i++ {
		rooms = append(rooms, &api.Room{
			ID: primitive.NewObjectID(), Name: "Группа",
			Members:    &[]api.User{testUser1, testUser2},
			Operations: &[]api.Operation{unreadOp(testUser1, testUser2, now.Add(-time.Minute))},
			CreateAt:   now.Add(-2 * time.Hour),
		})
	}
	reader := testUser2
	reader.NotificationsSeenAt = &seen
	roomRepo := newFakeRoomRepo(rooms...)
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, reader), roomRepo)

	if got := roomUnread(t, srv, testUser2.ID, rooms[0].ID.Hex()); got != 1 {
		t.Fatalf("unreadCount = %d, want 1", got)
	}
	if roomRepo.findByIdCalls != 0 {
		t.Fatalf("список групп прочитал комнаты поимённо %d раз(а) — N+1", roomRepo.findByIdCalls)
	}
}

// TestRoomUnreadOverflow — потолок общий с бейджем раздела: точное значение до
// 99, дальше маркер «больше 99» (клиент рисует «99+»).
func TestRoomUnreadOverflow(t *testing.T) {
	now := time.Now()
	seen := now.Add(-time.Hour)
	ops := make([]api.Operation, 0, maxUnreadCount+5)
	for i := 0; i < maxUnreadCount+5; i++ {
		ops = append(ops, unreadOp(testUser1, testUser2, now.Add(-time.Duration(i)*time.Second)))
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Шумная",
		Members: &[]api.User{testUser1, testUser2}, Operations: &ops,
		CreateAt: now.Add(-2 * time.Hour),
	}
	reader := testUser2
	reader.NotificationsSeenAt = &seen
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, reader), newFakeRoomRepo(room))

	if got := roomUnread(t, srv, testUser2.ID, room.ID.Hex()); got != unreadOverflow {
		t.Fatalf("unreadCount = %d, want %d (маркер «больше 99»)", got, unreadOverflow)
	}
}
