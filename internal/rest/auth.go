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
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/golang-jwt/jwt/v5"
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
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(_ *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(claims.Subject)
}

// auth — middleware: Bearer-токен → userId в context
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "требуется авторизация")
			return
		}
		userId, err := s.parseToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "невалидный токен")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUserId, userId)))
	})
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

// handleAuthTelegram POST /api/v1/auth/telegram — вход через Telegram Login Widget
func (s *Server) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TgToken == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telegram-авторизация не сконфигурирована")
		return
	}

	var req telegramAuthRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if req.Id == 0 || req.Hash == "" || req.AuthDate == 0 {
		writeError(w, http.StatusBadRequest, "validation", "поля id, authDate и hash обязательны")
		return
	}
	if !checkTelegramHash(req, s.cfg.TgToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "неверная подпись telegram")
		return
	}
	if time.Since(time.Unix(req.AuthDate, 0)) > maxAuthAge {
		writeError(w, http.StatusUnauthorized, "unauthorized", "данные авторизации устарели")
		return
	}

	displayName := strings.TrimSpace(strings.TrimSpace(req.FirstName) + " " + strings.TrimSpace(req.LastName))
	s.finishAuth(w, r, api.User{ID: req.Id, Username: req.Username, DisplayName: displayName})
}

type codeAuthRequest struct {
	Code string `json:"code"`
}

// handleAuthCode POST /api/v1/auth/code — вход по одноразовому коду,
// выданному командой /login в личном чате телеграм-бота.
// Код регистронезависим; проверка и пометка used атомарны (FindOneAndUpdate),
// поэтому конкурентные запросы с одним кодом дают ровно один успешный вход
func (s *Server) handleAuthCode(w http.ResponseWriter, r *http.Request) {
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

	lc, err := s.loginCodeRepo.UseLoginCode(r.Context(), code, time.Now())
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusUnauthorized, "invalid_code", "неверный, просроченный или уже использованный код")
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
			writeError(w, http.StatusUnauthorized, "invalid_code", "неверный, просроченный или уже использованный код")
			return
		}
		log.Error().Err(err).Msg("cannot find user by login code")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}

	s.finishAuth(w, r, api.User{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName})
}

type devAuthRequest struct {
	UserId      int    `json:"userId"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
}

// handleAuthDev POST /api/v1/auth/dev — вход для разработки, только при API_DEV_AUTH=true
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

	s.finishAuth(w, r, api.User{ID: req.UserId, Username: req.Username, DisplayName: req.DisplayName})
}

// finishAuth апсертит пользователя (сохраняя язык уже существующего) и отдаёт токен с профилем
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
		log.Error().Err(err).Msg("cannot upsert user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пользователя")
		return
	}

	token, err := s.issueToken(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("cannot issue token")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выпустить токен")
		return
	}

	writeJSON(w, http.StatusOK, authResponseDto{Token: token, User: toMeDto(user)})
}
