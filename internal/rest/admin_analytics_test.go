package rest

import (
	"net/http"
	"testing"
)

func analyticsAdminServer(t *testing.T) (*Server, *fakeProductEvents) {
	t.Helper()
	s := newTestServer(Config{AdminToken: testAdminToken}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	events := &fakeProductEvents{}
	s.SetProductEvents(events)
	return s, events
}

// Окно — из закрытого списка. Свободное число не принимается: панели незачем
// просить произвольное окно, а нам есть зачем не пускать в базу произвольный
// запрос. То же правило, по которому наружу торчат только именованные выборки.
func TestAdminAnalyticsWindowIsClosedList(t *testing.T) {
	s, events := analyticsAdminServer(t)

	for _, days := range []string{"7", "30", "90"} {
		if rr := doAdmin(t, s, "/admin/analytics/daily?days="+days, testAdminToken); rr.Code != http.StatusOK {
			t.Errorf("days=%s: status %d", days, rr.Code)
		}
	}
	for _, days := range []string{"", "1", "365", "0", "-1", "abc", "9999999"} {
		if rr := doAdmin(t, s, "/admin/analytics/daily?days="+days, testAdminToken); rr.Code != http.StatusBadRequest {
			t.Errorf("days=%q прошло со статусом %d", days, rr.Code)
		}
	}
	if events.lastDays != 90 {
		t.Errorf("в хранилище уехало окно %d", events.lastDays)
	}
}

// Лента — единственный блок, отдающий записи, а не агрегат: у неё жёсткий
// потолок строк, иначе окно в 90 дней вернуло бы всё разом.
func TestAdminAnalyticsFeedIsCapped(t *testing.T) {
	s, events := analyticsAdminServer(t)
	if rr := doAdmin(t, s, "/admin/analytics/feed?days=30", testAdminToken); rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	if events.lastLimit != analyticsFeedLimit {
		t.Errorf("потолок строк %d, ожидал %d", events.lastLimit, analyticsFeedLimit)
	}
}

// Блок «ошибки» — это daily с фильтром по имени, а не четвёртый именованный
// запрос: expense_parse_failed и соседи считаются теми же суточными счётчиками.
func TestAdminAnalyticsDailyFiltersByName(t *testing.T) {
	s, events := analyticsAdminServer(t)
	if rr := doAdmin(t, s, "/admin/analytics/daily?days=7&name=purchase_failed", testAdminToken); rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	if events.lastName != "purchase_failed" {
		t.Errorf("фильтр не доехал: %q", events.lastName)
	}
}

func TestAdminAnalyticsUnknownBlock(t *testing.T) {
	s, _ := analyticsAdminServer(t)
	if rr := doAdmin(t, s, "/admin/analytics/воронка?days=7", testAdminToken); rr.Code != http.StatusNotFound {
		t.Errorf("status %d, ожидал 404", rr.Code)
	}
}

// Без токена админского API наружу не отдаётся ничего.
func TestAdminAnalyticsRequiresToken(t *testing.T) {
	s, _ := analyticsAdminServer(t)
	if rr := doAdmin(t, s, "/admin/analytics/daily?days=7", ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, ожидал 401", rr.Code)
	}
}

// Журнал не подключён — 503, а не пустой ответ: «нет данных» и «нечем спросить»
// разные состояния, и путать их на экране нельзя.
func TestAdminAnalyticsWithoutStore(t *testing.T) {
	s := newTestServer(Config{AdminToken: testAdminToken}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	if rr := doAdmin(t, s, "/admin/analytics/daily?days=7", testAdminToken); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, ожидал 503", rr.Code)
	}
}
