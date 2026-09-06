package bot

import (
	"context"
	"fmt"
	"html"
	"slices"
	"sort"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
	"github.com/almaznur91/splitty/internal/pushtext"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// TelegramSender шлёт готовые сообщения в telegram. Реализация — *tgbotapi.BotAPI
// (и tbAPI листенера событий); узкий интерфейс, чтобы Notifier не требовал живого
// бота в тестах
type TelegramSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// NoopTelegramSender — заглушка, когда telegram-бот выключен (нет TG_TOKEN):
// telegram-уведомления no-op, а push-канал Notifier'а работает независимо.
type NoopTelegramSender struct{}

func (NoopTelegramSender) Send(tgbotapi.Chattable) (tgbotapi.Message, error) {
	return tgbotapi.Message{}, nil
}

// UserFinder читает канонические документы пользователей. Узкий интерфейс (как
// TelegramSender), репозиторий подходит как есть. FindByIds нужен списочным
// экранам бота: один запрос на отрисовку вместо N (см. canonicalUsers.warm)
type UserFinder interface {
	FindById(ctx context.Context, id int) (*api.User, error)
	FindByIds(ctx context.Context, ids []int) ([]api.User, error)
}

// Notifier отправляет участникам комнаты те же telegram-уведомления, что и экраны
// бота, но по мутациям, пришедшим не из telegram (REST API iOS/Android-приложений).
// Тексты (ключи scrn_notification_* / scrn_debt_returned_recepient), набор
// получателей и inline-кнопки — паритет со сценариями operation_screen.go
type Notifier struct {
	tg   TelegramSender
	os   OperationService
	bs   ButtonService
	uf   UserFinder
	push push.Sender
}

// NewNotifier собирает Notifier: os нужен для персиста NotificationSent
// (как у бота), bs — для сохранения inline-кнопок «Посмотреть операцию»,
// uf — для чтения АКТУАЛЬНЫХ настроек уведомлений (см. allowsTelegram),
// pushSender — для native-пушей приложениям (nil/NoopSender = пуши выключены).
func NewNotifier(tg TelegramSender, os OperationService, bs ButtonService, uf UserFinder, pushSender push.Sender) *Notifier {
	return &Notifier{tg: tg, os: os, bs: bs, uf: uf, push: pushSender}
}

// pushToUser шлёт native-пуш пользователю, если он включил push-канал категории.
// Канонический документ (с PushTokens и Notify) читаем через uf — во встроенных
// снимках их нет. Best-effort: ошибки/отсутствие токенов молчат.
// Возвращает true, если пуш действительно ушёл в очередь. Вызывающему это
// нужно, чтобы не считать человека уведомлённым, когда отправку отсекли
// настройки: см. pushOperationUpdated.
func (n *Notifier) pushToUser(ctx context.Context, userID int, category api.NotifyCategory, title, key string, data map[string]string, args ...any) bool {
	if n.push == nil || n.uf == nil {
		return false
	}
	canonical, err := n.uf.FindById(ctx, userID)
	if err != nil || canonical == nil || !canonical.WantsPush(category) || len(canonical.PushTokens) == 0 {
		return false
	}
	// По записи на каждый РАЗЛИЧНЫЙ язык среди устройств: у человека может быть
	// русский телефон и английский планшет, и один общий текст был бы неверен
	// для одного из них. Устройства без локали (старый клиент) собираются в
	// запись с пустым языком — она уходит на все токены, как раньше.
	for _, locale := range distinctLocales(canonical.PushTokens) {
		n.push.SendToUser(ctx, *canonical, locale, push.Notification{
			Title: title,
			Body:  pushtext.Tr(locale, key, args...),
			Data:  data,
		})
	}
	return true
}

// distinctLocales — языки устройств пользователя, по одному разу, в
// устойчивом порядке (иначе тесты ловили бы случайный порядок map).
func distinctLocales(tokens []api.PushToken) []string {
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := seen[t.Locale]; ok {
			continue
		}
		seen[t.Locale] = struct{}{}
		out = append(out, t.Locale)
	}
	sort.Strings(out)
	return out
}

// opPushData — данные deeplink пуша по операции (комната/операция + канал Android).
//
// Hex(), а не fmt "%v": ObjectID.String() возвращает `ObjectID("68f2…")`, и
// клиент открывал бы по этому id несуществующую операцию. Значение обязано
// совпадать с полем `id` операции в REST-ответе — по нему клиент её и ищет.
func opPushData(room api.Room, op api.Operation) map[string]string {
	return map[string]string{
		"channel":     "operations",
		"roomId":      room.ID.Hex(),
		"operationId": op.ID.Hex(),
		"type":        "operation",
	}
}

// allowsTelegram решает, слать ли уведомление, по КАНОНИЧЕСКОМУ документу
// пользователя. Получатели приходят из встроенных снимков (room.users[] и
// op.recipientsWithSum[].user), которые пишутся один раз при входе в комнату и
// больше не обновляются: PATCH /me/notifications меняет только коллекцию user,
// поэтому по снимку Notify всегда nil и AllowsTelegram уходит в легаси-ветку —
// выключенные в приложении уведомления продолжали приходить.
//
// Нет привязки к Telegram (вход через Google/Apple) — канал недоступен целиком.
// Сюда же попадает случай «канонический документ не прочитался»: в снимке
// telegram_id нет никогда, а значит и chat id взять неоткуда — слать некуда.
// Push от этого не страдает, он идёт отдельным путём (pushToUser).
func (n *Notifier) allowsTelegram(cu *canonicalUsers, u *api.User, category api.NotifyCategory) bool {
	if u == nil {
		return false
	}
	c := cu.get(u)
	return c.HasTelegram() && c.AllowsTelegram(category)
}

// NotifyOperationCreated — паритет с OperationAdded.notificationWhenCreateOperation:
// если плательщик не автор — ему уходит scrn_notification_payer_changed; каждому
// получателю с ненулевой долей (кроме автора и уже уведомлённых) —
// scrn_notification_operation_added.
//
// NotificationSent пополняется ДО гейтов каналов — намеренно. Список означает
// «кому это событие адресовано», а не «что доставлено»: по нему считается
// непрочитанное в разделе «Уведомления» (rest.notifiesUser), а раздел — входящие
// в приложении, а не третий канал доставки. Записывай мы только доставленное,
// человек без push-разрешения и без telegram (обычный вход через Google/Apple)
// выпадал бы из НЕПУСТОГО списка — и бейдж у него не поднялся бы никогда; заодно
// правило разошлось бы с фоллбэком по долям, который гейтов не знает вовсе.
// Пинается тестом TestNotifierRecordsAddresseesRegardlessOfDelivery
func (n *Notifier) NotifyOperationCreated(ctx context.Context, room api.Room, op api.Operation, author api.User) {
	if op.Donor == nil {
		return
	}

	rb := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: op.ID})
	backB := api.NewButton(viewStart, &api.CallbackData{})
	if _, err := n.bs.SaveAll(ctx, rb, backB); err != nil {
		log.Error().Err(err).Stack().Msg("notifier: save buttons failed")
		return
	}
	keyboardFor := func(u *api.User) [][]tgbotapi.InlineKeyboardButton {
		return [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData(I18n(u, "btn_view_operation"), rb.ID.Hex())},
			{tgbotapi.NewInlineKeyboardButtonData(I18n(u, "btn_to_start"), backB.ID.Hex())},
		}
	}

	// Описание операции и название комнаты — сырой пользовательский ввод, а
	// шаблоны scrn_notification_* уходят с ParseMode=HTML: без экранирования
	// "a < b" даёт 400 от Telegram (уведомление молча теряется), а "<b>"/"<a
	// href>" — разметку в чужих ЛС. Ср. memberName/itemLabel в operation_items.go
	desc := html.EscapeString(op.Description)
	roomName := html.EscapeString(room.Name)

	// chat id и упоминания — из канонических документов: во встроенных снимках
	// telegram_id нет никогда (api.User.Snapshot его обнуляет)
	cu := canonical(ctx, n.uf)
	sentBefore := len(op.NotificationSent)

	var messages []tgbotapi.Chattable
	// как у бота: назначенный плательщик уведомляется без проверки NotificationOn
	if op.Donor.ID != author.ID {
		op.NotificationSent = append(op.NotificationSent, op.Donor.ID)
		// push независим от telegram-канала: у google-пользователя telegram нет вовсе
		n.pushToUser(ctx, op.Donor.ID, api.NotifyOperations, room.Name,
			pushtext.PayerAssigned, opPushData(room, op),
			author.DisplayName, op.Description)
		if chatId, ok := cu.chatID(op.Donor); ok {
			messages = append(messages, NewMessage(chatId,
				I18n(op.Donor, "scrn_notification_payer_changed",
					cu.link(op.Donor), cu.link(&author), desc, moneySpace(op.Sum, room.Currency), roomName),
				keyboardFor(op.Donor)))
		}
	}
	for _, r := range op.RecipientsWithSum {
		recipient := r.User
		if slices.Contains(op.NotificationSent, recipient.ID) ||
			recipient.ID == author.ID ||
			r.Sum == 0 {
			continue
		}
		op.NotificationSent = append(op.NotificationSent, recipient.ID)
		n.pushToUser(ctx, recipient.ID, api.NotifyOperations, room.Name,
			pushtext.ExpenseAdded, opPushData(room, op),
			author.DisplayName, op.Description, moneySpace(int(r.Sum), room.Currency))
		if !n.allowsTelegram(cu, &recipient, api.NotifyOperations) {
			continue
		}
		chatId, ok := cu.chatID(&recipient)
		if !ok {
			continue
		}
		messages = append(messages, NewMessage(chatId,
			I18n(&recipient, "scrn_notification_operation_added",
				cu.link(&recipient), cu.link(&author), desc, moneySpace(op.Sum, room.Currency),
				roomName, moneySpace(int(r.Sum), room.Currency)),
			keyboardFor(&recipient)))
	}
	if len(op.NotificationSent) == sentBefore {
		return
	}

	// точечный $set вместо замены всей операции: горутина держит копию с момента
	// запроса, и полная запись откатывала бы правку, сделанную тем временем
	if err := n.os.SetNotificationSent(ctx, room.ID.Hex(), op.ID, op.NotificationSent); err != nil {
		log.Error().Err(err).Msg("notifier: persist notification_sent failed")
	}
	n.send(messages)
}

// NotifyOperationUpdated — паритет с notificationWhenUpdateOperation: уведомления
// по диффу операции (смена плательщика, название/фото, добавленные и удалённые
// получатели, изменённые доли) с теми же текстами и inline-кнопками
func (n *Notifier) NotifyOperationUpdated(ctx context.Context, room api.Room, oldOp api.Operation, newOp api.Operation, author api.User) {
	if oldOp.Donor == nil || newOp.Donor == nil {
		return
	}
	diff := computeOperationDiff(oldOp, newOp, api.RoomExponent(&room))
	if diff == nil {
		return
	}

	// Inline-кнопки нужны ТОЛЬКО telegram-сообщениям. Их неудача больше не
	// отменяет уведомление целиком: у человека, вошедшего через Google или
	// Apple, телеграма нет вовсе, и молчать в push из-за проблемы соседнего
	// канала бессмысленно.
	donOpBut := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: newOp.ID})
	viewRoomBut := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	if _, err := n.bs.SaveAll(ctx, donOpBut, viewRoomBut); err != nil {
		log.Error().Err(err).Stack().Msg("notifier: save buttons failed, telegram пропущен")
	} else {
		keyboard := [][]tgbotapi.InlineKeyboardButton{
			{
				tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_operation"), donOpBut.ID.Hex()),
			}, {
				tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_room"), viewRoomBut.ID.Hex()),
			},
		}

		// Резолвер канонических документов: без него правки/удаления операций уходили
		// бы даже тем, кто выключил уведомления в приложении (во встроенных снимках
		// Notify всегда nil), а chat id брать было бы неоткуда. Ср. NotifyOperationCreated.
		cu := canonical(ctx, n.uf)
		allows := func(u *api.User, c api.NotifyCategory) bool { return n.allowsTelegram(cu, u, c) }
		n.send(buildUpdateOperationMessages(cu, &author, &author, diff, oldOp, newOp, &room, keyboard, allows))
	}
	n.pushOperationUpdated(ctx, room, oldOp, newOp, author, diff)
}

// pushOperationUpdated — пуши по тому же diff, что и телеграм-сообщения выше.
// Отдельным проходом, а не внутри buildUpdateOperationMessages: тот собирает
// только telegram и общий с ботом, а пуш нужен и тем, у кого телеграма нет
// вовсе (вход через Google или Apple).
//
// Каждый получает не больше одного пуша за правку, поэтому порядок веток —
// от денежных к косметическим: если человеку и долю поменяли, и операцию
// заодно переименовали, важнее первое. Автор правки не уведомляется о себе.
func (n *Notifier) pushOperationUpdated(ctx context.Context, room api.Room, oldOp api.Operation, newOp api.Operation, author api.User, diff *api.OperationDiff) {
	data := opPushData(room, newOp)
	sent := map[int]bool{author.ID: true}
	// Помечаем ПОСЛЕ отправки, а не до: с operations.push=false и
	// edits.push=true денежная ветка ничего не отправит, но пометка «уже
	// уведомлён» съела бы разрешённый пуш про переименование, и человек не
	// получил бы ничего.
	send := func(userID int, category api.NotifyCategory, key string, args ...any) {
		if sent[userID] {
			return
		}
		if n.pushToUser(ctx, userID, category, room.Name, key, data, args...) {
			sent[userID] = true
		}
	}

	if oldOp.Donor.ID != newOp.Donor.ID {
		send(newOp.Donor.ID, api.NotifyOperations, pushtext.PayerAssigned,
			author.DisplayName, newOp.Description)
		send(oldOp.Donor.ID, api.NotifyOperations, pushtext.PayerChanged,
			author.DisplayName, newOp.Description, newOp.Donor.DisplayName)
	}
	for _, added := range diff.RecipientsAdded {
		send(added.User.ID, api.NotifyOperations, pushtext.RecipientAdded,
			author.DisplayName, newOp.Description, moneySpace(int(added.Sum), room.Currency))
	}
	for _, change := range diff.RecipientsShareChanged {
		send(change.User.ID, api.NotifyOperations, pushtext.ShareChanged,
			author.DisplayName, newOp.Description,
			moneySpace(api.FromMinor(change.OldSumMinor, api.RoomExponent(&room)), room.Currency),
			moneySpace(api.FromMinor(change.NewSumMinor, api.RoomExponent(&room)), room.Currency))
	}
	for _, removed := range diff.RecipientsRemoved {
		send(removed.User.ID, api.NotifyOperations, pushtext.RecipientRemoved,
			author.DisplayName, newOp.Description)
	}

	if !diff.NameChanged && !diff.PhotoAdded {
		return
	}
	var key string
	var args []any
	switch {
	case diff.NameChanged && diff.PhotoAdded:
		key, args = pushtext.RenamedWithPhoto, []any{author.DisplayName, oldOp.Description, newOp.Description}
	case diff.NameChanged:
		key, args = pushtext.Renamed, []any{author.DisplayName, oldOp.Description, newOp.Description}
	default:
		key, args = pushtext.PhotoAdded, []any{author.DisplayName, newOp.Description}
	}
	send(newOp.Donor.ID, api.NotifyOperationEdits, key, args...)
	for _, r := range newOp.RecipientsWithSum {
		send(r.User.ID, api.NotifyOperationEdits, key, args...)
	}
}

// NotifyOperationDeleted — паритет с DeleteDonorOperation: бот при удалении
// уведомляет через notificationWhenUpdateOperation с опустошённым списком
// получателей, то есть каждому получателю удалённой операции уходит
// scrn_notification_operation_recipient_removed. Отдельного текста «операция
// удалена» у бота нет — не выдумываем
func (n *Notifier) NotifyOperationDeleted(ctx context.Context, room api.Room, op api.Operation, author api.User) {
	deleted := op
	deleted.RecipientsWithSum = []api.RecipientWithSum{}
	n.NotifyOperationUpdated(ctx, room, op, deleted, author)
}

// NotifyRepaymentCreated — паритет с AddRecepientOperation (возврат долга в боте):
// кредитору уходит scrn_debt_returned_recepient. Текста для должника при
// подтверждении возврата кредитором у бота нет, поэтому в этом случае
// никто не уведомляется
func (n *Notifier) NotifyRepaymentCreated(ctx context.Context, room api.Room, op api.Operation, author api.User) {
	if op.Donor == nil || len(op.RecipientsWithSum) == 0 {
		return
	}
	lender := op.RecipientsWithSum[0].User
	if lender.ID == author.ID {
		return
	}

	// Push независим от telegram-канала: у кредитора tg может быть выключен, а push включён.
	n.pushToUser(ctx, lender.ID, api.NotifyDebts, room.Name,
		pushtext.DebtRepaid,
		map[string]string{"channel": "debts", "roomId": room.ID.Hex(), "type": "debt"},
		op.Donor.DisplayName, moneySpace(op.Sum, room.Currency))

	cu := canonical(ctx, n.uf)
	if !n.allowsTelegram(cu, &lender, api.NotifyDebts) {
		return
	}
	chatId, ok := cu.chatID(&lender)
	if !ok {
		return
	}

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	if _, err := n.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Stack().Msg("notifier: save buttons failed")
		return
	}
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(&lender, "btn_done"), rb.ID.Hex())},
	}
	n.send([]tgbotapi.Chattable{NewMessage(chatId,
		I18n(&lender, "scrn_debt_returned_recepient",
			html.EscapeString(lender.DisplayName), moneySpace(op.Sum, room.Currency), cu.link(op.Donor)),
		keyboard)})
}

// NotifyInvited сообщает человеку, что его позвали в комнату.
//
// isReturn различает два разных события: «вас добавили» (человек уже участник,
// карточка информационная) и «приглашает вернуться» (человек уходил, приглашение
// ждёт его решения). Тексты обязаны отличаться — иначе вернувшийся не поймёт,
// почему от него чего-то ждут.
//
// Канал push помечен как invites: на Android значение data["channel"] уходит в
// ChannelID уведомления, поэтому приложение обязано завести одноимённый канал —
// иначе фоновый пуш на Android 8+ молча не покажется.
func (n *Notifier) NotifyInvited(ctx context.Context, room api.Room, invitee api.User, inviter api.User, isReturn bool) {
	title := room.Name
	key := pushtext.Invited
	if isReturn {
		key = pushtext.InviteReturn
	}
	n.pushToUser(ctx, invitee.ID, api.NotifyInvites, title, key,
		map[string]string{"channel": "invites", "roomId": room.ID.Hex(), "type": "invite"},
		inviter.DisplayName)

	cu := canonical(ctx, n.uf)
	if !n.allowsTelegram(cu, &invitee, api.NotifyInvites) {
		return
	}
	chatId, ok := cu.chatID(&invitee)
	if !ok {
		return
	}

	// Кнопка ведёт в комнату. Для повторного приглашения её не даём: человек
	// ещё не участник, и переход упёрся бы в «вы не участник этой комнаты».
	var keyboard [][]tgbotapi.InlineKeyboardButton
	if !isReturn {
		rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
		if _, err := n.bs.SaveAll(ctx, rb); err != nil {
			log.Error().Err(err).Stack().Msg("notifier: save buttons failed")
			return
		}
		keyboard = [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData(I18n(&invitee, "btn_view_room"), rb.ID.Hex())},
		}
	}

	// Текст telegram — через I18n, как все остальные сообщения бота: у него
	// свои ключи и свои два языка. Push берёт текст из pushtext по языку
	// УСТРОЙСТВА — это разные механизмы намеренно, см. пакет pushtext.
	tgKey := "scrn_notification_invited"
	if isReturn {
		tgKey = "scrn_notification_invited_return"
	}
	n.send([]tgbotapi.Chattable{NewMessage(chatId,
		I18n(&invitee, tgKey, html.EscapeString(inviter.DisplayName), html.EscapeString(room.Name)),
		keyboard)})
}

// send отправляет сообщения по одному; ошибки только логируются (как в
// TelegramListener.sendBotResponse) — уведомление не должно ломать запрос
func (n *Notifier) send(messages []tgbotapi.Chattable) {
	for _, m := range messages {
		if m == nil {
			continue
		}
		if _, err := n.tg.Send(m); err != nil {
			log.Error().Err(err).Msgf("notifier: can't send message to telegram %v", m)
		}
	}
}

// SendDebtReminder — напоминание о невозвращённом долге в telegram
// (reminders.Telegram). Запасной канал рассылки: пуш умеет только приложение,
// а подавляющее большинство должников живёт в боте.
//
// В отличие от остальных методов Notifier, ошибку возвращает наружу: джобу она
// нужна, чтобы вернуть человеку попытку. Молча проглотив её, мы списали бы одно
// из четырёх напоминаний за сообщение, которого человек не видел.
func (n *Notifier) SendDebtReminder(ctx context.Context, user *api.User, text string, roomId string) error {
	if !user.HasTelegram() {
		return fmt.Errorf("у пользователя %d нет telegram", user.ID)
	}

	// Кнопка ведёт в ту комнату, где долг крупнее всего, — ровно туда, куда
	// уводит и тап по пушу (см. reminders.PushData).
	var keyboard [][]tgbotapi.InlineKeyboardButton
	if roomId != "" {
		rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: roomId})
		if _, err := n.bs.SaveAll(ctx, rb); err != nil {
			return errors.Wrap(err, "не сохранил кнопку напоминания")
		}
		keyboard = [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData(I18n(user, "btn_view_room"), rb.ID.Hex())},
		}
	}

	// Текст собран джобом на языке человека (reminders.Body), экранируем его
	// целиком: в нём есть название группы, которое писал человек, а сообщения
	// бота уходят с ParseMode=HTML.
	message := NewMessage(int64(*user.TelegramID), html.EscapeString(text), keyboard)
	if _, err := n.tg.Send(message); err != nil {
		return errors.Wrap(err, "не отправил напоминание в telegram")
	}
	return nil
}
