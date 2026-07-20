package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottleWindow(t *testing.T) {
	now := time.Now()
	th := newThrottle()
	th.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !th.allow("k", 3) {
			t.Fatalf("попытка %d должна проходить", i)
		}
	}
	if th.allow("k", 3) {
		t.Fatal("четвёртая попытка должна отсекаться")
	}

	// release возвращает занятую единицу. Отказавшая четвёртая попытка счётчик
	// всё равно занимает (окно остаётся «горячим»), поэтому мест снова станет
	// хватать после двух возвратов
	th.release("k")
	th.release("k")
	if !th.allow("k", 3) {
		t.Fatal("после release попытка должна проходить")
	}

	// окно минутное — через минуту счётчик обнуляется
	now = now.Add(61 * time.Second)
	if !th.allow("k", 3) {
		t.Fatal("после окна попытка должна проходить")
	}
}

// release не уводит счётчик в минус (иначе лишние возвраты раздували бы лимит)
func TestThrottleReleaseFloor(t *testing.T) {
	th := newThrottle()
	th.release("nope") // окна ещё нет — не паникуем
	th.allow("k", 1)
	th.release("k")
	th.release("k")
	if !th.allow("k", 1) || th.allow("k", 1) {
		t.Fatal("после лишних release лимит должен остаться прежним")
	}
}

// протухшие окна не копятся в карте (иначе перебор с разных адресов её растит)
func TestThrottleEvictsExpired(t *testing.T) {
	now := time.Now()
	th := newThrottle()
	th.now = func() time.Time { return now }

	th.allow("a", 1)
	th.allow("b", 1)
	now = now.Add(2 * time.Minute)
	th.allow("c", 1)

	if len(th.windows) != 1 {
		t.Fatalf("ожидалось 1 живое окно, got %d", len(th.windows))
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.5:53124"
	if got := clientIP(r); got != "10.0.0.5" {
		t.Errorf("RemoteAddr: got %q", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("X-Forwarded-For: got %q", got)
	}
}

// перебор кода входа с одного адреса упирается в лимит попыток
func TestAuthCodeThrottledPerIP(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())

	for i := 0; i < authCodePerIPPerMin; i++ {
		if rec := doRequest(t, s, "POST", "/api/v1/auth/code", "", `{"code":"WRONGCODE"}`); rec.Code != 401 {
			t.Fatalf("попытка %d: ожидался 401, got %d", i, rec.Code)
		}
	}
	if rec := doRequest(t, s, "POST", "/api/v1/auth/code", "", `{"code":"WRONGCODE"}`); rec.Code != 429 {
		t.Fatalf("после лимита ожидался 429, got %d", rec.Code)
	}
}

// Один шумный адрес, упёршийся в свой per-IP лимит, не должен выжигать общий
// бюджет: его 429 бюджет перебора не тратят, соседние адреса продолжают работать
func TestAuthCodePerIPRejectRefundsGlobalBudget(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	h := s.Handler()

	attempt := func(ip string) int {
		req := httptest.NewRequest("POST", "/api/v1/auth/code", strings.NewReader(`{"code":"WRONGCODE"}`))
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// шумный адрес долбит далеко за свой лимит: первые authCodePerIPPerMin
	// попыток тратят бюджет, остальные должны его возвращать
	for i := 0; i < authCodeFailuresPerMin*2; i++ {
		attempt("198.51.100.1")
	}

	// у соседа должен остаться нетронутым весь его per-IP лимит
	for i := 0; i < authCodePerIPPerMin; i++ {
		if code := attempt("203.0.113.7"); code == http.StatusTooManyRequests {
			t.Fatalf("попытка %d соседнего адреса отбита: шумный IP выжег общий бюджет", i)
		}
	}
}

// подделка X-Forwarded-For обходит окно на адрес, но упирается в общий бюджет неудач
func TestAuthCodeGlobalFailureBudget(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	h := s.Handler()

	attempt := func(ip string) int {
		req := httptest.NewRequest("POST", "/api/v1/auth/code", strings.NewReader(`{"code":"WRONGCODE"}`))
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	blocked := false
	for i := 0; i < authCodeFailuresPerMin+5; i++ {
		if attempt(fmt.Sprintf("198.51.100.%d", i%256)) == 429 {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatal("общий бюджет неудач должен отсекать перебор со сменой X-Forwarded-For")
	}
}

// Залп параллельных запросов с уникальными X-Forwarded-For не проскакивает
// мимо общего бюджета: попытка занимается тем же инкрементом, что и проверяет
func TestAuthCodeGlobalBudgetUnderConcurrency(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	h := s.Handler()

	const burst = authCodeFailuresPerMin * 3
	var wg sync.WaitGroup
	var passed int64
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/v1/auth/code", strings.NewReader(`{"code":"WRONGCODE"}`))
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d.%d", i/256, i%256))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusTooManyRequests {
				atomic.AddInt64(&passed, 1)
			}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&passed); got > authCodeFailuresPerMin {
		t.Fatalf("сквозь бюджет %d прошло %d параллельных попыток", authCodeFailuresPerMin, got)
	}
}
