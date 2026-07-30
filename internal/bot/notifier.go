package bot

import (
	"context"
	"fmt"
	"html"
	"slices"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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
func (n *Notifier) pushToUser(ctx context.Context, userID int, category api.NotifyCategory, title, body string, data map[string]string) {
	if n.push == nil || n.uf == nil {
		return
	}
	canonical, err := n.uf.FindById(ctx, userID)
	if err != nil || canonical == nil || !canonical.WantsPush(category) || len(canonical.PushTokens) == 0 {
		return
	}
	n.push.SendToUser(ctx, *canonical, push.Notification{Title: title, Body: body, Data: data})
}

// opPushData — данные deeplink пуша по операции (комната/операция + канал Android).
func opPushData(room api.Room, op api.Operation) map[string]string {
	return map[string]string{
		"channel":     "operations",
		"roomId":      room.ID.Hex(),
		"operationId": fmt.Sprintf("%v", op.ID),
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
			fmt.Sprintf("%s назначил вас плательщиком «%s»", author.DisplayName, op.Description),
			opPushData(room, op))
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
			fmt.Sprintf("%s добавил расход «%s» — ваша доля %s",
				author.DisplayName, op.Description, moneySpace(int(r.Sum), room.Currency)),
			opPushData(room, op))
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

	// Резолвер канонических документов: без него правки/удаления операций уходили
	// бы даже тем, кто выключил уведомления в приложении (во встроенных снимках
	// Notify всегда nil), а chat id брать было бы неоткуда. Ср. NotifyOperationCreated.
	cu := canonical(ctx, n.uf)
	allows := func(u *api.User, c api.NotifyCategory) bool { return n.allowsTelegram(cu, u, c) }
	n.send(buildUpdateOperationMessages(cu, &author, &author, diff, oldOp, newOp, &room, keyboard, allows))
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
		fmt.Sprintf("%s вернул вам долг %s", op.Donor.DisplayName, moneySpace(op.Sum, room.Currency)),
		map[string]string{"channel": "debts", "roomId": room.ID.Hex(), "type": "debt"})

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
