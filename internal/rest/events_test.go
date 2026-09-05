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

// --- Анонимный маршрут: всё, что до входа ---

func postAnonymous(t *testing.T, s *Server, body string) (int, eventsResponse) {
	t.Helper()
	rr := doRequest(t, s, http.MethodPost, "/api/v1/events/anonymous", "", body)
	var out eventsResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("не разобрал ответ: %v (%s)", err, rr.Body.String())
		}
	}
	return rr.Code, out
}

func anonEvent(name, id string, params string) string {
	at := time.Now().UTC().Format(time.RFC3339)
	p := ""
	if params != "" {
		p = `,"params":` + params
	}
	return `{"id":"` + id + `","name":"` + name + `","session":"s-1","platform":"ios","at":"` + at + `"` + p + `}`
}

// Событие до входа пишется БЕЗ токена и без номера человека: экран входа
// человека ещё не знает, и знаменателя у login_completed иначе не существует.
func TestAnonymousEventsStoredWithoutToken(t *testing.T) {
	s, store := newEventsServer(t)

	code, got := postAnonymous(t, s, `{"device":"dev-1","events":[
		`+anonEvent("app_open", "a-1", `{"cold":"true"}`)+`,
		`+anonEvent("login_shown", "a-2", "")+`,
		`+anonEvent("login_started", "a-3", `{"method":"google"}`)+`
	]}`)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Accepted != 3 {
		t.Fatalf("принято %d, ожидал 3 (%+v)", got.Accepted, got)
	}
	for _, e := range store.events {
		if e.UserID != 0 {
			t.Errorf("анонимное событие записано на человека %d", e.UserID)
		}
		if e.DeviceID != "dev-1" {
			t.Errorf("устройство %q, ожидалось dev-1", e.DeviceID)
		}
	}
}

// Именные события через анонимный маршрут не проходят. Маршрут открыт всему
// интернету: пусти сюда room_created — и коллекцию засорят бесплатно, а в
// агрегате появятся тусы, которых никто не создавал.
func TestAnonymousEventsAcceptOnlyPreLoginNames(t *testing.T) {
	s, store := newEventsServer(t)

	code, got := postAnonymous(t, s, `{"device":"dev-1","events":[
		`+anonEvent("login_shown", "a-1", "")+`,
		`+anonEvent("room_created", "a-2", "")+`,
		`+anonEvent("purchase_completed", "a-3", `{"product":"yearly"}`)+`
	]}`)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Accepted != 1 || got.Rejected != 2 {
		t.Fatalf("принято %d, отвергнуто %d — ожидал 1 и 2", got.Accepted, got.Rejected)
	}
	if len(store.events) != 1 || store.events[0].Name != "login_shown" {
		t.Errorf("в журнал уехало не то: %+v", store.events)
	}
}

// Причина неудачи входа — из закрытого множества, как и везде.
func TestAnonymousEventsValidateLoginFailure(t *testing.T) {
	s, _ := newEventsServer(t)

	code, got := postAnonymous(t, s, `{"device":"dev-1","events":[
		`+anonEvent("login_failed", "a-1", `{"method":"google","reason":"cancelled"}`)+`,
		`+anonEvent("login_failed", "a-2", `{"method":"google","reason":"человек передумал"}`)+`
	]}`)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Accepted != 1 || got.Rejected != 1 {
		t.Errorf("принято %d, отвергнуто %d — ожидал 1 и 1", got.Accepted, got.Rejected)
	}
}

// Без устройства считать нечего: пустая строка проходит проверку формата, и
// без отдельной проверки такие события легли бы в одну безымянную кучу.
func TestAnonymousEventsRequireDevice(t *testing.T) {
	s, _ := newEventsServer(t)

	for _, body := range []string{
		`{"events":[` + anonEvent("login_shown", "a-1", "") + `]}`,
		`{"device":"","events":[` + anonEvent("login_shown", "a-1", "") + `]}`,
		`{"device":"плохое устройство","events":[` + anonEvent("login_shown", "a-1", "") + `]}`,
	} {
		if code, _ := postAnonymous(t, s, body); code != http.StatusBadRequest {
			t.Errorf("status %d на теле %s, ожидал 400", code, body)
		}
	}
}

// Поток открыт наружу, поэтому лимит на устройство ниже, чем на человека.
func TestAnonymousEventsRateLimited(t *testing.T) {
	s, _ := newEventsServer(t)

	sent := 0
	for i := 0; i < 40; i++ {
		body := `{"device":"dev-1","events":[`
		for j := 0; j < 10; j++ {
			if j > 0 {
				body += ","
			}
			body += anonEvent("login_shown", fmt.Sprintf("a-%d-%d", i, j), "")
		}
		body += `]}`
		code, _ := postAnonymous(t, s, body)
		if code == http.StatusTooManyRequests {
			return
		}
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		sent += 10
	}
	t.Errorf("лимит не сработал: приняли %d событий с одного устройства", sent)
}
