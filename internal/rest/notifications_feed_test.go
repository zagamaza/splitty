package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func fetchNotifications(t *testing.T, s *Server, userId int) notificationsDto {
	t.Helper()
	token := mustToken(t, s, userId)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/notifications", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notifications: ожидался 200, получен %d (%s)", rec.Code, rec.Body.String())
	}
	var out notificationsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}
	return out
}

func markSeen(t *testing.T, s *Server, userId int, through time.Time) int {
	t.Helper()
	token := mustToken(t, s, userId)
	body := fmt.Sprintf(`{"seenThrough":%q}`, through.Format(time.RFC3339Nano))
	return doRequest(t, s, http.MethodPost, "/api/v1/me/notifications-seen", token, body).Code
}

// notifFixture — комната с одним расходом на пользователя 2.
func notifFixture(t *testing.T) (*Server, *fakeInviteStore, *api.Room) {
	t.Helper()
	donor := testUser1
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
		CreateAt:          time.Now(),
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2}, Operations: &[]api.Operation{op},
		CreateAt: time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	invites := newFakeInviteStore()
	srv.SetInvites(invites)
	return srv, invites, room
}

func TestNotificationsCountsUnreadEvents(t *testing.T) {
	srv, _, _ := notifFixture(t)

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Items) != 1 {
		t.Fatalf("ожидалось 1 событие, получено %d", len(got.Items))
	}
	if got.UnreadCount != 1 {
		t.Fatalf("непрочитанных должно быть 1, получено %d", got.UnreadCount)
	}
	if got.SeenThrough.IsZero() {
		t.Fatal("seenThrough не заполнен — клиенту нечего вернуть при отметке")
	}
	if got.SeenThrough.After(time.Now().Add(time.Minute)) {
		t.Fatalf("seenThrough из будущего: %v", got.SeenThrough)
	}

	if code := markSeen(t, srv, testUser2.ID, got.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка прочитанного: ожидался 204, получен %d", code)
	}

	after := fetchNotifications(t, srv, testUser2.ID)
	if after.UnreadCount != 0 {
		t.Fatalf("после отметки непрочитанных должно быть 0, получено %d", after.UnreadCount)
	}
}

// TestNotificationsUnreadCountsAddedInvite — ОБЯЗАТЕЛЬНЫЙ тест: карточка
// «вас добавили» обязана попадать в счётчик. Иначе первое приглашение друга
// показывало бы закреплённую карточку при бейдже 0.
func TestNotificationsUnreadCountsAddedInvite(t *testing.T) {
	srv, invites, room := notifFixture(t)
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InviteAdded, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Invites) != 1 {
		t.Fatalf("ожидалась 1 карточка приглашения, получено %d", len(got.Invites))
	}
	// 1 событие ленты + 1 карточка added
	if got.UnreadCount != 2 {
		t.Fatalf("непрочитанных должно быть 2 (событие + added), получено %d", got.UnreadCount)
	}
	if got.Invites[0].RoomName != room.Name || got.Invites[0].InviterName == "" {
		t.Fatalf("карточка не заполнена: %+v", got.Invites[0])
	}
}

// TestNotificationsPendingSurvivesSeen — pending требует решения, поэтому
// не гаснет от того, что человек заглянул в раздел.
func TestNotificationsPendingSurvivesSeen(t *testing.T) {
	srv, invites, room := notifFixture(t)
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InvitePending, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}

	got := fetchNotifications(t, srv, testUser2.ID)
	markSeen(t, srv, testUser2.ID, got.SeenThrough)

	after := fetchNotifications(t, srv, testUser2.ID)
	if len(after.Invites) != 1 || after.Invites[0].Status != api.InvitePending {
		t.Fatalf("pending пропал после отметки: %+v", after.Invites)
	}
	if after.UnreadCount != 1 {
		t.Fatalf("pending должен оставаться в счётчике, получено %d", after.UnreadCount)
	}
}

// TestNotificationsSeenDoesNotSwallowNewEvents — главный смысл seenThrough:
// событие, появившееся ПОСЛЕ формирования ответа, не должно быть погашено
// отметкой по этому ответу.
func TestNotificationsSeenDoesNotSwallowNewEvents(t *testing.T) {
	srv, invites, room := notifFixture(t)

	got := fetchNotifications(t, srv, testUser2.ID)

	// Пока человек смотрел на экран, пришло приглашение.
	later := got.SeenThrough.Add(time.Second)
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InviteAdded, later); err != nil {
		t.Fatalf("новое приглашение: %v", err)
	}

	markSeen(t, srv, testUser2.ID, got.SeenThrough)

	after := fetchNotifications(t, srv, testUser2.ID)
	if after.UnreadCount == 0 {
		t.Fatal("событие, пришедшее после seenThrough, было погашено — человек его не видел")
	}
	if len(after.Invites) != 1 {
		t.Fatalf("карточка нового приглашения пропала: %+v", after.Invites)
	}
}

func TestMarkNotificationsSeenValidation(t *testing.T) {
	srv, _, _ := notifFixture(t)
	token := mustToken(t, srv, testUser2.ID)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"пустое тело", `{}`, http.StatusBadRequest},
		{"из будущего", fmt.Sprintf(`{"seenThrough":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339)), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/notifications-seen", token, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("ожидался %d, получен %d", tt.want, rec.Code)
			}
		})
	}
}

// TestMarkSeenNeverGoesBackwards — запоздавший запрос со старым значением не
// должен вернуть уже прочитанное в непрочитанные.
func TestMarkSeenNeverGoesBackwards(t *testing.T) {
	srv, _, _ := notifFixture(t)

	got := fetchNotifications(t, srv, testUser2.ID)
	markSeen(t, srv, testUser2.ID, got.SeenThrough)
	markSeen(t, srv, testUser2.ID, got.SeenThrough.Add(-time.Hour))

	after := fetchNotifications(t, srv, testUser2.ID)
	if after.UnreadCount != 0 {
		t.Fatalf("старая отметка откатила прочитанное: непрочитанных %d", after.UnreadCount)
	}
}

// TestActivityStillWorks — /activity обязан продолжать отвечать: в проде есть
// старые сборки клиентов, и выкатка бэкенда не должна их ломать.
func TestActivityStillWorks(t *testing.T) {
	srv, _, _ := notifFixture(t)
	token := mustToken(t, srv, testUser2.ID)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/activity", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/activity сломан: %d", rec.Code)
	}
	var items []activityItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("формат /activity изменился: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ожидалось 1 событие, получено %d", len(items))
	}
}
