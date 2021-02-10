package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

type ViewUserDebts struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewViewUserDebts(s ChatStateService, bs ButtonService, rs OperationService, cfg *Config) *ViewUserDebts {
	return &ViewUserDebts{
		css: s,
		bs:  bs,
		os:  rs,
		cfg: cfg,
	}
}

func (bot ViewUserDebts) HasReact(u *api.Update) bool {
	return hasAction(u, viewUserDebts)
}

func (bot ViewUserDebts) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	userId := getFrom(u).ID
	page := u.Button.CallbackData.Page
	size := 5
	skip := page * size

	debts, err := bot.os.GetAllUsersDebts(ctx, userId, roomId)
	if err != nil {
		return
	}
	if len(*debts) < 1 {
		callback := createCallback(u, string(emoji.Warning)+"У вас отсутствуют долги", true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var toSave []*api.Button
	var debtBtns []tgbotapi.InlineKeyboardButton
	for i := skip; i < skip+size && i < len(*debts); i++ {
		debt := (*debts)[i]
		var dbtB *api.Button
		if debt.Debtor.ID == userId {
			dbtB = api.NewButton(chooseRecipient, &api.CallbackData{RoomId: roomId, UserId: debt.Lender.ID})
		} else {
			dbtB = api.NewButton(viewUserDebts, &api.CallbackData{RoomId: roomId, Page: page})
		}
		toSave = append(toSave, dbtB)
		text := fmt.Sprintf("%s %s %s [ %s ₽ ]", shortName(debt.Debtor), emoji.RightArrow, shortName(debt.Lender), thousandSpace(debt.Sum))
		debtBtns = append(debtBtns, tgbotapi.NewInlineKeyboardButtonData(text, dbtB.ID.Hex()))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewUserDebts, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow)+" prev", prevB.ID.Hex()))
	}
	if skip+size < len(*debts) {
		nextB := api.NewButton(viewUserDebts, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("next "+string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard := splitKeyboardButtons(debtBtns, 1)
	if len(navRow) != 0 {
		keyboard = append(keyboard, navRow)
	}

	backB := api.NewButton(viewRoom, u.Button.CallbackData)
	toSave = append(toSave, backB)
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("В комнату", backB.ID.Hex()),
	})

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, "Мои долги", &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

type ViewAllDebts struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewViewAllDebts(s ChatStateService, bs ButtonService, rs OperationService, cfg *Config) *ViewAllDebts {
	return &ViewAllDebts{
		css: s,
		bs:  bs,
		os:  rs,
		cfg: cfg,
	}
}

func (bot ViewAllDebts) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllDebts)
}

func (bot ViewAllDebts) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := 5
	skip := page * size

	debts, err := bot.os.GetAllDebts(ctx, roomId)
	if err != nil {
		return
	}

	var toSave []*api.Button
	var debtBtns []tgbotapi.InlineKeyboardButton
	for i := skip; i < skip+size && i < len(*debts); i++ {
		debt := (*debts)[i]
		var dbtB *api.Button
		dbtB = api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page})

		toSave = append(toSave, dbtB)
		text := fmt.Sprintf("%s %s %s [ %s ₽ ]", shortName(debt.Debtor), emoji.RightArrow, shortName(debt.Lender), thousandSpace(debt.Sum))
		debtBtns = append(debtBtns, tgbotapi.NewInlineKeyboardButtonData(text, dbtB.ID.Hex()))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	if skip+size < len(*debts) {
		nextB := api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard := splitKeyboardButtons(debtBtns, 1)
	if len(navRow) != 0 {
		keyboard = append(keyboard, navRow)
	}

	backB := api.NewButton(viewRoom, u.Button.CallbackData)
	toSave = append(toSave, backB)
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("В комнату", backB.ID.Hex()),
	})

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, "Мои долги", &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
