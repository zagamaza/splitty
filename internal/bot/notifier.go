package bot

import (
	"context"
	"html"
	"slices"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

// TelegramSender шлёт готовые сообщения в telegram. Реализация — *tgbotapi.BotAPI
// (и tbAPI листенера событий); узкий интерфейс, чтобы Notifier не требовал живого
// бота в тестах
type TelegramSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// UserFinder читает канонический документ пользователя. Узкий интерфейс (как
// TelegramSender): Notifier'у нужен только FindById, репозиторий подходит как есть
type UserFinder interface {
	FindById(ctx context.Context, id int) (*api.User, error)
}

// Notifier отправляет участникам комнаты те же telegram-уведомления, что и экраны
// бота, но по мутациям, пришедшим не из telegram (REST API iOS/Android-приложений).
// Тексты (ключи scrn_notification_* / scrn_debt_returned_recepient), набор
// получателей и inline-кнопки — паритет со сценариями operation_screen.go
type Notifier struct {
	tg TelegramSender
	os OperationService
	bs ButtonService
	uf UserFinder
}

// NewNotifier собирает Notifier: os нужен для персиста NotificationSent
// (как у бота), bs — для сохранения inline-кнопок «Посмотреть операцию»,
// uf — для чтения АКТУАЛЬНЫХ настроек уведомлений (см. allowsTelegram)
func NewNotifier(tg TelegramSender, os OperationService, bs ButtonService, uf UserFinder) *Notifier {
	return &Notifier{tg: tg, os: os, bs: bs, uf: uf}
}

// allowsTelegram решает, слать ли уведомление, по КАНОНИЧЕСКОМУ документу
// пользователя. Получатели приходят из встроенных снимков (room.users[] и
// op.recipientsWithSum[].user), которые пишутся один раз при входе в комнату и
// больше не обновляются: PATCH /me/notifications меняет только коллекцию user,
// поэтому по снимку Notify всегда nil и AllowsTelegram уходит в легаси-ветку —
// выключенные в приложении уведомления продолжали приходить.
// Пользователя не удалось прочитать — падаем на снимок, а не молчим.
func (n *Notifier) allowsTelegram(ctx context.Context, u *api.User, category api.NotifyCategory) bool {
	if u == nil {
		return false
	}
	if n.uf != nil {
		if canonical, err := n.uf.FindById(ctx, u.ID); err != nil {
			log.Warn().Err(err).Int("user", u.ID).Msg("notifier: can't read notify prefs, using embedded snapshot")
		} else if canonical != nil {
			return canonical.AllowsTelegram(category)
		}
	}
	return u.AllowsTelegram(category)
}

// NotifyOperationCreated — паритет с OperationAdded.notificationWhenCreateOperation:
// если плательщик не автор — ему уходит scrn_notification_payer_changed; каждому
// получателю (кроме автора, ещё не уведомлённых, с включёнными уведомлениями и
// ненулевой долей) — scrn_notification_operation_added. Уведомлённые помечаются
// в NotificationSent и персистятся, как у бота
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

	var messages []tgbotapi.Chattable
	// как у бота: назначенный плательщик уведомляется без проверки NotificationOn
	if op.Donor.ID != author.ID {
		messages = append(messages, NewMessage(int64(op.Donor.ID),
			I18n(op.Donor, "scrn_notification_payer_changed",
				userLink(op.Donor), userLink(&author), desc, moneySpace(op.Sum, room.Currency), roomName),
			keyboardFor(op.Donor)))
		op.NotificationSent = append(op.NotificationSent, op.Donor.ID)
	}
	for _, r := range op.RecipientsWithSum {
		recipient := r.User
		if slices.Contains(op.NotificationSent, recipient.ID) ||
			!n.allowsTelegram(ctx, &recipient, api.NotifyOperations) ||
			recipient.ID == author.ID ||
			r.Sum == 0 {
			continue
		}
		messages = append(messages, NewMessage(int64(recipient.ID),
			I18n(&recipient, "scrn_notification_operation_added",
				userLink(&recipient), userLink(&author), desc, moneySpace(op.Sum, room.Currency),
				roomName, moneySpace(int(r.Sum), room.Currency)),
			keyboardFor(&recipient)))
		op.NotificationSent = append(op.NotificationSent, recipient.ID)
	}
	if len(messages) == 0 {
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
	diff := computeOperationDiff(oldOp, newOp)
	if diff == nil {
		return
	}

	donOpBut := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: newOp.ID})
	viewRoomBut := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	if _, err := n.bs.SaveAll(ctx, donOpBut, viewRoomBut); err != nil {
		log.Error().Err(err).Stack().Msg("notifier: save buttons failed")
		return
	}
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_operation"), donOpBut.ID.Hex()),
		}, {
			tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_room"), viewRoomBut.ID.Hex()),
		},
	}

	// Резолвер канонических настроек: иначе правки/удаления операций уходили бы
	// даже тем, кто выключил уведомления в приложении (во встроенных снимках
	// Notify всегда nil). Ср. NotifyOperationCreated.
	allows := func(u *api.User, c api.NotifyCategory) bool { return n.allowsTelegram(ctx, u, c) }
	n.send(buildUpdateOperationMessages(&author, &author, diff, oldOp, newOp, &room, keyboard, allows))
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
	if lender.ID == author.ID || !n.allowsTelegram(ctx, &lender, api.NotifyDebts) {
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
	n.send([]tgbotapi.Chattable{NewMessage(int64(lender.ID),
		I18n(&lender, "scrn_debt_returned_recepient",
			html.EscapeString(lender.DisplayName), moneySpace(op.Sum, room.Currency), userLink(op.Donor)),
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
