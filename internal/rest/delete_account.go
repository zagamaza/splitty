package rest

import (
	"context"
	"net/http"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

// Удаление аккаунта (DELETE /api/v1/me) — обязательное требование Apple
// Guideline 5.1.1(v) для приложений с регистрацией.
//
// ⚠️ ПОРЯДОК ШАГОВ — суть этого файла, менять его нельзя:
//
//	(0) отзыв токенов Apple — строго ДО tombstone: SoftDeleteUser делает
//	    $unset apple_refresh_token, и после него отзывать уже нечем;
//	(1) чистка chat_state — тоже строго ДО tombstone: часть состояний бота
//	    сохранена под СЫРЫМ telegram id, а SoftDeleteUser вычищает telegram_id,
//	    и повторному вызову этот id взять было бы уже неоткуда (chatStateIDs);
//	(2) SoftDeleteUser — tombstone: с этого момента аккаунт недоступен;
//	(3) чистка кеша auth-middleware — иначе токен живёт ещё accountTTL;
//	(4) анонимизация встроенных снимков в комнатах;
//	(5) чистка побочных коллекций с PII;
//	(6) 204.
//
// Обратный порядок (4) → (2) недопустим: если анонимизация пройдёт, а tombstone
// упадёт (транзиентная ошибка mongo, отмена контекста), аккаунт останется ЖИВЫМ
// с затёртым во всех комнатах именем. Снимки из канонического документа не
// перестраиваются — это необратимая порча живого аккаунта, а не «повторный
// вызов доделает».
//
// Шаг (1), наоборот, ДО tombstone безопасен: chat_state — это незавершённый
// диалог с ботом, а не данные аккаунта. Упавший на нём запрос оставляет аккаунт
// живым (errCodeInternal), и человек теряет разве что недописанный расход —
// цена, несопоставимая с НАВСЕГДА застрявшим в базе телом его расходов, если
// сырой telegram id потерять вместе с tombstone.
//
// Шаги (4)-(5) повторяемы: запрос, упавший после (2), доводится до конца любым
// повторным DELETE /me (маршрут висит на authDeleted, см. server.go). Состояние
// между шагами безопасно — аккаунт уже недоступен, в комнатах пока видно старое
// имя.
//
// ⚠️ КОД ОШИБКИ РАЗЛИЧАЕТ «ДО» И «ПОСЛЕ» tombstone — на нём держится поведение
// клиента, и одинаковые коды тут стоили бы человеку данных:
//
//   - errCodeInternal — сбой ДО tombstone (шаги 0-2): аккаунт ЖИВ, ничего
//     необратимого не произошло. Клиент обязан оставить сессию и очередь
//     неотправленных расходов на месте (iOS раньше стирал их на любом 500:
//     транзиентный сбой mongo уносил офлайн-очередь при целом аккаунте);
//   - errCodePurgeIncomplete — сбой ПОСЛЕ tombstone (шаги 4-5): аккаунт уже
//     удалён, но PII осталась. Клиент обязан СОХРАНИТЬ токен: только им и можно
//     повторить запрос (authDeleted пускает удалённых, а войти заново нельзя —
//     SoftDeleteUser вычистил все личности). Выбросив токен, клиент навсегда
//     закрывает единственный путь доделать чистку.
const (
	errCodeInternal        = "internal"
	errCodePurgeIncomplete = "purge_incomplete"
)

// purgeIncompleteMessage — текст для клиента при сбое после tombstone.
const purgeIncompleteMessage = "аккаунт удалён, но очистка данных не завершена: повторите запрос"

// handleDeleteMe DELETE /api/v1/me — удаление аккаунта: tombstone, чистка PII,
// анонимизация снимков. Долги и суммы сохраняются: их id, значения и доли не
// меняются ни на одном шаге
func (s *Server) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	// Демо-аккаунт ревьюеров App Store удалять нельзя. Ревьюер проверяет
	// 5.1.1(v) именно нажатием кнопки «удалить аккаунт», и без этого запрета он
	// поставил бы tombstone на демо-аккаунт: REVIEW_LOGIN_CODE продолжил бы
	// выпускать токены (handleAuthCode ищет через FindById, tombstone найдётся),
	// а middleware их отвергал — демо-вход умер бы до ручной правки базы, и
	// следующее ревью провалилось бы
	if s.cfg.ReviewUserId != 0 && userId == s.cfg.ReviewUserId {
		writeError(w, http.StatusForbidden, "forbidden", "Демонстрационный аккаунт удалить нельзя")
		return
	}

	// Пользователь читается ДО удаления: дальше понадобятся apple_refresh_token
	// (его вычистит шаг 1) и telegram_id (по нему сохранялись состояния бота)
	user, err := s.userRepo.FindById(ctx, userId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
			return
		}
		log.Error().Err(err).Msg("cannot find user for deletion")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось получить пользователя")
		return
	}

	// (0) отзыв токенов Apple — best-effort, но строго до шага 2
	s.revokeAppleTokens(ctx, user)

	// (1) chat_state — пока из документа виден сырой telegram id
	if err := s.purgeChatStates(ctx, chatStateIDs(user)); err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot purge chat states")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось удалить аккаунт")
		return
	}

	// (2) tombstone: аккаунт становится недоступен
	if err := s.userRepo.SoftDeleteUser(ctx, userId); err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot soft delete user")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось удалить аккаунт")
		return
	}

	// (3) инвалидация токена: сам этот запрос прогрел кеш вердиктом «жив»
	s.accounts.forget(userId)

	// (4) анонимизация встроенных снимков
	if err := s.roomRepo.AnonymizeUser(ctx, userId, repository.DeletedUserPlaceholder); err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot anonymize user snapshots")
		writeError(w, http.StatusInternalServerError, errCodePurgeIncomplete, purgeIncompleteMessage)
		return
	}

	// (5) побочные коллекции с PII
	if err := s.purgeUserData(ctx, user); err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot purge user data")
		writeError(w, http.StatusInternalServerError, errCodePurgeIncomplete, purgeIncompleteMessage)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// revokeAppleTokens отзывает refresh token Apple (Guideline 5.1.1(v)).
//
// Best-effort по построению: у пользователя может не быть Apple вовсе, ключ .p8
// может быть не задан (локальная разработка), а Apple — лежать. Ни один из этих
// случаев не повод отказать человеку в удалении аккаунта, поэтому ошибка только
// логируется.
//
// Вызывается СТРОГО до SoftDeleteUser: тот делает $unset apple_refresh_token
func (s *Server) revokeAppleTokens(ctx context.Context, u *api.User) {
	if u.AppleRefreshToken == "" {
		return
	}
	if s.cfg.AppleTokens == nil {
		log.Warn().Int("userId", u.ID).Msg("APPLE_PRIVATE_KEY не задан: токены Apple при удалении аккаунта не отозваны")
		return
	}
	if err := s.cfg.AppleTokens.RevokeToken(ctx, u.AppleRefreshToken); err != nil {
		log.Warn().Err(err).Int("userId", u.ID).Msg("не удалось отозвать токены Apple, удаление аккаунта продолжено")
	}
}

// purgeUserData вычищает PII из побочных коллекций.
//
// Что чистится и почему это НЕ технический мусор:
//   - chat_state — CallbackData.ExternalData хранит текст расхода пользователя;
//   - bug_report — username, display_name и свободный текст жалобы;
//   - push_outbox — отрендеренные title/body, содержащие имя автора и описание
//     расхода: без чистки после анонимизации комнат доставился бы пуш со старым
//     именем;
//   - login_code — живой код иначе продолжил бы логинить в tombstone;
//   - room_invite — invitee_id и inviter_id это id человека, плюс сама связь
//     «кто кого звал в какую комнату»; приглашения удалённого обязаны исчезнуть
//     с обеих сторон.
//
// Что НЕ чистится осознанно:
//   - button — только id комнат и операций, PII там нет;
//   - ai_usage — счётчики запросов без содержимого.
//
// Каждый шаг идемпотентен (DeleteMany по user_id), поэтому повторный DELETE /me
// безопасно доводит чистку до конца. Ошибка возвращается вызывающему: 500
// говорит клиенту повторить, а не делает вид, что всё убрано.
//
// ⚠️ Не подключённая коллекция — тоже ОШИБКА, а не «пропускаем». Раньше nil
// молча пропускался, и потерянный вызов SetPushOutbox (или SetBugReports)
// означал бы 204 с оставшимися в базе именами, текстами жалоб и отрендеренными
// пушами — молчаливый провал ровно того требования, ради которого этот файл
// написан. Все три подключаются в main.go безусловно, так что nil здесь может
// означать только ошибку проводки, и узнать о ней надо громко
func (s *Server) purgeUserData(ctx context.Context, u *api.User) error {
	if err := s.loginCodeRepo.DeleteByUserId(ctx, u.ID); err != nil {
		return err
	}
	// chat_state здесь чистится ПОВТОРНО и только по каноническому _id: основную
	// работу сделал шаг (1) до tombstone, а этот проход добирает состояние,
	// которое бот успел записать между шагами (1) и (2), пока аккаунт был жив.
	// Сырой telegram id тут уже не нужен: под ним лежат только исторические
	// документы, новых после шага (1) не появляется — весь текущий код бота
	// кладёт в chat_state.user_id канонический номер (см. api.ChatState.UserId)
	for _, cleaner := range []struct {
		name string
		repo userDataCleaner
		ids  []int
	}{
		{name: "chat_state", repo: s.chatStates, ids: []int{u.ID}},
		{name: "bug_report", repo: s.bugReports, ids: []int{u.ID}},
		{name: "push_outbox", repo: s.pushOutbox, ids: []int{u.ID}},
		{name: "room_invite", repo: s.invites, ids: []int{u.ID}},
		{name: "debt_reminder", repo: s.debtReminders, ids: []int{u.ID}},
	} {
		if cleaner.repo == nil {
			return errors.Errorf("коллекция %s не подключена: PII удалённого пользователя осталась бы в базе", cleaner.name)
		}
		for _, id := range cleaner.ids {
			if err := cleaner.repo.DeleteByUserId(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// purgeChatStates удаляет состояния бота по всем переданным id.
//
// Вызывается ДО tombstone (шаг 1) — ради этого у него отдельная функция, а не
// строчка в purgeUserData: CallbackData.ExternalData хранит свободный текст
// расхода, то есть настоящий PII, а часть таких записей лежит под СЫРЫМ
// telegram id. SoftDeleteUser вычищает telegram_id первым же действием, и после
// него повторный DELETE /me видит в документе только канонический _id —
// telegram-ключ был бы потерян навсегда вместе с текстами расходов
func (s *Server) purgeChatStates(ctx context.Context, ids []int) error {
	if s.chatStates == nil {
		return errors.New("коллекция chat_state не подключена: PII удалённого пользователя осталась бы в базе")
	}
	for _, id := range ids {
		if err := s.chatStates.DeleteByUserId(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// chatStateIDs — идентификаторы, под которыми могли сохраняться состояния бота.
// Сырой telegram id виден ТОЛЬКО в живом документе: tombstone его уже не
// содержит, поэтому звать это надо строго до SoftDeleteUser
func chatStateIDs(u *api.User) []int {
	ids := []int{u.ID}
	if u.HasTelegram() && *u.TelegramID != u.ID {
		ids = append(ids, *u.TelegramID)
	}
	return ids
}
