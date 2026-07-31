package rest

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

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

// identityAlreadyLinkedMessage — текст 409 при попытке привязать ВТОРУЮ
// личность того же провайдера.
//
// Молча перезаписывать первую нельзя. Для Apple это прямо про Guideline
// 5.1.1(v): перезапись отцепляет прежний apple_sub БЕЗ вызова auth/revoke,
// и Splitty навсегда остаётся в списке «Вход через Apple» того Apple ID, у
// которого доступа к аккаунту больше нет; заодно затирается его
// apple_refresh_token — отзывать потом нечем. Для Google эффект тише, но
// не лучше: человек думает, что ДОБАВИЛ второй способ входа, а на самом деле
// потерял первый. Оба клиента прячут кнопку «Привязать» у привязанного
// провайдера, но это UI, а не защита: сюда ходят и по прямому запросу
const identityAlreadyLinkedMessage = "К аккаунту уже привязан другой аккаунт этого способа входа. " +
	"Сначала отвяжите текущий."

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
// его SHA256. Для Google поле не используется — Google-клиент шлёт один idToken.
//
// AuthorizationCode — тоже только Apple и ровно по той же причине, что и у
// /auth/apple (см. appleAuthRequest): код одноразовый, живёт минуты и другого
// шанса получить его НЕТ. Без него у человека, который завёлся через
// Telegram/Google и привязал Apple здесь, окажется apple_sub без
// apple_refresh_token — и удаление аккаунта не сможет отозвать токены Apple
// (Guideline 5.1.1(v)). Именно эта группа пользователей и есть смысл экрана
// «Способы входа», и именно её проверяет ревью Apple
type linkRequest struct {
	IdToken           string `json:"idToken"`
	Nonce             string `json:"nonce"`
	AuthorizationCode string `json:"authorizationCode"`
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

// linkedIdentityValue — личность провайдера, УЖЕ привязанная к пользователю.
// Второй результат false — привязки этого провайдера у него нет.
//
// Типы возвращаемых значений обязаны совпадать с теми, что приезжают в
// linkIdentity (int у telegram, string у google/apple): сравнение идёт через
// interface{}, и int64 против int дал бы «не равно» на одинаковых числах
func linkedIdentityValue(u *api.User, provider string) (interface{}, bool) {
	switch provider {
	case providerTelegram:
		if u.HasTelegram() {
			return *u.TelegramID, true
		}
	case providerGoogle:
		if u.GoogleSub != "" {
			return u.GoogleSub, true
		}
	case providerApple:
		if u.AppleSub != "" {
			return u.AppleSub, true
		}
	}
	return nil, false
}

// handleLinkIdentity POST /api/v1/me/link/{provider} — привязка способа входа
// к ТЕКУЩЕМУ аккаунту (кто текущий — решает токен, а не тело запроса)
func (s *Server) handleLinkIdentity(w http.ResponseWriter, r *http.Request) {
	// Лимит тот же, что у /auth/google|apple, но ключ по ПОЛЬЗОВАТЕЛЮ, а не по
	// адресу: сюда попадают только запросы с валидным токеном, и подделать
	// ключ (в отличие от X-Forwarded-For) нельзя. Смысл тот же — каждая
	// попытка это разбор JWT и, на промахе кеша ключей, поход к провайдеру
	if !s.authThrottle.allow("link:"+strconv.Itoa(userIdFromCtx(r.Context())), oauthAttemptsPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, попробуйте позже")
		return
	}

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
		writeProviderRejected(w, rejectMsg)
		return
	}
	// Nonce проверяем, КОГДА ОН ЕСТЬ В ТОКЕНЕ: его значение подписано провайдером
	// и подделать его нельзя, поэтому «есть claim → сверяем» защищает от replay
	// токена нашего же клиента (он всегда шлёт nonce), а токен, выпущенный без
	// nonce вовсе, проверять просто нечем
	if claims.Nonce != "" && !checkHashedNonce(req.Nonce, claims.Nonce) {
		log.Warn().Str("provider", provider).Msg("id token nonce mismatch on link")
		writeProviderRejected(w, rejectMsg)
		return
	}

	ok := s.linkIdentity(w, r, provider, claims.Subject, func(ctx context.Context) (*api.User, error) {
		if provider == providerGoogle {
			return s.userRepo.FindByGoogleSub(ctx, claims.Subject)
		}
		return s.userRepo.FindByAppleSub(ctx, claims.Subject)
	})
	if !ok {
		return
	}

	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	if provider == providerApple {
		// Обмен строго ПОСЛЕ успешной привязки: на 409 (личность занята) код
		// тратить незачем, а после привязки он обязан быть обменян — иначе
		// отзывать при удалении аккаунта будет нечего (см. linkRequest)
		s.saveAppleLink(ctx, userId, claims.Email, s.exchangeAppleCode(ctx, req.AuthorizationCode))
	}
	s.writeLinks(ctx, w, userId, "")
}

// writeProviderRejected — ответ на ОТВЕРГНУТЫЙ ТОКЕН ПРОВАЙДЕРА.
//
// Статус намеренно 400, а не 401: 401 на всём остальном API означает «сессия
// Splitty мертва», и оба клиента по нему разлогинивают человека и чистят
// локальные данные (Android вместе с очередью неотправленных расходов). Токен
// Google/Apple же отвергается по совершенно бытовым причинам — разъехавшиеся
// часы, протухший токен, ещё не прописанный в GOOGLE_CLIENT_IDS aud, — и
// выкидывать из живой сессии за это нельзя. Код provider_rejected клиенты
// показывают текстом и остаются залогиненными
func writeProviderRejected(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "provider_rejected", message)
}

// saveAppleLink дописывает то, что приезжает вместе с привязкой Apple: email из
// токена (только в пустое поле — переименовывать и переадресовывать человека
// провайдер права не имеет, см. fillAppleProfile) и свежий refresh token.
//
// Best-effort: привязка уже состоялась, и откатывать её из-за неудавшегося
// дозаполнения профиля нельзя
func (s *Server) saveAppleLink(ctx context.Context, userId int, email, refreshToken string) {
	if strings.TrimSpace(email) == "" && refreshToken == "" {
		return
	}
	user, err := s.userRepo.FindById(ctx, userId)
	if err != nil {
		log.Warn().Err(err).Int("userId", userId).Msg("не удалось перечитать пользователя после привязки Apple")
		return
	}
	s.fillAppleProfile(ctx, user, email, "", refreshToken)
}

// linkTelegram POST /api/v1/me/link/telegram — тело и проверки те же, что у
// входа через Telegram Login Widget: подпись плюс свежесть auth_date
// (подпись телеграма валидна вечно, единственная защита от replay — возраст)
func (s *Server) linkTelegram(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeTelegramAuth(w, r)
	if !ok {
		return
	}

	// Профиль (username/display_name) здесь намеренно не обновляем: привязка
	// отвечает за личность, а имя подтянет первый же апдейт бота
	// (UpsertTelegramUser) — так у привязки ровно один эффект
	if s.linkIdentity(w, r, providerTelegram, req.Id, func(ctx context.Context) (*api.User, error) {
		return s.userRepo.FindByTelegramID(ctx, req.Id)
	}) {
		s.writeLinks(r.Context(), w, userIdFromCtx(r.Context()), "")
	}
}

// linkIdentity — общий хвост привязки: владелец личности уже проверен провайдером,
// осталось решить конфликт и записать. owner ищет живого владельца этой личности.
//
// Возвращает true, если личность теперь принадлежит текущему пользователю
// (включая идемпотентный повтор). Успешный ответ НЕ пишет: у Apple между
// привязкой и ответом есть ещё один шаг (обмен authorizationCode), а
// перечитывать пользователя до него бессмысленно. Ответ на любую неудачу пишет
// сам и возвращает false
func (s *Server) linkIdentity(w http.ResponseWriter, r *http.Request, provider string, value interface{},
	owner func(context.Context) (*api.User, error)) bool {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	existing, err := owner(ctx)
	switch {
	case err == nil && existing.ID == userId:
		// Идемпотентность: та же личность уже привязана к этому же аккаунту.
		// Повтор запроса (ретрай клиента, второе нажатие) — не ошибка
		return true
	case err == nil:
		writeError(w, http.StatusConflict, "identity_taken", identityTakenMessage)
		return false
	case err != mongo.ErrNoDocuments:
		log.Error().Err(err).Str("provider", provider).Msg("cannot find identity owner")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить способ входа")
		return false
	}

	// Личность свободна, но у ТЕКУЩЕГО аккаунта этот провайдер уже занят другим
	// значением: SetIdentity молча перезаписал бы его (см.
	// identityAlreadyLinkedMessage). Проверка именно здесь, а не раньше:
	// идемпотентный повтор выше уже отсеян, а «личность занята чужим профилем»
	// (identity_taken) — более точный ответ, чем этот
	me, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return false
	}
	if linked, ok := linkedIdentityValue(me, provider); ok && linked != value {
		writeError(w, http.StatusConflict, "identity_already_linked", identityAlreadyLinkedMessage)
		return false
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
		return false
	}
	return true
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
	if !slices.Contains(linked, provider) {
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

	// Отзыв токенов Apple — строго ДО ClearIdentity: тот вместе с apple_sub
	// вычищает и apple_refresh_token, после него отзывать уже нечем. Логика та
	// же, что при удалении аккаунта (см. revokeAppleTokens), и по той же
	// причине: держать у себя рабочий refresh token от личности, которой у
	// пользователя больше нет, — и лишний секрет, и повод отозвать при будущем
	// DELETE /me токен уже отвязанной личности
	if provider == providerApple {
		s.revokeAppleTokens(ctx, user)
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
	// Не подключённая коллекция — ошибка проводки, а не штатный случай: ровно
	// так на неё смотрит purgeUserData, только там её ещё и можно вернуть
	// вызывающему. Отменить уже выполненную отвязку нельзя, поэтому здесь
	// остаётся сказать об этом громко в лог, а не уйти молча
	if s.chatStates == nil {
		log.Error().Int("userId", u.ID).
			Msg("коллекция chat_state не подключена: состояния бота с PII остались после отвязки telegram")
		return
	}
	// Набор идентификаторов тот же, что при удалении аккаунта: состояния
	// сохранялись и по каноническому _id, и по сырому telegram id
	for _, id := range chatStateIDs(u) {
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
