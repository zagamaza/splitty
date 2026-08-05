package rest

import (
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

// Вход по email и паролю — четвёртый способ входа рядом с telegram, google и
// apple. Почта входа живёт в ОТДЕЛЬНОМ поле api.User.LoginEmail: api.User.Email
// заполняется best-effort из Google/Apple (Apple отдаёт relay-адрес),
// идентификатором не является, и unique-индекс на нём сломал бы вход тому, чей
// адрес совпал.
//
// Инфраструктуры отправки писем нет, поэтому нет ни подтверждения адреса, ни
// сброса пароля по почте. Забытый пароль восстанавливается только входом другим
// способом и заданием нового из профиля (POST /me/password).

const (
	// minPasswordLen — минимальная длина пароля в символах
	minPasswordLen = 8
	// maxPasswordBytes — предел bcrypt: всё после 72 байт он молча отбрасывает,
	// то есть длинные пароли совпадали бы по общему префиксу
	maxPasswordBytes = 72
)

// dummyPasswordHash — с ним сверяется пароль, когда аккаунта с таким адресом
// нет. Без этого ответ на неизвестный email возвращался бы заметно быстрее
// (bcrypt не считался), и по времени ответа проверялось бы существование почты
const dummyPasswordHash = "$2a$10$kIrxCn.fdgyg0ImMN5bYpeCCbJy4gJKW0uZj31vhMnmxd.mG1dOg2"

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type passwordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type setPasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// hasPasswordLogin — можно ли войти в аккаунт по паролю. Нужны ОБА поля: хеш без
// адреса войти не даёт, поэтому и способом входа не считается (иначе правило
// last_identity разрешило бы отвязать последний рабочий способ)
func hasPasswordLogin(u *api.User) bool {
	return u != nil && u.LoginEmail != "" && u.PasswordHash != ""
}

// writeInvalidCredentials — единственный ответ на неудачный вход по паролю.
// Неизвестный адрес и неверный пароль обязаны быть НЕОТЛИЧИМЫ (статус, код и
// текст совпадают побайтно): иначе эндпоинт становится оракулом регистрации
func writeInvalidCredentials(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "invalid_credentials", "неверный email или пароль")
}

// validatePassword проверяет длину нового пароля
func validatePassword(password string) *httpError {
	if utf8.RuneCountInString(password) < minPasswordLen {
		return &httpError{http.StatusBadRequest, "validation", "пароль должен быть не короче 8 символов"}
	}
	if len(password) > maxPasswordBytes {
		return &httpError{http.StatusBadRequest, "validation", "пароль слишком длинный"}
	}
	return nil
}

// handleAuthRegister POST /api/v1/auth/register — регистрация по email и паролю.
//
// Аккаунты не склеиваются: если тем же адресом человек потом войдёт через
// Google, получится второй профиль — ровно та же политика, что уже действует
// между Google и Apple (см. resolveGoogleUser)
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !s.authThrottle.allow("register:"+s.clientIP(r), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

	var req registerRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	email := repository.NormalizeLoginEmail(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "невалидный email")
		return
	}
	if hErr := validatePassword(req.Password); hErr != nil {
		hErr.write(w)
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле displayName обязательно")
		return
	}

	ctx := r.Context()
	// Проверка занятости до вставки — ради внятного 409 и чтобы не жечь номера
	// аллокатора. Настоящая защита от гонки — unique-индекс ниже
	switch _, err := s.userRepo.FindByLoginEmail(ctx, email); {
	case err == nil:
		writeError(w, http.StatusConflict, "email_taken", "этот email уже зарегистрирован")
		return
	case err != mongo.ErrNoDocuments:
		log.Error().Err(err).Msg("cannot check login email")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить email")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Error().Err(err).Msg("cannot hash password")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пароль")
		return
	}

	// Номер как у входа через Google/Apple: telegram id у такого аккаунта нет
	id, err := s.userIDs.NextUserID(ctx)
	if err != nil {
		log.Error().Err(err).Msg("cannot allocate user id")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось создать пользователя")
		return
	}
	err = s.userRepo.CreateIdentityUser(ctx, api.User{
		ID:           id,
		DisplayName:  displayName,
		LoginEmail:   email,
		PasswordHash: string(hash),
	})
	if err != nil {
		if repository.IsDuplicateKey(err) {
			writeError(w, http.StatusConflict, "email_taken", "этот email уже зарегистрирован")
			return
		}
		log.Error().Err(err).Msg("cannot create password user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось создать пользователя")
		return
	}

	user, err := s.userRepo.FindById(ctx, id)
	if err != nil {
		log.Error().Err(err).Msg("cannot read created user")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}
	s.respondWithToken(w, user)
}

// handleAuthPassword POST /api/v1/auth/login — вход по email и паролю
func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if !s.authThrottle.allow("pwd-login:"+s.clientIP(r), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

	var req passwordLoginRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}

	user, err := s.userRepo.FindByLoginEmail(r.Context(), req.Email)
	if err != nil && err != mongo.ErrNoDocuments {
		log.Error().Err(err).Msg("cannot find user by login email")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить пользователя")
		return
	}
	// Хеш сверяется ВСЕГДА, даже когда пользователя нет: см. dummyPasswordHash
	hash := dummyPasswordHash
	if err == nil && user.PasswordHash != "" {
		hash = user.PasswordHash
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil || !hasPasswordLogin(user) {
		writeInvalidCredentials(w)
		return
	}
	s.respondWithToken(w, user)
}

// handleSetPassword POST /api/v1/me/password — задать или сменить пароль.
//
// Текущий пароль обязателен, только если он уже есть. Забывший его входит любым
// другим способом, отвязывает пароль (DELETE /me/link/password — адрес остаётся
// за аккаунтом) и задаёт новый здесь: другого пути восстановления нет и по
// решению не будет — почту мы не отправляем
func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	// Ключ по пользователю, а не по адресу: запрос уже прошёл auth-middleware,
	// подделать id (в отличие от X-Forwarded-For) нельзя
	if !s.authThrottle.allow("pwd-change:"+strconv.Itoa(userId), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

	var req setPasswordRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if hErr := validatePassword(req.NewPassword); hErr != nil {
		hErr.write(w)
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}
	if user.PasswordHash != "" {
		// Статус 403, а не 401: 401 на этом API означает «сессия мертва», и оба
		// клиента по нему разлогинивают человека (см. writeProviderRejected)
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
			writeError(w, http.StatusForbidden, "invalid_password", "неверный текущий пароль")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Error().Err(err).Msg("cannot hash password")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пароль")
		return
	}
	if err := s.userRepo.SetPasswordHash(ctx, user.ID, string(hash)); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
			return
		}
		log.Error().Err(err).Msg("cannot set password")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить пароль")
		return
	}
	s.writeLinks(ctx, w, user.ID, "")
}
