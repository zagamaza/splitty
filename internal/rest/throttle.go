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

// sweepEvery — как часто выметаются протухшие окна.
//
// Раньше карта обходилась целиком на КАЖДЫЙ новый ключ под общим мьютексом:
// при потоке разовых ключей это O(N) на запрос на замке, который делят
// /auth/code, /auth/google, /auth/apple, /me/link/* и /join. Раз в минуту
// достаточно — окно всё равно живёт минуту, и между уборками карта не может
// вырасти больше, чем прошло запросов за эту минуту
const sweepEvery = time.Minute

// throttle — минутные окна счётчиков попыток в памяти процесса.
// Хватает одного инстанса: сервер запускается в единственном экземпляре,
// а распределённый лимит уже есть у AI-парсинга (service.RateLimiter)
type throttle struct {
	mu        sync.Mutex
	windows   map[string]*throttleWindow
	nextSweep time.Time
	now       func() time.Time // подменяется в тестах
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
		t.sweep(now)
		w = &throttleWindow{resetAt: now.Add(time.Minute)}
		t.windows[key] = w
	}
	w.count++
	return w.count <= limit
}

// sweep выметает протухшие окна, но не чаще sweepEvery. Вызывается под мьютексом
func (t *throttle) sweep(now time.Time) {
	if now.Before(t.nextSweep) {
		return
	}
	t.nextSweep = now.Add(sweepEvery)
	for k, v := range t.windows {
		if now.After(v.resetAt) {
			delete(t.windows, k)
		}
	}
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

// clientIP — адрес клиента для лимитов.
//
// ⚠️ X-Forwarded-For НЕ ЧИТАЕТСЯ, пока не задан TRUSTED_PROXY_COUNT. Заголовок
// пишет кто угодно, и раньше первый его элемент брался безусловно — то есть
// любой per-IP лимит обходился случайным заголовком на каждый запрос. Для
// /auth/code это компенсировал глобальный счётчик неудач, а вот /join такого
// счётчика не имеет: страница приглашения — оракул существования комнаты по
// ObjectID, и «60 в минуту» для перебирающего означало отсутствие лимита.
//
// Как считается адрес при прокси: обратный прокси ДОПИСЫВАЕТ реальный адрес в
// КОНЕЦ списка (nginx $proxy_add_x_forwarded_for), поэтому доверять можно
// только хвосту — ровно tp последних элементов принадлежат нашим прокси, а
// нужный нам адрес стоит перед ними. Всё, что левее, пишет клиент, и оно
// игнорируется. Список короче ожидаемого (запрос пришёл в обход прокси) —
// откат к RemoteAddr
func clientIP(r *http.Request, trustedProxies int) string {
	if trustedProxies > 0 {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			if i := len(parts) - trustedProxies; i >= 0 && i < len(parts) {
				if ip := strings.TrimSpace(parts[i]); ip != "" {
					return ip
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientIP — адрес клиента с учётом настроенного числа доверенных прокси
func (s *Server) clientIP(r *http.Request) string {
	return clientIP(r, s.cfg.TrustedProxies)
}
