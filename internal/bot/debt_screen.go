package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

// Operation show screen with my and all chooseOperations buttons
type Debt struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewDebt(s ChatStateService, bs ButtonService, os OperationService, cfg *Config) *Debt {
	return &Debt{
		css: s,
		bs:  bs,
		os:  os,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (bot Debt) HasReact(u *api.Update) bool {
	if hasAction(u, chooseDebts) {
		return true
	}
	return false
}

// OnMessage returns one entry
func (bot Debt) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	debts, err := bot.os.GetAllDebts(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}
	if len(*debts) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_debts"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}
	data := &api.CallbackData{RoomId: roomId}

	viewUserOpsB := api.NewButton(viewUserDebts, data)
	viewAllOpsB := api.NewButton(viewAllDebts, data)
	backB := api.NewButton(viewRoom, data)

	if _, err := bot.bs.SaveAll(ctx, viewUserOpsB, viewAllOpsB, backB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_user_debts"), viewUserOpsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_all_debts"), viewAllOpsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex())},
	}
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, I18n(u.User, "scrn_debts"), &keyboard)},
		Send:      true,
	}
}

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

	debts, err := bot.os.GetUserDebts(ctx, userId, roomId)
	if err != nil {
		return
	}
	if len(*debts) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_user_debts"), true)
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
		text := fmt.Sprintf("%s➡️%s ₽➡️%s", shortName(debt.Debtor), moneySpace(debt.Sum), shortName(debt.Lender))
		debtBtns = append(debtBtns, tgbotapi.NewInlineKeyboardButtonData(text, dbtB.ID.Hex()))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewUserDebts, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(chooseDebts, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*debts) {
		nextB := api.NewButton(viewUserDebts, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}

	keyboard := splitKeyboardButtons(debtBtns, 1)
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_my_debts"), &keyboard)
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
	userId := getFrom(u).ID
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
		if debt.Debtor.ID == userId {
			dbtB = api.NewButton(chooseRecipient, &api.CallbackData{RoomId: roomId, UserId: debt.Lender.ID})
		} else {
			dbtB = api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page})
		}
		toSave = append(toSave, dbtB)
		text := fmt.Sprintf("%s➡️%s ₽➡️%s", shortName(debt.Debtor), moneySpace(debt.Sum), shortName(debt.Lender))
		debtBtns = append(debtBtns, tgbotapi.NewInlineKeyboardButtonData(text, dbtB.ID.Hex()))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️", prevB.ID.Hex()))
	}
	backB := api.NewButton(chooseDebts, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*debts) {
		nextB := api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️", nextB.ID.Hex()))
	}

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboard := splitKeyboardButtons(debtBtns, 1)
	keyboard = append(keyboard, navRow)
	screen := createScreen(u, I18n(u.User, "scrn_all_debts"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
