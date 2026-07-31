package rest

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/oidc"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

// Привязка и отвязка способов входа. Файл соседствует с auth.go, а не с
// handlers.go: здесь та же работа с личностями — проверка подписи Telegram
// Login Widget (checkTelegramHash) и верификаторы OIDC живут рядом.

// Имена провайдеров берём из repository: одни и те же строки едут в пути
// эндпоинта и в маппинге на поля документа
const (
	providerTelegram = repository.IdentityTelegram
	providerGoogle   = repository.IdentityGoogle
	providerApple    = repository.IdentityApple
)

// identityTakenMessage — текст 409 при попытке привязать чужую личность.
// Слияние профилей вне объёма: снимки пользователей денормализованы по всем
// комнатам и операциям, поэтому «перенести данные» — отдельная большая задача
const identityTakenMessage = "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него."

// telegramUnlinkWarning — предупреждение, которое клиент обязан показать после
// отвязки telegram.
//
// ⚠️ Почему отвязка telegram — особый случай. У исторического пользователя
// _id == telegram_id. После отвязки его telegram_id вычищен, но _id остался
// прежним, и следующий /start в боте идёт так: UpsertTelegramUser →
// FindByTelegramID пусто → _id, равный telegram id, ЗАНЯТ его же собственным
// документом → номер берётся из аллокатора и заводится ВТОРОЙ, пустой профиль.
// Старые комнаты остаются на первом (он открывается прежним способом входа —
// Google/Apple), но привязать этот telegram обратно уже нельзя: личность
// занята вторым профилем, и /me/link/telegram ответит 409 identity_taken.
//
// [decision] Из двух разрешений, предусмотренных планом, выбран рекомендованный
// вариант (а): отвязку разрешаем, но честно предупреждаем и чистим chat_state,
// чтобы бот не подхватил незавершённый сценарий (см. clearChatState). Вариант
// (б) — запретить отвязку до появления слияния профилей — оставил бы человека,
// уходящего из телеграма, привязанным к нему навсегда, ради защиты от ситуации,
// в которую он попадает только если продолжит писать боту
const telegramUnlinkWarning = "Telegram отвязан. Если вы снова напишете боту, он заведёт отдельный новый профиль без ваших групп, " +
	"и привязать этот Telegram обратно к текущему аккаунту уже не получится."

// linkRequest — тело POST /me/link/{google|apple}.
//
// Nonce присылает только Apple-клиент (сырым, как и при входе): в токене лежит
// его SHA256. Для Google поле не используется — Google-клиент шлёт один idToken
type linkRequest struct {
	IdToken string `json:"idToken"`
	Nonce   string `json:"nonce"`
}

// linkResponseDto — ответ /me/link/*: актуальный профиль (с linkedProviders) и
// необязательное предупреждение для показа пользователю
type linkResponseDto struct {
	User    meDto  `json:"user"`
	Warning string `json:"warning,omitempty"`
}

// linkedProviders — привязанные способы входа в стабильном порядке.
// Пустой срез, а не nil: в json обязан быть [], а не null
func linkedProviders(u *api.User) []string {
	providers := make([]string, 0, 3)
	if u.HasTelegram() {
		providers = append(providers, providerTelegram)
	}
	if u.GoogleSub != "" {
		providers = append(providers, providerGoogle)
	}
	if u.AppleSub != "" {
		providers = append(providers, providerApple)
	}
	return providers
}

// handleLinkIdentity POST /api/v1/me/link/{provider} — привязка способа входа
// к ТЕКУЩЕМУ аккаунту (кто текущий — решает токен, а не тело запроса)
func (s *Server) handleLinkIdentity(w http.ResponseWriter, r *http.Request) {
	switch provider := r.PathValue("provider"); provider {
	case providerGoogle:
		s.linkOIDC(w, r, providerGoogle, s.cfg.GoogleVerifier, "вход через Google не сконфигурирован", "не удалось проверить токен Google")
	case providerApple:
		s.linkOIDC(w, r, providerApple, s.cfg.AppleVerifier, "вход через Apple не сконфигурирован", "не удалось проверить токен Apple")
	case providerTelegram:
		s.linkTelegram(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "неизвестный способ входа")
	}
}

// linkOIDC общая часть привязки Google и Apple: проверить id-токен провайдера и
// записать его sub текущему пользователю
func (s *Server) linkOIDC(w http.ResponseWriter, r *http.Request, provider string, verifier oidc.Verifier, unavailableMsg, rejectMsg string) {
	if verifier == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", unavailableMsg)
		return
	}

	var req linkRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	idToken := strings.TrimSpace(req.IdToken)
	if idToken == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле idToken обязательно")
		return
	}

	claims, err := verifier.Verify(r.Context(), idToken)
	if err != nil {
		// Причину наружу не отдаём — см. handleAuthGoogle
		log.Warn().Err(err).Str("provider", provider).Msg("id token rejected on link")
		writeError(w, http.StatusUnauthorized, "unauthorized", rejectMsg)
		return
	}
	// Nonce проверяем, КОГДА ОН ЕСТЬ В ТОКЕНЕ: его значение подписано провайдером
	// и подделать его нельзя, поэтому «есть claim → сверяем» защищает от replay
	// токена нашего же клиента (он всегда шлёт nonce), а токен, выпущенный без
	// nonce вовсе, проверять просто нечем
	if claims.Nonce != "" && !checkAppleNonce(req.Nonce, claims.Nonce) {
		log.Warn().Str("provider", provider).Msg("id token nonce mismatch on link")
		writeError(w, http.StatusUnauthorized, "unauthorized", rejectMsg)
		return
	}

	s.linkIdentity(w, r, provider, claims.Subject, func(ctx context.Context) (*api.User, error) {
		if provider == providerGoogle {
			return s.userRepo.FindByGoogleSub(ctx, claims.Subject)
		}
		return s.userRepo.FindByAppleSub(ctx, claims.Subject)
	})
}

// linkTelegram POST /api/v1/me/link/telegram — тело и проверки те же, что у
// входа через Telegram Login Widget: подпись плюс свежесть auth_date
// (подпись телеграма валидна вечно, единственная защита от replay — возраст)
func (s *Server) linkTelegram(w http.ResponseWriter, r *http.Request) {
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

	// Профиль (username/display_name) здесь намеренно не обновляем: привязка
	// отвечает за личность, а имя подтянет первый же апдейт бота
	// (UpsertTelegramUser) — так у привязки ровно один эффект
	s.linkIdentity(w, r, providerTelegram, req.Id, func(ctx context.Context) (*api.User, error) {
		return s.userRepo.FindByTelegramID(ctx, req.Id)
	})
}

// linkIdentity — общий хвост привязки: владелец личности уже проверен провайдером,
// осталось решить конфликт и записать. owner ищет живого владельца этой личности
func (s *Server) linkIdentity(w http.ResponseWriter, r *http.Request, provider string, value interface{},
	owner func(context.Context) (*api.User, error)) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	existing, err := owner(ctx)
	switch {
	case err == nil && existing.ID == userId:
		// Идемпотентность: та же личность уже привязана к этому же аккаунту.
		// Повтор запроса (ретрай клиента, второе нажатие) — не ошибка
		s.writeLinks(ctx, w, userId, "")
		return
	case err == nil:
		writeError(w, http.StatusConflict, "identity_taken", identityTakenMessage)
		return
	case err != mongo.ErrNoDocuments:
		log.Error().Err(err).Str("provider", provider).Msg("cannot find identity owner")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить способ входа")
		return
	}

	if err := s.userRepo.SetIdentity(ctx, userId, provider, value); err != nil {
		switch {
		case repository.IsDuplicateKey(err):
			// Гонку выиграл кто-то другой между поиском владельца и записью —
			// ответ тот же, что и при найденном владельце
			writeError(w, http.StatusConflict, "identity_taken", identityTakenMessage)
		case err == mongo.ErrNoDocuments:
			// Аккаунт удалён параллельным DELETE /me: писать личность на
			// tombstone нельзя (см. MongoUserRepository.SetIdentity)
			writeError(w, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
		default:
			log.Error().Err(err).Str("provider", provider).Msg("cannot set identity")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось привязать способ входа")
		}
		return
	}
	s.writeLinks(ctx, w, userId, "")
}

// handleUnlinkIdentity DELETE /api/v1/me/link/{provider} — отвязка способа входа
func (s *Server) handleUnlinkIdentity(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !repository.IsKnownIdentityProvider(provider) {
		writeError(w, http.StatusNotFound, "not_found", "неизвестный способ входа")
		return
	}

	ctx := r.Context()
	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	linked := linkedProviders(user)
	if !containsProvider(linked, provider) {
		// Идемпотентность, симметрично привязке: отвязывать нечего
		writeJSON(w, http.StatusOK, linkResponseDto{User: toMeDto(user)})
		return
	}
	if len(linked) < 2 {
		// Иначе аккаунт остался бы без единого способа войти: токен живёт 90
		// дней, после чего данные становятся недоступны навсегда
		writeError(w, http.StatusConflict, "last_identity",
			"Нельзя отвязать единственный способ входа. Сначала добавьте другой.")
		return
	}

	if err := s.userRepo.ClearIdentity(ctx, user.ID, provider); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
			return
		}
		log.Error().Err(err).Str("provider", provider).Msg("cannot clear identity")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось отвязать способ входа")
		return
	}

	warning := ""
	if provider == providerTelegram {
		warning = telegramUnlinkWarning
		// user — снимок ДО отвязки, и это намеренно: telegram id нужен, чтобы
		// удалить состояния, сохранённые по нему
		s.clearChatState(ctx, user)
	}
	s.writeLinks(ctx, w, user.ID, warning)
}

// clearChatState вычищает незавершённые сценарии бота после отвязки telegram.
//
// Без этого следующий /start подхватил бы чужое состояние: бот заведёт человеку
// НОВЫЙ профиль с синтетическим номером (см. telegramUnlinkWarning), а
// populateChatState ищет состояние по каноническому id с fallback на сырой
// telegram id — и у исторического пользователя, где _id == telegram id, этот
// fallback попал бы ровно в состояние старого профиля. Плюс chat_state хранит
// PII (текст расхода в CallbackData.ExternalData), которому после ухода из
// телеграма там делать нечего.
//
// Best-effort: чистка не может отменить уже выполненную отвязку, поэтому
// ошибка только логируется
func (s *Server) clearChatState(ctx context.Context, u *api.User) {
	if s.chatStates == nil {
		return
	}
	ids := []int{u.ID}
	if u.HasTelegram() && *u.TelegramID != u.ID {
		// состояния сохранялись и по сырому telegram id (исторические пути бота)
		ids = append(ids, *u.TelegramID)
	}
	for _, id := range ids {
		if err := s.chatStates.DeleteByUserId(ctx, id); err != nil {
			log.Warn().Err(err).Int("userId", id).Msg("не удалось очистить chat_state после отвязки telegram")
		}
	}
}

// writeLinks перечитывает пользователя и отдаёт актуальный список способов входа
func (s *Server) writeLinks(ctx context.Context, w http.ResponseWriter, userId int, warning string) {
	user, err := s.userRepo.FindById(ctx, userId)
	if err != nil {
		log.Error().Err(err).Msg("cannot read user after identity change")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}
	writeJSON(w, http.StatusOK, linkResponseDto{User: toMeDto(user), Warning: warning})
}

func containsProvider(providers []string, provider string) bool {
	for _, p := range providers {
		if p == provider {
			return true
		}
	}
	return false
}
