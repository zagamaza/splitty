package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const testAdminToken = "admin-token"

type fakeAdminRooms struct {
	rooms     []repository.RoomBrief
	ofUser    []api.Room
	lastQuery string
	lastLimit int
	size      int
}

func (f *fakeAdminRooms) SearchRooms(_ context.Context, query string, limit int) ([]repository.RoomBrief, error) {
	f.lastQuery = query
	f.lastLimit = limit
	return f.rooms, nil
}

func (f *fakeAdminRooms) RoomSizeBytes(_ context.Context, _ string) (int, error) {
	return f.size, nil
}

func (f *fakeAdminRooms) AllRoomsOfUser(_ context.Context, _ int) ([]api.Room, error) {
	return f.ofUser, nil
}

type fakeAdminUsers struct {
	users     []api.User
	lastQuery string
}

func (f *fakeAdminUsers) SearchUsers(_ context.Context, query string, _ int) ([]api.User, error) {
	f.lastQuery = query
	return f.users, nil
}

func adminServer(t *testing.T, room *api.Room) (*Server, *fakeAdminRooms) {
	t.Helper()
	s := newTestServer(Config{AdminToken: testAdminToken}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	store := &fakeAdminRooms{size: 4096, ofUser: []api.Room{*room}}
	s.SetAdminRooms(store)
	s.SetAdminUsers(&fakeAdminUsers{users: []api.User{testUser1, testUser2}})
	return s, store
}

func doAdmin(t *testing.T, s *Server, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	return rec
}

// Админские маршруты НЕ должны существовать на публичном обработчике. Проверка
// не теоретическая: ровно на этом обжигались в панели — обработчики оказались
// подключены не туда, запрос падал в другую ветку и снаружи выглядел рабочим.
// Здесь цена ошибки выше: /admin отдаёт чужие суммы и долги
func TestAdminRoutesAbsentFromPublicHandler(t *testing.T) {
	room := newTestRoom()
	s, _ := adminServer(t, room)

	for _, path := range []string{"/admin/rooms", "/admin/rooms/" + room.ID.Hex()} {
		rec := doRequest(t, s, "GET", path, testAdminToken, "")
		assertErrorCode(t, rec, 404, "not_found")
	}
}

func TestAdminApiRejectsBadToken(t *testing.T) {
	room := newTestRoom()
	s, _ := adminServer(t, room)

	for _, token := range []string{"", "не тот токен", testAdminToken + "x"} {
		rec := doAdmin(t, s, "/admin/rooms", token)
		assertErrorCode(t, rec, 401, "unauthorized")
	}

	assertStatus(t, doAdmin(t, s, "/admin/rooms", testAdminToken), 200)
}

// Пустой токен в конфиге — API закрыт целиком, а не «пускает всех, у кого
// пустой заголовок»: пустая строка совпала бы сама с собой
func TestAdminApiClosedWithoutConfiguredToken(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))
	s.SetAdminRooms(&fakeAdminRooms{})

	assertErrorCode(t, doAdmin(t, s, "/admin/rooms", ""), 503, "unavailable")
	assertErrorCode(t, doAdmin(t, s, "/admin/rooms/"+room.ID.Hex(), "любой"), 503, "unavailable")
}

func TestAdminRoomsSearch(t *testing.T) {
	room := newTestRoom()
	s, store := adminServer(t, room)
	last := time.Now().UTC().Truncate(time.Second)
	store.rooms = []repository.RoomBrief{{
		ID: room.ID, Name: "Стамбул", CreateAt: last, Currency: "",
		MemberCount: 3, OperationCount: 7, LastOperationAt: &last, SizeBytes: 12345,
	}}

	rec := doAdmin(t, s, "/admin/rooms?q=%D0%A1%D1%82%D0%B0%D0%BC&limit=5", testAdminToken)
	assertStatus(t, rec, 200)

	if store.lastQuery != "Стам" || store.lastLimit != 5 {
		t.Errorf("в хранилище ушло q=%q limit=%d", store.lastQuery, store.lastLimit)
	}

	var items []adminRoomBriefDto
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("строк в ответе: %d", len(items))
	}
	if items[0].ID != room.ID.Hex() || items[0].MemberCount != 3 || items[0].OperationCount != 7 {
		t.Errorf("строка списка: %+v", items[0])
	}
	// Комната без валюты в базе — не комната без валюты на экране: до выбора
	// валюты приложение считает такие рублёвыми, и админка обязана показывать
	// то же самое, а не пустое место
	if items[0].Currency != api.DefaultCurrency {
		t.Errorf("валюта = %q, ожидался дефолт %q", items[0].Currency, api.DefaultCurrency)
	}
}

// Пустой поиск — не ошибка: это «покажи последние». Отдельный тест, потому что
// пустой q легко было бы отбить как невалидный запрос
func TestAdminRoomsAllowsEmptyQuery(t *testing.T) {
	s, store := adminServer(t, newTestRoom())

	assertStatus(t, doAdmin(t, s, "/admin/rooms", testAdminToken), 200)
	if store.lastQuery != "" {
		t.Errorf("пустой поиск ушёл как %q", store.lastQuery)
	}
	if store.lastLimit <= 0 {
		t.Errorf("лимит по умолчанию = %d", store.lastLimit)
	}
}

func TestAdminRoomCard(t *testing.T) {
	room := newTestRoom()
	// Удалённый расход остаётся в документе и обязан быть видим в админке:
	// вопрос «куда делись деньги» чаще всего именно про него
	donor := testUser2
	deleted := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "Отменённый ужин",
		Sum:               300,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 150}, {User: testUser2, Sum: 150}},
		Status:            api.StatusArchive,
		CreateAt:          time.Now(),
	}
	*room.Operations = append(*room.Operations, deleted)
	// Участник 2 спрятал группу у себя — на остальных это не влияет
	room.RoomStates.Archived = []int{testUser2.ID}

	s, _ := adminServer(t, room)

	rec := doAdmin(t, s, "/admin/rooms/"+room.ID.Hex(), testAdminToken)
	assertStatus(t, rec, 200)

	var card adminRoomDto
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("ответ: %v", err)
	}

	if card.ID != room.ID.Hex() || card.Name != room.Name {
		t.Errorf("карточка не про ту комнату: %+v", card)
	}
	// Легаси-расход на 100 пополам; удалённый в сумму не входит
	if card.TotalSpent != 100 {
		t.Errorf("потрачено = %d, ожидалось 100", card.TotalSpent)
	}
	if card.OperationCount != 1 || card.DeletedCount != 1 {
		t.Errorf("действующих %d, удалённых %d", card.OperationCount, card.DeletedCount)
	}
	if len(card.Operations) != 2 {
		t.Fatalf("в списке %d операций — удалённая должна быть видна", len(card.Operations))
	}
	if card.SizeBytes != 4096 {
		t.Errorf("вес комнаты = %d", card.SizeBytes)
	}

	var statuses []string
	for _, op := range card.Operations {
		statuses = append(statuses, op.Status)
	}
	if statuses[0] != string(api.StatusActive) || statuses[1] != string(api.StatusArchive) {
		t.Errorf("статусы операций: %v", statuses)
	}

	if len(card.Debts) != 1 || card.Debts[0].Sum != 50 {
		t.Fatalf("долги: %+v", card.Debts)
	}
	if card.Debts[0].Debtor.ID != testUser2.ID || card.Debts[0].Lender.ID != testUser1.ID {
		t.Errorf("долг не в ту сторону: %+v", card.Debts[0])
	}

	byId := map[int]adminMemberDto{}
	for _, m := range card.Members {
		byId[m.ID] = m
	}
	if len(byId) != 2 {
		t.Fatalf("участников: %d", len(byId))
	}
	// Балансы участников — то же самое, что видит каждый из них у себя
	if byId[testUser1.ID].Balance != 50 || byId[testUser2.ID].Balance != -50 {
		t.Errorf("балансы: %+v", byId)
	}
	if byId[testUser1.ID].Spent != 50 || byId[testUser2.ID].Spent != 50 {
		t.Errorf("доли в расходах: %+v", byId)
	}
	if byId[testUser1.ID].Archived || !byId[testUser2.ID].Archived {
		t.Errorf("архив — свойство пары «человек × комната»: %+v", byId)
	}
}

func TestAdminRoomNotFound(t *testing.T) {
	s, _ := adminServer(t, newTestRoom())

	assertErrorCode(t, doAdmin(t, s, "/admin/rooms/"+primitive.NewObjectID().Hex(), testAdminToken), 404, "not_found")
	assertErrorCode(t, doAdmin(t, s, "/admin/rooms/не-id", testAdminToken), 404, "not_found")
}

// Без поиска (SetAdminRooms не вызывали) карточка обязана работать: она
// обходится обычным чтением комнаты, а падать целиком из-за отсутствующей
// зависимости — терять весь экран ради одной цифры
func TestAdminRoomCardWithoutSearchStore(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{AdminToken: testAdminToken}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))

	assertStatus(t, doAdmin(t, s, "/admin/rooms/"+room.ID.Hex(), testAdminToken), 200)
	assertErrorCode(t, doAdmin(t, s, "/admin/rooms", testAdminToken), 503, "unavailable")
}

// Карточка человека отвечает на вопросы поддержки: чем он входит, сколько у
// него устройств и что у него с деньгами по тусам.
func TestAdminUserCard(t *testing.T) {
	room := newTestRoom()
	room.RoomStates.Archived = []int{testUser2.ID}
	telegram := 777001
	user := testUser2
	user.TelegramID = &telegram
	user.PasswordHash = "$2a$10$очень-секретно"
	user.LoginEmail = "almaz@example.test"
	user.PushTokens = []api.PushToken{{Token: "секретный-токен", Platform: "ios"}, {Token: "другой", Platform: "ios"}}

	s := newTestServer(Config{AdminToken: testAdminToken}, newFakeUserRepo(testUser1, user), newFakeRoomRepo(room))
	s.SetAdminRooms(&fakeAdminRooms{ofUser: []api.Room{*room}})

	rec := doAdmin(t, s, "/admin/users/"+strconv.Itoa(user.ID), testAdminToken)
	assertStatus(t, rec, 200)

	body := rec.Body.String()
	// Секреты наружу не уходят ни под каким видом: по ним человеку не помочь,
	// а утечь они могут
	for _, secret := range []string{"секретно", "секретный-токен", "777001"} {
		if strings.Contains(body, secret) {
			t.Errorf("в карточке человека утёк секрет %q", secret)
		}
	}

	var card adminUserDto
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if card.ID != user.ID || card.DisplayName != user.DisplayName {
		t.Errorf("карточка не про того человека: %+v", card)
	}
	// Факт привязки нужен, значение — нет
	if strings.Join(card.Logins, ",") != "telegram,password" {
		t.Errorf("способы входа: %v", card.Logins)
	}
	if card.Devices != 2 || strings.Join(card.Platforms, ",") != "ios" {
		t.Errorf("устройства: %d %v", card.Devices, card.Platforms)
	}
	if card.LoginEmail != user.LoginEmail {
		t.Errorf("адрес входа потерялся: %q", card.LoginEmail)
	}

	if len(card.Rooms) != 1 {
		t.Fatalf("тус в карточке: %d", len(card.Rooms))
	}
	line := card.Rooms[0]
	// Легаси-расход на 100 пополам: второй должен первому 50
	if line.Balance != -50 || line.Spent != 50 {
		t.Errorf("деньги по тусе: баланс %d, доля %d", line.Balance, line.Spent)
	}
	// Спрятанная у себя туса обязана быть видна админке — «у меня пропала
	// группа» чаще всего означает именно архив
	if !line.Archived {
		t.Error("архив у себя не показан")
	}
}

func TestAdminUsersSearch(t *testing.T) {
	s, _ := adminServer(t, newTestRoom())

	rec := doAdmin(t, s, "/admin/users?q=zagir", testAdminToken)
	assertStatus(t, rec, 200)

	var items []adminUserBriefDto
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if len(items) != 2 || items[0].ID != testUser1.ID {
		t.Fatalf("выдача поиска: %+v", items)
	}
}

func TestAdminUserNotFound(t *testing.T) {
	s, _ := adminServer(t, newTestRoom())

	assertErrorCode(t, doAdmin(t, s, "/admin/users/999999", testAdminToken), 404, "not_found")
	assertErrorCode(t, doAdmin(t, s, "/admin/users/не-число", testAdminToken), 404, "not_found")
}
