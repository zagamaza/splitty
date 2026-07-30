package rest

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Лимиты перебора кода входа: код одноразовый и живёт 5 минут, но без
// ограничения частоты его 40-битное пространство (и тем более постоянный
// REVIEW_LOGIN_CODE) можно долбить сколько угодно.
const (
	// authCodePerIPPerMin — попыток обмена кода с одного адреса в минуту
	authCodePerIPPerMin = 10
	// authCodeFailuresPerMin — неудачных попыток на весь сервер в минуту.
	// Нужен потому, что за реверс-прокси X-Forwarded-For подделывается и
	// per-IP окно обходится сменой заголовка. Занимается ДО валидации кода
	// (атомарный резерв, иначе залп параллельных запросов проскочил бы между
	// проверкой и инкрементом) и возвращается при успешном входе.
	// ВАЖНО: исчерпанный бюджет НЕ отбивает запрос сразу — код всё равно
	// проверяется, и верный пропускается. Иначе один аноним, льющий мусорные
	// коды, выжигал бы бюджет и клал вход для всех — полный DoS авторизации
	authCodeFailuresPerMin = 100
	// oauthPerIPPerMin — попыток входа через внешнего провайдера (Google, Apple)
	// с одного адреса в минуту. Лимит здесь не про перебор — подделать подпись
	// провайдера нельзя, — а про стоимость: каждая попытка это разбор JWT и,
	// на промахе кеша ключей, поход к провайдеру. Порог выше кодового, потому
	// что за одним адресом сидит целый NAT, а честный вход укладывается в
	// одну-две попытки. Ключ обязан иметь СВОЙ префикс: общий с /auth/code
	// означал бы, что вход через Google выжигает бюджет входа по коду
	oauthPerIPPerMin = 20
)

// throttle — минутные окна счётчиков попыток в памяти процесса.
// Хватает одного инстанса: сервер запускается в единственном экземпляре,
// а распределённый лимит уже есть у AI-парсинга (service.RateLimiter)
type throttle struct {
	mu      sync.Mutex
	windows map[string]*throttleWindow
	now     func() time.Time // подменяется в тестах
}

type throttleWindow struct {
	count   int
	resetAt time.Time
}

func newThrottle() *throttle {
	return &throttle{windows: map[string]*throttleWindow{}, now: time.Now}
}

// allow инкрементирует счётчик key и сообщает, уложились ли в limit за минуту
func (t *throttle) allow(key string, limit int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	w := t.windows[key]
	if w == nil || now.After(w.resetAt) {
		// заодно выметаем протухшие окна: карта не растёт от разовых адресов
		for k, v := range t.windows {
			if now.After(v.resetAt) {
				delete(t.windows, k)
			}
		}
		w = &throttleWindow{resetAt: now.Add(time.Minute)}
		t.windows[key] = w
	}
	w.count++
	return w.count <= limit
}

// release возвращает в окно одну ранее занятую единицу (счётчик не уходит
// ниже нуля). Нужен для «резервирования» попытки до проверки: удачная попытка
// бюджет перебора не тратит
func (t *throttle) release(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if w := t.windows[key]; w != nil && w.count > 0 {
		w.count--
	}
}

// clientIP — адрес клиента для лимитов. За прокси реальный адрес приходит
// в X-Forwarded-For; заголовок подделываем, поэтому per-IP окно дополнено
// глобальным счётчиком неудач (см. authCodeFailuresPerMin)
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first := strings.TrimSpace(strings.Split(fwd, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
