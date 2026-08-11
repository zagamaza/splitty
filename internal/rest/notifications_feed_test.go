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
	// pending бывает только у НЕ-участника: приглашение как раз и ждёт согласия
	// войти. Участнику такая карточка не показывается вовсе (inviteCardStatus),
	// поэтому «висящая заявка» на участнике проверяла бы несуществующий случай
	room.Members = &[]api.User{testUser1}
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
	if code := markSeen(t, srv, testUser2.ID, got.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("первая отметка: ожидался 204, получен %d", code)
	}
	// Запоздавший запрос — идемпотентный повтор, а не ошибка: 204 без изменений.
	if code := markSeen(t, srv, testUser2.ID, got.SeenThrough.Add(-time.Hour)); code != http.StatusNoContent {
		t.Fatalf("запоздавшая отметка: ожидался 204, получен %d", code)
	}

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

// TestNotificationsAddedCardDisappearsAfterSeen — карточка «вас добавили»
// информационная и обязана гаснуть после просмотра. Требование Task 7; без
// этого теста переход к «показывать added всегда» прошёл бы незамеченным.
func TestNotificationsAddedCardDisappearsAfterSeen(t *testing.T) {
	srv, invites, room := notifFixture(t)
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InviteAdded, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Invites) != 1 {
		t.Fatalf("карточка added должна показываться до отметки, получено %d", len(got.Invites))
	}
	if code := markSeen(t, srv, testUser2.ID, got.SeenThrough); code != http.StatusNoContent {
		t.Fatalf("отметка прочитанного: получен %d", code)
	}

	after := fetchNotifications(t, srv, testUser2.ID)
	if len(after.Invites) != 0 {
		t.Fatalf("после отметки карточка added осталась: %+v", after.Invites)
	}
	if after.UnreadCount != 0 {
		t.Fatalf("после отметки непрочитанных должно быть 0, получено %d", after.UnreadCount)
	}
}

// TestNotificationsUnreadCountsWholeFeed — счётчик считается по ВСЕЙ ленте, а
// не по отданной странице. Источник бейджа просит limit=1, и счёт по странице
// упирал бы его в единицу: двадцать новых расходов давали бы «1».
func TestNotificationsUnreadCountsWholeFeed(t *testing.T) {
	donor := testUser1
	ops := make([]api.Operation, 0, 5)
	for i := 0; i < 5; i++ {
		ops = append(ops, api.Operation{
			ID: primitive.NewObjectID(), Description: fmt.Sprintf("Расход %d", i), Sum: 100,
			Donor:             &donor,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
			CreateAt:          time.Now(),
		})
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2}, Operations: &ops,
		CreateAt: time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())

	token := mustToken(t, srv, testUser2.ID)
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/notifications?limit=1&offset=0", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d", rec.Code)
	}
	var got notificationsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("страница обязана уважать limit=1, получено %d", len(got.Items))
	}
	if got.UnreadCount != 5 {
		t.Fatalf("счётчик должен считать всю ленту (5), получено %d", got.UnreadCount)
	}
}

// TestNotificationsCountsOnlyEventsAddressedToUser — счётчик обязан совпадать с
// тем, о чём приходит push (internal/bot/notifier.go): свой расход и чужой
// расход без твоей доли бейдж не поднимают. Иначе раздел «Уведомления» сообщал
// бы человеку о его же действиях.
func TestNotificationsCountsOnlyEventsAddressedToUser(t *testing.T) {
	me, other := testUser2, testUser1
	mine := api.Operation{
		ID: primitive.NewObjectID(), Description: "Мой расход", Sum: 100,
		Donor:             &me,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 50}, {User: testUser2, Sum: 50}},
		CreateAt:          time.Now(),
	}
	foreign := api.Operation{
		ID: primitive.NewObjectID(), Description: "Чужой расход", Sum: 100,
		Donor:             &other,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser3, Sum: 100}},
		CreateAt:          time.Now(),
	}
	zeroShare := api.Operation{
		ID: primitive.NewObjectID(), Description: "Нулевая доля", Sum: 100,
		Donor:             &other,
		SplitType:         splitByExactAmount,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser3, Sum: 100}, {User: testUser2, Sum: 0}},
		CreateAt:          time.Now(),
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &[]api.Operation{mine, foreign, zeroShare},
		CreateAt:   time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Items) != 3 {
		t.Fatalf("лента показывает все события комнаты, получено %d", len(got.Items))
	}
	if got.UnreadCount != 0 {
		t.Fatalf("непрочитанных быть не должно (свой расход, чужой расход, нулевая доля), получено %d", got.UnreadCount)
	}

	// Контроль: расход с ненулевой долей от другого человека считается.
	addressed := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             &other,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
		CreateAt:          time.Now(),
	}
	*room.Operations = append(*room.Operations, addressed)
	if again := fetchNotifications(t, srv, testUser2.ID); again.UnreadCount != 1 {
		t.Fatalf("адресованный человеку расход обязан попасть в счётчик, получено %d", again.UnreadCount)
	}
}

// notifServerWithOps — сервер с одной комнатой из переданных операций.
func notifServerWithOps(t *testing.T, ops ...api.Operation) *Server {
	t.Helper()
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &ops,
		CreateAt:   time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())
	return srv
}

// TestNotificationsCountsWhoWasNotified — счётчик обязан совпадать с тем, о чём
// человеку сообщали, а точная запись этого — notification_sent самой операции
// (его пишут и notifier REST, и экраны бота).
//
// Без него бейдж строился по долям и промахивался в обе стороны: расход, где
// тебя НАЗНАЧИЛИ плательщиком (уведомление уходит всегда, даже мимо настроек),
// в бейдж не попадал, а свой же расход с чужим плательщиком — попадал.
func TestNotificationsCountsWhoWasNotified(t *testing.T) {
	me, other := testUser2, testUser1

	// Меня назначили плательщиком: уведомление ушло мне, по долям я «свой».
	payerAssigned := api.Operation{
		ID: primitive.NewObjectID(), Description: "Такси", Sum: 100,
		Donor:             &me,
		RecipientsWithSum: []api.RecipientWithSum{{User: other, Sum: 50}, {User: me, Sum: 50}},
		NotificationSent:  []int{me.ID},
		CreateAt:          time.Now(),
	}
	// Расход завёл я, плательщиком поставил другого: уведомили его, не меня.
	authoredByMe := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             &other,
		RecipientsWithSum: []api.RecipientWithSum{{User: me, Sum: 100}},
		NotificationSent:  []int{other.ID},
		CreateAt:          time.Now(),
	}
	// Легаси эпохи master-2021: списка нет вовсе — работает правило по долям.
	legacy := api.Operation{
		ID: primitive.NewObjectID(), Description: "Продукты", Sum: 100,
		Donor:      &other,
		Recipients: &[]api.User{other, me},
		CreateAt:   time.Now(),
	}

	tests := []struct {
		name string
		op   api.Operation
		want int
	}{
		{"назначенный плательщик — уведомили, значит непрочитано", payerAssigned, 1},
		{"свой расход с чужим плательщиком — уведомили не меня", authoredByMe, 0},
		{"легаси без списка — правило по долям", legacy, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fetchNotifications(t, notifServerWithOps(t, tt.op), me.ID)
			if len(got.Items) != 1 {
				t.Fatalf("лента показывает событие всегда, получено %d", len(got.Items))
			}
			if got.UnreadCount != tt.want {
				t.Fatalf("непрочитанных должно быть %d, получено %d", tt.want, got.UnreadCount)
			}
		})
	}
}

// TestNotificationsExactCeilingIsNotOverflow — граница 99/100. Ровно 99
// непрочитанных — честное число, и клампить его нельзя: клиент нарисовал бы
// «99+» там, где счёт точный. Сотый переводит счётчик в маркер переполнения.
func TestNotificationsExactCeilingIsNotOverflow(t *testing.T) {
	// Литерал, а не константа: «99+» на обоих клиентах зашит числом
	// (MainTabView.badgeLabel, MainScaffold.badgeLabel), и сдвиг потолка на
	// сервере молча превратил бы точный счёт в потолок.
	if maxUnreadCount != 99 {
		t.Fatalf("потолок счётчика изменился (%d) — клиенты рисуют «99+» по 99", maxUnreadCount)
	}

	donor := testUser1
	op := func(i int) api.Operation {
		return api.Operation{
			ID: primitive.NewObjectID(), Description: fmt.Sprintf("Расход %d", i), Sum: 100,
			Donor:             &donor,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
			NotificationSent:  []int{testUser2.ID},
			CreateAt:          time.Now(),
		}
	}

	exact := make([]api.Operation, 0, 99)
	for i := 0; i < 99; i++ {
		exact = append(exact, op(i))
	}
	if got := fetchNotifications(t, notifServerWithOps(t, exact...), testUser2.ID); got.UnreadCount != 99 {
		t.Fatalf("ровно 99 непрочитанных обязаны отдаваться точным числом, получено %d", got.UnreadCount)
	}

	over := append(exact, op(99))
	if got := fetchNotifications(t, notifServerWithOps(t, over...), testUser2.ID); got.UnreadCount != 100 {
		t.Fatalf("сотое непрочитанное обязано давать маркер переполнения 100, получено %d", got.UnreadCount)
	}
}

// TestNotificationsSeenThroughTakenBeforeFeedRead — seenThrough обязан быть
// снят ДО чтения ленты. Иначе событие, созданное между чтением и снимком
// времени, в ответ не попадает, но клиентская отметка его гасит.
func TestNotificationsSeenThroughTakenBeforeFeedRead(t *testing.T) {
	srv, _, _ := notifFixture(t)

	// Часы тикают на каждом обращении: так порядок вызовов виден по значениям.
	base := time.Now()
	var tick int
	srv.now = func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	}
	var readAt time.Time
	srv.roomRepo.(*fakeRoomRepo).onFindRooms = func() { readAt = srv.now() }

	got := fetchNotifications(t, srv, testUser2.ID)
	if readAt.IsZero() {
		t.Fatal("лента не читалась — тест не проверяет порядок")
	}
	if !got.SeenThrough.Before(readAt) {
		t.Fatalf("seenThrough (%v) снят не раньше чтения ленты (%v): всё, что появится между ними, погаснет непоказанным",
			got.SeenThrough, readAt)
	}
}

// TestNotificationsOverflowIsDistinguishable — переполнение обязано отличаться
// от честной сотни минус один: ровно 99 клиент нарисовал бы как точное число.
func TestNotificationsOverflowIsDistinguishable(t *testing.T) {
	donor := testUser1
	ops := make([]api.Operation, 0, 120)
	for i := 0; i < 120; i++ {
		ops = append(ops, api.Operation{
			ID: primitive.NewObjectID(), Description: fmt.Sprintf("Расход %d", i), Sum: 100,
			Donor:             &donor,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
			CreateAt:          time.Now(),
		})
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2}, Operations: &ops,
		CreateAt: time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())

	got := fetchNotifications(t, srv, testUser2.ID)
	if got.UnreadCount != unreadOverflow {
		t.Fatalf("переполнение должно отдаваться как %d («больше 99»), получено %d", unreadOverflow, got.UnreadCount)
	}
}

// TestNotificationsPagination — лента листается теми же limit/offset, что и
// /activity: хендлер, игнорирующий offset, обязан падать здесь.
func TestNotificationsPagination(t *testing.T) {
	donor := testUser1
	ops := make([]api.Operation, 0, 3)
	for i := 0; i < 3; i++ {
		ops = append(ops, api.Operation{
			ID: primitive.NewObjectID(), Description: fmt.Sprintf("Расход %d", i), Sum: 100,
			Donor:             &donor,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
			// Разное время: порядок ленты обязан быть предсказуем.
			CreateAt: time.Now().Add(time.Duration(-i) * time.Hour),
		})
	}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2}, Operations: &ops,
		CreateAt: time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())
	token := mustToken(t, srv, testUser2.ID)

	page := func(query string) notificationsDto {
		t.Helper()
		rec := doRequest(t, srv, http.MethodGet, "/api/v1/notifications"+query, token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("ожидался 200, получен %d", rec.Code)
		}
		var out notificationsDto
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("не удалось разобрать ответ: %v", err)
		}
		return out
	}

	first := page("?limit=2&offset=0")
	if len(first.Items) != 2 {
		t.Fatalf("первая страница: ожидалось 2 события, получено %d", len(first.Items))
	}
	second := page("?limit=2&offset=2")
	if len(second.Items) != 1 {
		t.Fatalf("вторая страница: ожидалось 1 событие, получено %d", len(second.Items))
	}
	if second.Items[0].Operation.ID == first.Items[0].Operation.ID {
		t.Fatal("offset проигнорирован: вторая страница повторяет первую")
	}
}

// TestNotificationsEmptyState — пустой раздел отдаёт пустые массивы, а не null:
// клиенты декодируют их в списки.
func TestNotificationsEmptyState(t *testing.T) {
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	srv.SetInvites(newFakeInviteStore())

	got := fetchNotifications(t, srv, testUser1.ID)
	if got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("items должны быть пустым списком, получено %+v", got.Items)
	}
	if got.Invites == nil || len(got.Invites) != 0 {
		t.Fatalf("invites должны быть пустым списком, получено %+v", got.Invites)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("непрочитанных быть не должно, получено %d", got.UnreadCount)
	}
}

// TestNotificationsInviteToDeletedRoom — комнату удалили, а приглашение
// осталось: ответ обязан построиться, карточка приходит с пустым названием.
func TestNotificationsInviteToDeletedRoom(t *testing.T) {
	srv, invites, room := notifFixture(t)
	gone := primitive.NewObjectID()
	if err := invites.Upsert(context.Background(), gone, testUser2.ID, testUser1.ID, api.InvitePending, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}
	_ = room

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Invites) != 1 {
		t.Fatalf("ожидалась 1 карточка, получено %d", len(got.Invites))
	}
	if got.Invites[0].RoomName != "" {
		t.Fatalf("название удалённой комнаты взяться неоткуда, получено %q", got.Invites[0].RoomName)
	}
}

// TestNotificationsHidesAddedCardForNonMember — added у не-участника это не
// битые данные, а штатный исход гонки «добавили × вышел»: две записи в разных
// документах, транзакций нет. Показывать «вас добавили в группу» для группы,
// которой человек не видит, нельзя — карточка ведёт в никуда.
func TestNotificationsHidesAddedCardForNonMember(t *testing.T) {
	srv, invites, room := notifFixture(t)
	room.Members = &[]api.User{testUser1}
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InviteAdded, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Invites) != 0 {
		t.Fatalf("карточка показана не-участнику: %+v", got.Invites)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("бейдж поднят карточкой, которой не должно быть: %d", got.UnreadCount)
	}
}

// TestNotificationsShowsPendingToMemberAsAdded — обратная сторона того же
// правила: участнику выбор «Принять/Отклонить» не предлагаем. Членство у него
// уже есть, «Отклонить» из группы не выводит, а pending на участнике остаётся
// после отката неудавшегося accept или конкурентного входа по ссылке.
func TestNotificationsShowsPendingToMemberAsAdded(t *testing.T) {
	srv, invites, room := notifFixture(t)
	if err := invites.Upsert(context.Background(), room.ID, testUser2.ID, testUser1.ID, api.InvitePending, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения: %v", err)
	}

	got := fetchNotifications(t, srv, testUser2.ID)
	if len(got.Invites) != 1 {
		t.Fatalf("ожидалась 1 карточка, получено %d", len(got.Invites))
	}
	if got.Invites[0].Status != api.InviteAdded {
		t.Fatalf("участнику показан статус %q — он увидит кнопки «Принять/Отклонить» для своей же группы",
			got.Invites[0].Status)
	}
}
