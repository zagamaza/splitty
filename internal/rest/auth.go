package rest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/oidc"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

// tokenTTL время жизни JWT — 90 дней
const tokenTTL = 90 * 24 * time.Hour

// maxAuthAge максимальный возраст auth_date из Telegram Login Widget.
// Окно короткое (10 минут), чтобы минимизировать replay перехваченного payload:
// подпись телеграма валидна сколь угодно долго, единственная защита — свежесть auth_date
const maxAuthAge = 10 * time.Minute

type ctxKey string

const ctxKeyUserId ctxKey = "userId"

// issueToken выпускает JWT (HS256) для пользователя
func (s *Server) issueToken(userId int) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.Itoa(userId),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JwtSecret))
}

// parseToken валидирует JWT и возвращает userId из claim sub
func (s *Server) parseToken(tokenStr string) (int, error) {
	userId, _, err := s.parseTokenWithIssuedAt(tokenStr)
	return userId, err
}

// parseTokenWithIssuedAt — то же, но отдаёт и дату выпуска: по ней middleware
// сверяется с отсечкой отзыва конкретного пользователя.
func (s *Server) parseTokenWithIssuedAt(tokenStr string) (int, time.Time, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(_ *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return 0, time.Time{}, err
	}
	// Отсечка по дате выпуска: токены старше её недействительны, даже если срок
	// ещё не вышел. Перехваченный у HTTP-сборок токен иначе жил бы до ноября, и
	// обновление приложения этого не отменяло
	if !s.cfg.TokenMinIssuedAt.IsZero() {
		if claims.IssuedAt == nil || claims.IssuedAt.Time.Before(s.cfg.TokenMinIssuedAt) {
			return 0, time.Time{}, errTokenTooOld
		}
	}
	var issuedAt time.Time
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}
	userId, err := strconv.Atoi(claims.Subject)
	return userId, issuedAt, err
}

// errTokenTooOld — токен выпущен раньше отсечки (см. Config.TokenMinIssuedAt).
var errTokenTooOld = errors.New("token issued before the configured cutoff")

// accountTTL сколько auth-middleware помнит вердикт «аккаунт жив/удалён».
// Компромисс: за минуту токен удалённого перестаёт работать везде, а обычный
// запрос платит один поход в mongo раз в минуту, а не на каждый вызов.
// Собственное удаление кеш не ждёт — handleDeleteMe чистит свою запись сам
const accountTTL = 60 * time.Second

// accountCacheMax — потолок числа записей. Кеш держит только int→(bool, time),
// но расти безгранично он не должен: при переполнении сбрасывается целиком
// (проще LRU и безопасно — потеря записи стоит одного запроса в mongo)
const accountCacheMax = 10000

// accountCache — кеш вердиктов auth-middleware о состоянии аккаунта.
//
// Зачем вообще проверка в middleware: currentUser вызывается лишь в 7 хендлерах
// из ~25, поэтому без неё токен удалённого пользователя ещё 90 дней открывал бы
// комнаты, операции и создание расходов — просто потому, что эти хендлеры
// канонический документ не читают
type accountCache struct {
	mu      sync.Mutex
	entries map[int]accountEntry
	ttl     time.Duration
	max     int
	now     func() time.Time // подменяется в тестах
}

type accountEntry struct {
	alive bool
	until time.Time
	// validFrom — отсечка отзыва токенов пользователя (nil — не отзывал).
	// Лежит в том же кеше: иначе проверка стоила бы похода в mongo на КАЖДЫЙ
	// авторизованный запрос
	validFrom *time.Time
}

func newAccountCache() *accountCache {
	return &accountCache{entries: map[int]accountEntry{}, ttl: accountTTL, max: accountCacheMax, now: time.Now}
}

// get возвращает вердикт и признак попадания в кеш
func (c *accountCache) get(userId int) (alive, hit bool) {
	e, _, hit := c.entry(userId)
	return e, hit
}

// entry возвращает вердикт вместе с отсечкой отзыва токенов.
func (c *accountCache) entry(userId int) (alive bool, validFrom *time.Time, hit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[userId]
	if !ok {
		return false, nil, false
	}
	if c.now().After(e.until) {
		delete(c.entries, userId)
		return false, nil, false
	}
	return e.alive, e.validFrom, true
}

func (c *accountCache) put(userId int, alive bool) {
	c.putState(userId, alive, nil)
}

func (c *accountCache) putState(userId int, alive bool, validFrom *time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.max {
		c.entries = map[int]accountEntry{}
	}
	c.entries[userId] = accountEntry{alive: alive, until: c.now().Add(c.ttl), validFrom: validFrom}
}

// forget убирает запись немедленно. Нужен handleDeleteMe: сам запрос DELETE /me
// проходит через middleware и прогревает кеш вердиктом «жив», поэтому без явной
// чистки удалённый пользователь ещё accountTTL ходил бы с рабочим токеном
func (c *accountCache) forget(userId int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, userId)
}

// auth — middleware: Bearer-токен → userId в context. Токен удалённого аккаунта
// отвергается (401)
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return s.authenticate(next, false)
}

// authDeleted — то же, но пропускает и удалённых. Нужен ровно одному маршруту,
// DELETE /me: удаление обязано быть повторяемым, а повторяет его тот самый
// пользователь, которого предыдущая попытка уже пометила tombstone
func (s *Server) authDeleted(next http.HandlerFunc) http.Handler {
	return s.authenticate(next, true)
}

func (s *Server) authenticate(next http.HandlerFunc, allowDeleted bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "требуется авторизация")
			return
		}
		userId, issuedAt, err := s.parseTokenWithIssuedAt(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "невалидный токен")
			return
		}
		if !allowDeleted {
			alive, validFrom, hErr := s.accountState(r.Context(), userId)
			if hErr != nil {
				hErr.write(w)
				return
			}
			if !alive {
				writeError(w, http.StatusUnauthorized, "unauthorized", "аккаунт удалён")
				return
			}
			// «Выйти на всех устройствах»: токены, выпущенные до отсечки,
			// больше не работают — украденный телефон переставал открывать
			// чужие расходы только сменой общего секрета, то есть разлогином всех
			if validFrom != nil && issuedAt.Before(*validFrom) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "сессия завершена")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUserId, userId)))
	})
}

// accountAlive — существует ли пользователь и не помечен ли он удалённым.
//
// Ошибка базы НЕ трактуется как «жив»: отвечаем 500, а не пропускаем запрос.
// Fail-open здесь ничего бы не спас (хендлеры всё равно ходят в ту же mongo),
// а fail-closed не даёт лежащей базе стать обходом инвалидации токена
func (s *Server) accountAlive(ctx context.Context, userId int) (bool, *httpError) {
	alive, _, hErr := s.accountState(ctx, userId)
	return alive, hErr
}

// accountState — вердикт «жив» плюс отсечка отзыва токенов. Оба факта берутся
// из одного документа и живут в одном кеше: разделять их значило бы ходить в
// mongo дважды на каждый авторизованный запрос
func (s *Server) accountState(ctx context.Context, userId int) (bool, *time.Time, *httpError) {
	if alive, validFrom, hit := s.accounts.entry(userId); hit {
		return alive, validFrom, nil
	}
	user, err := s.userRepo.FindById(ctx, userId)
	if err == mongo.ErrNoDocuments {
		s.accounts.put(userId, false)
		return false, nil, nil
	}
	if err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot check account status")
		return false, nil, &httpError{http.StatusInternalServerError, "internal", "не удалось проверить аккаунт"}
	}
	alive := !user.IsDeleted()
	s.accounts.putState(userId, alive, user.TokensValidFrom)
	return alive, user.TokensValidFrom, nil
}

// userIdFromCtx возвращает userId, положенный auth-middleware
func userIdFromCtx(ctx context.Context) int {
	userId, _ := ctx.Value(ctxKeyUserId).(int)
	return userId
}

type telegramAuthRequest struct {
	Id        int    `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Username  string `json:"username"`
	PhotoUrl  string `json:"photoUrl"`
	AuthDate  int64  `json:"authDate"`
	Hash      string `json:"hash"`
}

// checkTelegramHash валидирует подпись Telegram Login Widget:
// data-check-string из присутствующих полей, отсортированных по алфавиту,
// ключ — SHA256(TG_TOKEN), сравнение hex-подписей
func checkTelegramHash(req telegramAuthRequest, tgToken string) bool {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(req.AuthDate, 10),
		"id":        strconv.Itoa(req.Id),
	}
	if req.FirstName != "" {
		fields["first_name"] = req.FirstName
	}
	if req.LastName != "" {
		fields["last_name"] = req.LastName
	}
	if req.Username != "" {
		fields["username"] = req.Username
	}
	if req.PhotoUrl != "" {
		fields["photo_url"] = req.PhotoUrl
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, fields[k]))
	}
	dataCheckString := strings.Join(pairs, "\n")

	secret := sha256.Sum256([]byte(tgToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(dataCheckString))
	expected := hex.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(req.Hash))) == 1
}

// decodeTelegramAuth разбирает и проверяет данные Telegram Login Widget — общий
// вход и для входа (handleAuthTelegram), и для привязки (linkTelegram): проверки
// у них ровно одни и те же, а разъехавшись, они дали бы привязку слабее входа.
//
// Возвращает ok=false, когда ответ клиенту уже написан.
func (s *Server) decodeTelegramAuth(w http.ResponseWriter, r *http.Request) (telegramAuthRequest, bool) {
	var req telegramAuthRequest
	if s.cfg.TgToken == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telegram-авторизация не сконфигурирована")
		return req, false
	}
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return req, false
	}
	if req.Id == 0 || req.Hash == "" || req.AuthDate == 0 {
		writeError(w, http.StatusBadRequest, "validation", "поля id, authDate и hash обязательны")
		return req, false
	}
	if !checkTelegramHash(req, s.cfg.TgToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "неверная подпись telegram")
		return req, false
	}
	// Подпись телеграма валидна вечно — единственная защита от replay это возраст
	if time.Since(time.Unix(req.AuthDate, 0)) > maxAuthAge {
		writeError(w, http.StatusUnauthorized, "unauthorized", "данные авторизации устарели")
		return req, false
	}
	return req, true
}

// handleAuthTelegram POST /api/v1/auth/telegram — вход через Telegram Login Widget
func (s *Server) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeTelegramAuth(w, r)
	if !ok {
		return
	}

	displayName := strings.TrimSpace(strings.TrimSpace(req.FirstName) + " " + strings.TrimSpace(req.LastName))

	// Резолв идёт по telegram_id, а не по _id: req.Id — telegram id, и равен _id
	// он только у исторических аккаунтов. sub токена — _id НАЙДЕННОГО
	// пользователя, иначе у google-аккаунта с привязанным telegram завёлся бы
	// второй профиль под номером, равным telegram id.
	// Язык не передаём: Login Widget его не присылает, а затирать выбранный
	// пользователем нельзя (пустой userLang UpsertTelegramUser игнорирует)
	user, err := s.userRepo.UpsertTelegramUser(r.Context(), req.Id, req.Username, displayName, "")
	if err != nil {
		log.Error().Err(err).Msg("cannot upsert telegram user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пользователя")
		return
	}
	s.respondWithToken(w, user)
}

type codeAuthRequest struct {
	Code string `json:"code"`
}

// respondWithToken выпускает JWT для пользователя и пишет стандартный
// auth-ответ (общий хвост всех способов входа).
func (s *Server) respondWithToken(w http.ResponseWriter, user *api.User) {
	token, err := s.issueToken(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("cannot issue token")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выпустить токен")
		return
	}
	writeJSON(w, http.StatusOK, authResponseDto{Token: token, User: toMeDto(user)})
}

// authFailKey ключ общего (на весь сервер) счётчика неудачных попыток входа по коду
const authFailKey = "auth_code_failures"

// writeInvalidCode отвечает на неудачную попытку входа. Текст один на все
// причины — не раскрываем, что именно не так. Единицу общего бюджета попытка
// уже заняла на входе в handleAuthCode и, будучи неудачной, не возвращает.
// budgetOK=false означает, что общий бюджет перебора исчерпан: код всё равно
// был проверен (верный бы прошёл), но раз он неверный — отвечаем 429
func (s *Server) writeInvalidCode(w http.ResponseWriter, budgetOK bool) {
	if !budgetOK {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}
	writeError(w, http.StatusUnauthorized, "invalid_code", "неверный, просроченный или уже использованный код")
}

// handleAuthCode: резолв по _id корректен и после перехода на telegram_id —
// lc.UserId это уже номер Splitty (код выдаётся боту, который знает канонического
// пользователя), а не telegram id.
//
// handleAuthCode POST /api/v1/auth/code — вход по одноразовому коду,
// выданному командой /login в личном чате телеграм-бота.
// Код регистронезависим; проверка и пометка used атомарны (FindOneAndUpdate),
// поэтому конкурентные запросы с одним кодом дают ровно один успешный вход
func (s *Server) handleAuthCode(w http.ResponseWriter, r *http.Request) {
	// Первым идёт per-IP окно: это единственный лимит, который вправе отбить
	// запрос до проверки кода. Он адресный — шумный клиент режет только себя
	if !s.authThrottle.allow("ip:"+s.clientIP(r), authCodePerIPPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}
	// Общий бюджет перебора занимаем тем же атомарным инкрементом, что и
	// проверяем (залп параллельных запросов не проскочит между check и
	// increment), но по его исчерпании запрос НЕ обрываем: иначе аноним с
	// мусорными кодами закрывал бы вход всем. Исчерпанный бюджет лишь превращает
	// неудачную попытку в 429 — верный код проходит в любом случае
	budgetOK := s.authThrottle.allow(authFailKey, authCodeFailuresPerMin)
	// Успешный вход бюджет перебора не тратит — резерв возвращаем
	authSucceeded := false
	defer func() {
		if authSucceeded {
			s.authThrottle.release(authFailKey)
		}
	}()

	var req codeAuthRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле code обязательно")
		return
	}

	// Многоразовый код ревьюеров App Store: логинит в выделенный демо-аккаунт
	// без Telegram и без пометки used (ревью может входить многократно).
	// Сравнение постоянного секрета — constant-time
	if s.cfg.ReviewLoginCode != "" && s.cfg.ReviewUserId != 0 &&
		subtle.ConstantTimeCompare([]byte(code), []byte(strings.ToUpper(s.cfg.ReviewLoginCode))) == 1 {
		reviewer, err := s.userRepo.FindById(r.Context(), s.cfg.ReviewUserId)
		if err != nil {
			log.Error().Err(err).Msg("review user not found")
			s.writeInvalidCode(w, budgetOK)
			return
		}
		authSucceeded = true
		s.respondWithToken(w, reviewer)
		return
	}

	lc, err := s.loginCodeRepo.UseLoginCode(r.Context(), code, time.Now())
	if err != nil {
		if err == mongo.ErrNoDocuments {
			s.writeInvalidCode(w, budgetOK)
			return
		}
		log.Error().Err(err).Msg("cannot use login code")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить код")
		return
	}

	// пользователь обязан существовать (код выдаётся только написавшему боту /login);
	// его отсутствие трактуем как невалидный код, не раскрывая деталей
	user, err := s.userRepo.FindById(r.Context(), lc.UserId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			s.writeInvalidCode(w, budgetOK)
			return
		}
		log.Error().Err(err).Msg("cannot find user by login code")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}

	// Апсерт здесь не нужен: пользователь только что прочитан, и записывать
	// обратно ровно то же самое (finishAuth) значило бы лишний раз сходить в
	// базу и рискнуть затереть поля, которых нет в собранном api.User.
	// Остальные способы входа тоже отвечают через respondWithToken
	authSucceeded = true
	s.respondWithToken(w, user)
}

type googleAuthRequest struct {
	IdToken string `json:"idToken"`
}

// identityAuthAttempts — сколько раз вход через внешнего провайдера
// переспрашивает базу на duplicate key. Двух хватает на любую реальную гонку
// (проигравший видит документ победителя со второй попытки), третья — запас
const identityAuthAttempts = 3

// handleAuthGoogle POST /api/v1/auth/google — вход по ID-токену Google.
// Тело: {"idToken":"…"}. Пользователь ищется по google_sub; не нашли — заводим
// нового с синтетическим номером из аллокатора (telegram id у него нет)
func (s *Server) handleAuthGoogle(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GoogleVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "вход через Google не сконфигурирован")
		return
	}
	// Ключ троттлинга с собственным префиксом: "ip:"+clientIP, как у /auth/code
	// (см. handleAuthCode), означал бы общий бюджет — вход через Google выжигал
	// бы попытки входа по коду с того же адреса и наоборот
	if !s.authThrottle.allow("google:"+s.clientIP(r), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

	var req googleAuthRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	idToken := strings.TrimSpace(req.IdToken)
	if idToken == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле idToken обязательно")
		return
	}

	claims, err := s.cfg.GoogleVerifier.Verify(r.Context(), idToken)
	if err != nil {
		// Причину наружу не отдаём: подпись, издатель, aud и срок — подсказки
		// тому, кто подбирает токен. Детали остаются в логах
		log.Warn().Err(err).Msg("google id token rejected")
		writeError(w, http.StatusUnauthorized, "unauthorized", "не удалось проверить токен Google")
		return
	}

	user, err := s.resolveGoogleUser(r.Context(), claims)
	if err != nil {
		log.Error().Err(err).Msg("cannot resolve google user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пользователя")
		return
	}
	s.respondWithToken(w, user)
}

// resolveGoogleUser находит пользователя по google_sub или заводит нового.
//
// Аккаунты по email НЕ склеиваются, даже при полном совпадении: почту меняют,
// Apple выдаёт relay-адрес, и доверять ей как идентификатору нельзя. Привязка
// google-личности к существующему аккаунту — только явная (см. /me/link/*)
func (s *Server) resolveGoogleUser(ctx context.Context, claims *oidc.Claims) (*api.User, error) {
	return s.resolveIdentityUser(ctx, "google",
		func(ctx context.Context) (*api.User, error) { return s.userRepo.FindByGoogleSub(ctx, claims.Subject) },
		func(id int) api.User {
			return api.User{
				ID:          id,
				GoogleSub:   claims.Subject,
				Email:       claims.Email,
				DisplayName: strings.TrimSpace(claims.Name),
			}
		},
		nil)
}

// resolveIdentityUser — общий резолв «найти по личности провайдера или завести
// нового». Один на Google и Apple: логика не очевидная и переписывать её дважды
// значит однажды поправить только одну копию.
//
//   - find ищет живого владельца личности;
//   - build собирает документ нового пользователя по выданному номеру;
//   - onFound (может быть nil) дозаполняет НАЙДЕННОГО — Apple отдаёт email, имя
//     и свежий refresh token, которые надо дописать существующему профилю.
//
// ⚠️ Поиск по личности идёт первым шагом КАЖДОЙ итерации, а не только первой:
// duplicate key здесь означает гонку двух первых входов одного человека (номер
// из аллокатора атомарен и сам по себе не конфликтует), поэтому проигравший
// обязан подобрать документ, созданный победителем. Слепой retry «взять новый
// номер и вставить снова» упёрся бы в unique-индекс по google_sub/apple_sub все
// три раза и отдал клиенту 500
func (s *Server) resolveIdentityUser(ctx context.Context, provider string,
	find func(context.Context) (*api.User, error), build func(id int) api.User,
	onFound func(context.Context, *api.User) *api.User) (*api.User, error) {

	var lastErr error
	for attempt := 0; attempt < identityAuthAttempts; attempt++ {
		existing, err := find(ctx)
		if err == nil {
			if onFound != nil {
				return onFound(ctx, existing), nil
			}
			return existing, nil
		}
		if err != mongo.ErrNoDocuments {
			return nil, err
		}

		id, err := s.userIDs.NextUserID(ctx)
		if err != nil {
			return nil, err
		}
		err = s.userRepo.CreateIdentityUser(ctx, build(id))
		if err == nil {
			return s.userRepo.FindById(ctx, id)
		}
		if !repository.IsDuplicateKey(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, errors.Wrapf(lastErr, "не удалось создать пользователя %s за %d попыток", provider, identityAuthAttempts)
}

// appleAuthRequest — тело POST /auth/apple.
//
// Имени в ID-токене Apple нет: его отдают клиенту отдельным объектом и ТОЛЬКО
// при первом входе, поэтому displayName приезжает полем запроса. Nonce клиент
// присылает СЫРЫМ — в токене лежит его SHA256 (см. checkHashedNonce).
// authorizationCode нужен не входу, а удалению аккаунта: обменяв его на refresh
// token сейчас, мы сможем отозвать токены Apple потом (Guideline 5.1.1(v))
type appleAuthRequest struct {
	IdToken           string `json:"idToken"`
	DisplayName       string `json:"displayName"`
	Nonce             string `json:"nonce"`
	AuthorizationCode string `json:"authorizationCode"`
}

// handleAuthApple POST /api/v1/auth/apple — вход по ID-токену Sign in with Apple.
// Пользователь ищется по apple_sub; не нашли — заводим нового с синтетическим
// номером из аллокатора (telegram id у него нет)
func (s *Server) handleAuthApple(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AppleVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "вход через Apple не сконфигурирован")
		return
	}
	// Свой префикс ключа, как и у Google: общий с /auth/code бюджет означал бы,
	// что один способ входа выжигает попытки другого с того же адреса
	if !s.authThrottle.allow("apple:"+s.clientIP(r), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

	var req appleAuthRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	idToken := strings.TrimSpace(req.IdToken)
	if idToken == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле idToken обязательно")
		return
	}
	// Nonce обязателен: без него токен, перехваченный у другого приложения или
	// переигранный позже, принимался бы как свежий вход
	if strings.TrimSpace(req.Nonce) == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле nonce обязательно")
		return
	}

	claims, err := s.cfg.AppleVerifier.Verify(r.Context(), idToken)
	if err != nil {
		// Причину наружу не отдаём — см. handleAuthGoogle
		log.Warn().Err(err).Msg("apple id token rejected")
		writeError(w, http.StatusUnauthorized, "unauthorized", "не удалось проверить токен Apple")
		return
	}
	if !checkHashedNonce(req.Nonce, claims.Nonce) {
		log.Warn().Msg("apple id token nonce mismatch")
		writeError(w, http.StatusUnauthorized, "unauthorized", "не удалось проверить токен Apple")
		return
	}

	// Обмен кода стоит ДО резолва пользователя: свежий refresh token нужен и
	// новому (пишется при вставке), и существующему. Best-effort — см. exchangeAppleCode
	refreshToken := s.exchangeAppleCode(r.Context(), req.AuthorizationCode)

	user, err := s.resolveAppleUser(r.Context(), claims, req.DisplayName, refreshToken)
	if err != nil {
		log.Error().Err(err).Msg("cannot resolve apple user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пользователя")
		return
	}
	s.respondWithToken(w, user)
}

// checkHashedNonce сверяет сырой nonce клиента с тем, что провайдер положил в
// токен. Специфики Apple здесь нет: тем же способом проверяется nonce Google
// при привязке (см. linkOIDC).
//
// Клиент генерирует случайный nonce, кладёт в запрос к провайдеру его SHA256 в hex и
// присылает нам ОРИГИНАЛ — совпадение хешей доказывает, что токен выпущен по
// запросу именно этого клиента, а не переигран. Сравнение constant-time
func checkHashedNonce(rawNonce, tokenNonce string) bool {
	sum := sha256.Sum256([]byte(rawNonce))
	expected := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(strings.TrimSpace(tokenNonce)))) == 1
}

// exchangeAppleCode меняет одноразовый authorizationCode на refresh token,
// который понадобится при удалении аккаунта (POST /auth/revoke у Apple).
//
// Best-effort по построению: пустой код, невыключенный ключ .p8 (AppleTokens ==
// nil — локальная разработка) или недоступный Apple дают пустую строку и Warn
// в лог. Вход обязан пройти в любом случае: человек не виноват, что машинерия
// отзыва временно недоступна
func (s *Server) exchangeAppleCode(ctx context.Context, code string) string {
	code = strings.TrimSpace(code)
	if code == "" || s.cfg.AppleTokens == nil {
		return ""
	}
	refreshToken, err := s.cfg.AppleTokens.ExchangeCode(ctx, code)
	if err != nil {
		log.Warn().Err(err).Msg("не удалось обменять authorizationCode Apple: отзыв токенов при удалении аккаунта будет невозможен")
		return ""
	}
	return refreshToken
}

// resolveAppleUser находит пользователя по apple_sub или заводит нового.
//
// Аккаунты по email НЕ склеиваются — тем более здесь: Apple по умолчанию
// выдаёт relay-адрес вида xxx@privaterelay.appleid.com. Адрес валиден и письма
// доходят, но принадлежит он паре «человек + приложение» и идентификатором быть
// не может (см. также resolveGoogleUser)
func (s *Server) resolveAppleUser(ctx context.Context, claims *oidc.Claims, displayName, refreshToken string) (*api.User, error) {
	return s.resolveIdentityUser(ctx, "apple",
		func(ctx context.Context) (*api.User, error) { return s.userRepo.FindByAppleSub(ctx, claims.Subject) },
		func(id int) api.User {
			return api.User{
				ID:       id,
				AppleSub: claims.Subject,
				// email и имя Apple присылает только сейчас, при первом входе —
				// другого шанса их сохранить не будет
				Email:             strings.TrimSpace(claims.Email),
				DisplayName:       strings.TrimSpace(displayName),
				AppleRefreshToken: refreshToken,
			}
		},
		func(ctx context.Context, u *api.User) *api.User {
			return s.fillAppleProfile(ctx, u, claims.Email, displayName, refreshToken)
		})
}

// fillAppleProfile дозаполняет профиль существующего пользователя.
//
// Email и имя Apple отдаёт ТОЛЬКО при первом входе, поэтому при повторных они
// приходят пустыми — записывать их нечем и нельзя. Непустое значение пишется
// лишь в ПУСТОЕ поле: провайдер вправе дать нам имя, которого мы не знаем, но
// не вправе переименовать человека, который уже назвался в Splitty сам.
// Refresh token, наоборот, обновляется всегда — каждый вход выдаёт новый.
//
// Ошибка записи не валит вход: аккаунт уже найден, а дозаполнение профиля и
// машинерия отзыва — не повод отказать человеку во входе
func (s *Server) fillAppleProfile(ctx context.Context, u *api.User, email, displayName, refreshToken string) *api.User {
	email, displayName = strings.TrimSpace(email), strings.TrimSpace(displayName)
	if u.Email != "" {
		email = ""
	}
	if u.DisplayName != "" {
		displayName = ""
	}
	if email == "" && displayName == "" && refreshToken == "" {
		return u
	}
	if err := s.userRepo.UpdateAppleProfile(ctx, u.ID, email, displayName, refreshToken); err != nil {
		log.Warn().Err(err).Int("userId", u.ID).Msg("не удалось дописать профиль Apple")
		return u
	}
	updated := *u
	if email != "" {
		updated.Email = email
	}
	if displayName != "" {
		updated.DisplayName = displayName
	}
	if refreshToken != "" {
		updated.AppleRefreshToken = refreshToken
	}
	return &updated
}

type devAuthRequest struct {
	UserId      int    `json:"userId"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
}

// handleAuthDev POST /api/v1/auth/dev — вход для разработки, только при API_DEV_AUTH=true.
//
// Резолв остаётся по _id (req.UserId — номер Splitty, а не telegram id), поэтому
// dev-пользователи создаются БЕЗ telegram_id. Следствия ожидаемые, не баги:
// telegram-уведомления им не идут (канал пропускается при пустом telegram_id),
// а /users/{id}/avatar отдаёт 404 — клиенты рисуют инициалы
func (s *Server) handleAuthDev(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DevAuth {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	var req devAuthRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if req.UserId == 0 {
		writeError(w, http.StatusBadRequest, "validation", "поле userId обязательно")
		return
	}

	// DevAuth помечает документ на диске: по содержимому dev-аккаунт неотличим
	// от ИСТОРИЧЕСКОГО telegram-пользователя (маленький _id, ни одного поля
	// личности), а бэкфилл telegram_id обязан трогать только вторых —
	// см. repository.BackfillTelegramID
	s.finishAuth(w, r, api.User{
		ID:          req.UserId,
		Username:    req.Username,
		DisplayName: req.DisplayName,
		DevAuth:     true,
	})
}

// finishAuth апсертит пользователя (сохраняя язык уже существующего) и отдаёт
// токен с профилем. Нужен только тем способам входа, которые ДЕЙСТВИТЕЛЬНО
// создают или обновляют документ (dev-вход): у остальных пользователь уже есть
// в базе, и им хватает respondWithToken
func (s *Server) finishAuth(w http.ResponseWriter, r *http.Request, u api.User) {
	ctx := r.Context()

	// язык подхватываем только при успешном чтении; ErrNoDocuments — новый пользователь.
	// Любая другая ошибка — 500: молчаливый upsert с пустым UserLang затёр бы язык пользователя
	existing, err := s.userRepo.FindById(ctx, u.ID)
	if err != nil && err != mongo.ErrNoDocuments {
		log.Error().Err(err).Msg("cannot find user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}
	if err == nil && existing != nil {
		u.UserLang = existing.UserLang
	}

	user, err := s.userRepo.UpsertUser(ctx, u)
	if err != nil {
		// аккаунт удалён: воскрешать его входом нельзя — dev-вход по тому же
		// номеру иначе вернул бы на tombstone имя и username
		if errors.Is(err, repository.ErrUserDeleted) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "аккаунт удалён")
			return
		}
		log.Error().Err(err).Msg("cannot upsert user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пользователя")
		return
	}

	s.respondWithToken(w, user)
}
