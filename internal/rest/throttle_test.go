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
	tests := []struct {
		name           string
		fwd            string
		trustedProxies int
		want           string
	}{
		{name: "без заголовка", trustedProxies: 0, want: "10.0.0.5"},
		{
			// главное свойство: без TRUSTED_PROXY_COUNT заголовок не читается
			// вовсе, иначе любой per-IP лимит обходится случайным значением
			name: "заголовок не читается без доверенных прокси",
			fwd:  "203.0.113.7, 198.51.100.9", trustedProxies: 0, want: "10.0.0.5",
		},
		{
			// один прокси дописал реальный адрес в конец — берём его, а не то,
			// что клиент написал сам
			name: "подделка клиента отбрасывается",
			fwd:  "1.2.3.4, 203.0.113.7", trustedProxies: 1, want: "203.0.113.7",
		},
		{name: "клиент ничего не писал", fwd: "203.0.113.7", trustedProxies: 1, want: "203.0.113.7"},
		{name: "два прокси", fwd: "1.2.3.4, 203.0.113.7, 10.0.0.1", trustedProxies: 2, want: "203.0.113.7"},
		{
			// запрос пришёл в обход прокси: элементов меньше, чем ожидалось —
			// доверять нечему, откат к RemoteAddr
			name: "список короче ожидаемого",
			fwd:  "203.0.113.7", trustedProxies: 2, want: "10.0.0.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "/", nil)
			r.RemoteAddr = "10.0.0.5:53124"
			if tt.fwd != "" {
				r.Header.Set("X-Forwarded-For", tt.fwd)
			}
			if got := clientIP(r, tt.trustedProxies, defaultTrustedProxyNets); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Заголовок с ПРЯМОГО соединения не читается никогда.
//
// Порт сервера бывает доступен и напрямую — health-check по IP, забытое
// правило файрвола. Пока доверие определялось только числом хопов, такому
// запросу достаточно было прислать свой X-Forwarded-For, чтобы любой per-IP
// лимит считался по адресу, который выбрал сам перебирающий.
func TestClientIPIgnoresHeaderFromDirectConnection(t *testing.T) {
	r, _ := http.NewRequest("POST", "/", nil)
	r.RemoteAddr = "203.0.113.99:53124" // адрес из интернета, не прокси
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")

	got := clientIP(r, 1, defaultTrustedProxyNets)
	if got != "203.0.113.99" {
		t.Fatalf("адрес клиента %q — подделанный заголовок с прямого соединения приняли", got)
	}
}

// Запрос через прокси считается по адресу конечного клиента, а не общим ведром.
func TestClientIPUsesRealAddressBehindProxy(t *testing.T) {
	r, _ := http.NewRequest("POST", "/", nil)
	r.RemoteAddr = "172.18.0.3:40000" // docker-сеть: наш реверс-прокси
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.7")

	if got := clientIP(r, 1, defaultTrustedProxyNets); got != "203.0.113.7" {
		t.Fatalf("адрес клиента %q — лимиты считались бы одним ведром на всех", got)
	}
}

// Явно заданные подсети сужают доверие: прокси в docker-сети, а не любой
// приватный адрес.
func TestParseTrustedProxyNetsNarrowsTrust(t *testing.T) {
	nets := ParseTrustedProxyNets([]string{"172.18.0.0/16"})

	behindProxy, _ := http.NewRequest("POST", "/", nil)
	behindProxy.RemoteAddr = "172.18.0.3:40000"
	behindProxy.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(behindProxy, 1, nets); got != "203.0.113.7" {
		t.Fatalf("свой прокси не распознан: %q", got)
	}

	other, _ := http.NewRequest("POST", "/", nil)
	other.RemoteAddr = "10.9.9.9:40000" // приватный, но не наш
	other.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(other, 1, nets); got != "10.9.9.9" {
		t.Fatalf("заголовок принят от чужого адреса: %q", got)
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
		req.RemoteAddr = ip + ":40000"
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

// перебор с РАЗНЫХ адресов (ботнет, мобильный NAT) обходит окно на адрес,
// но упирается в общий бюджет неудач
func TestAuthCodeGlobalFailureBudget(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	h := s.Handler()

	attempt := func(ip string) int {
		req := httptest.NewRequest("POST", "/api/v1/auth/code", strings.NewReader(`{"code":"WRONGCODE"}`))
		req.RemoteAddr = ip + ":40000"
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
		t.Fatal("общий бюджет неудач должен отсекать перебор со сменой адреса")
	}
}

// Исчерпанный общий бюджет не должен класть авторизацию целиком: он режет
// только НЕУДАЧНЫЕ попытки. Иначе один аноним, льющий мусорные коды со
// сменой адреса, ~2 запросами в секунду закрывал бы вход всем
func TestAuthCodeGlobalBudgetDoesNotRejectValidCode(t *testing.T) {
	const reviewCode = "REVIEWCODE1234567890"
	s := newTestServer(Config{ReviewLoginCode: reviewCode, ReviewUserId: testUser1.ID},
		newFakeUserRepo(testUser1), newFakeRoomRepo())
	h := s.Handler()

	attempt := func(ip, code string) int {
		req := httptest.NewRequest("POST", "/api/v1/auth/code", strings.NewReader(`{"code":"`+code+`"}`))
		req.RemoteAddr = ip + ":40000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// выжигаем общий бюджет мусорными кодами с разных адресов
	for i := 0; i < authCodeFailuresPerMin*2; i++ {
		attempt(fmt.Sprintf("198.51.%d.%d", i/256, i%256), "WRONGCODE")
	}
	// мусор после исчерпания бюджета отбивается
	if got := attempt("198.51.100.250", "WRONGCODE"); got != http.StatusTooManyRequests {
		t.Fatalf("неверный код при исчерпанном бюджете: got %d, want 429", got)
	}
	// а верный код обязан пройти
	if got := attempt("203.0.113.7", reviewCode); got != http.StatusOK {
		t.Fatalf("верный код отбит исчерпанным бюджетом: got %d, want 200", got)
	}
}

// Залп параллельных запросов с уникальных адресов не проскакивает
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
			req.RemoteAddr = fmt.Sprintf("198.51.100.%d:%d", i%256, 40000+i)
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
