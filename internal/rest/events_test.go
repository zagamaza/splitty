package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// newEventsServer — сервер с подключённым журналом событий и включённым приёмом.
func newEventsServer(t *testing.T) (*Server, *fakeProductEvents) {
	t.Helper()
	users := newFakeUserRepo(testUser1)
	s := newTestServer(Config{}, users, newFakeRoomRepo(newTestRoom()))
	events := &fakeProductEvents{}
	s.SetProductEvents(events)
	s.SetAnalyticsEnabled(true)
	return s, events
}

func postEvents(t *testing.T, s *Server, body string) (int, eventsResponse) {
	t.Helper()
	rr := doRequest(t, s, http.MethodPost, "/api/v1/events", mustToken(t, s, testUser1.ID), body)
	var out eventsResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("не разобрал ответ: %v (%s)", err, rr.Body.String())
		}
	}
	return rr.Code, out
}

// Пачка пишется, а номер человека берётся ИЗ ТОКЕНА. В теле его нет и быть не
// должно: иначе любой авторизованный клиент писал бы события от чужого имени.
func TestEventsStoredWithUserFromToken(t *testing.T) {
	s, store := newEventsServer(t)

	code, got := postEvents(t, s, `{"events":[
		{"id":"e-1","name":"app_open","session":"s-1","platform":"ios","at":"`+time.Now().UTC().Format(time.RFC3339)+`","params":{"cold":"true"}},
		{"id":"e-2","name":"room_created","session":"s-1","platform":"ios","at":"`+time.Now().UTC().Format(time.RFC3339)+`"}
	]}`)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Accepted != 2 {
		t.Errorf("принято %d, ожидал 2 (%+v)", got.Accepted, got)
	}
	if len(store.events) != 2 {
		t.Fatalf("в журнал уехало %d событий", len(store.events))
	}
	for _, e := range store.events {
		if e.UserID != testUser1.ID {
			t.Errorf("событие записано на %d вместо %d", e.UserID, testUser1.ID)
		}
	}
}

// Незнакомое имя не пишется, а считается отвергнутым — и не роняет пачку:
// у клиента может разъехаться одно поле, терять из-за него остальные незачем.
func TestEventsRejectUnknownNameButKeepRest(t *testing.T) {
	s, store := newEventsServer(t)

	_, got := postEvents(t, s, `{"events":[
		{"id":"e-1","name":"никогда_такого_не_было","session":"s-1","platform":"ios"},
		{"id":"e-2","name":"app_open","session":"s-1","platform":"ios"}
	]}`)
	if got.Rejected != 1 || got.Accepted != 1 {
		t.Errorf("получил %+v, ожидал 1 отвергнутое и 1 принятое", got)
	}
	if len(store.events) != 1 || store.events[0].Name != "app_open" {
		t.Errorf("в журнал уехало не то: %+v", store.events)
	}
}

// Значение вне закрытого множества — тот же отказ. Именно так в агрегаты
// попадает мусор, который потом не отличить от данных.
func TestEventsRejectValueOutsideSet(t *testing.T) {
	s, _ := newEventsServer(t)
	_, got := postEvents(t, s, `{"events":[{"id":"e-1","name":"app_open","session":"s-1","platform":"ios","params":{"cold":"может быть"}}]}`)
	if got.Rejected != 1 {
		t.Errorf("получил %+v, ожидал отказ", got)
	}
}

// Пустой id ломает дедуп молча, поэтому он обязателен. Формат тот же, что у
// clientOpId, но пустота здесь запрещена — в отличие от него.
func TestEventsRequireIdAndSession(t *testing.T) {
	s, _ := newEventsServer(t)
	for _, body := range []string{
		`{"events":[{"id":"","name":"app_open","session":"s-1","platform":"ios"}]}`,
		`{"events":[{"id":"e-1","name":"app_open","session":"","platform":"ios"}]}`,
		`{"events":[{"id":"e 1","name":"app_open","session":"s-1","platform":"ios"}]}`,
		`{"events":[{"id":"e-1","name":"app_open","session":"s-1","platform":"symbian"}]}`,
	} {
		if _, got := postEvents(t, s, body); got.Rejected != 1 {
			t.Errorf("%s → %+v, ожидал отказ", body, got)
		}
	}
}

// Часы телефона врут и переводятся руками. Событие «из 2035 года» испортило бы
// все графики разом, поэтому время зажимается в окно доверия.
func TestEventsClampClientClock(t *testing.T) {
	s, store := newEventsServer(t)
	now := time.Now().UTC()

	postEvents(t, s, `{"events":[
		{"id":"e-1","name":"app_open","session":"s-1","platform":"ios","at":"2035-01-01T00:00:00Z"},
		{"id":"e-2","name":"app_open","session":"s-1","platform":"ios","at":"2019-01-01T00:00:00Z"}
	]}`)

	if len(store.events) != 2 {
		t.Fatalf("записалось %d событий", len(store.events))
	}
	for _, e := range store.events {
		if e.At.Before(now.Add(-time.Minute)) || e.At.After(now.Add(time.Minute)) {
			t.Errorf("время не зажато: %s", e.At)
		}
	}
}

// Потолок пачки. Без него один запрос мог бы принести сколько угодно.
func TestEventsRejectOversizedBatch(t *testing.T) {
	s, _ := newEventsServer(t)
	items := make([]string, 0, maxEventsPerRequest+1)
	for i := 0; i <= maxEventsPerRequest; i++ {
		items = append(items, fmt.Sprintf(`{"id":"e-%d","name":"app_open","session":"s-1","platform":"ios"}`, i))
	}
	rr := doRequest(t, s, http.MethodPost, "/api/v1/events", mustToken(t, s, testUser1.ID),
		`{"events":[`+strings.Join(items, ",")+`]}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, ожидал 400", rr.Code)
	}
}

// Лимит списывает СОБЫТИЯ, а не запросы. Иначе пачка из полусотни обходит его
// ровно в пятьдесят раз: клиент льёт полсотни событий там, где счётчик видит
// одно обращение.
func TestEventsRateLimitCountsEventsNotRequests(t *testing.T) {
	s, store := newEventsServer(t)

	items := make([]string, 0, maxEventsPerRequest)
	for i := 0; i < maxEventsPerRequest; i++ {
		items = append(items, fmt.Sprintf(`{"id":"e-%d","name":"app_open","session":"s-1","platform":"ios"}`, i))
	}
	body := `{"events":[` + strings.Join(items, ",") + `]}`

	limited := false
	for i := 0; i < eventsPerUserPerMin/maxEventsPerRequest+2; i++ {
		rr := doRequest(t, s, http.MethodPost, "/api/v1/events", mustToken(t, s, testUser1.ID), body)
		if rr.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("лимит не сработал: записалось %d событий", len(store.events))
	}
}

// Выключенный приём отвечает «принято» и ничего не пишет. Отказ заставил бы
// клиента считать, что он не доставил, копить очередь и ретраить её вечно —
// то есть выключатель сам стал бы источником нагрузки.
func TestEventsDisabledAcceptsAndDropsSilently(t *testing.T) {
	s, store := newEventsServer(t)
	s.SetAnalyticsEnabled(false)

	code, got := postEvents(t, s, `{"events":[{"id":"e-1","name":"app_open","session":"s-1","platform":"ios"}]}`)
	if code != http.StatusOK {
		t.Fatalf("status %d, ожидал 200", code)
	}
	if got.Accepted != 1 {
		t.Errorf("получил %+v, ожидал «принято»", got)
	}
	if len(store.events) != 0 {
		t.Errorf("при выключенном приёме записалось %d событий", len(store.events))
	}
}

// Без авторизации маршрута нет: анонимного пути записи наружу не появляется.
func TestEventsRequireAuth(t *testing.T) {
	s, _ := newEventsServer(t)
	rr := doRequest(t, s, http.MethodPost, "/api/v1/events", "", `{"events":[]}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, ожидал 401", rr.Code)
	}
}
