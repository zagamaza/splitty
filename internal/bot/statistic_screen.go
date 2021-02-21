package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

type StatisticService interface {
	GetUserDebtAndLendSum(ctx context.Context, userId int, roomId string) (debt int, lent int, e error)
	GetUserCostsSum(ctx context.Context, userId int, roomId string) (int, error)
	GetAllCostsSum(ctx context.Context, roomId string) (int, error)
	GetAllDebtsSum(ctx context.Context, roomId string) (int, error)
}

// Statistic screen w
type Statistic struct {
	bs  ButtonService
	rs  RoomService
	css ChatStateService
	ss  StatisticService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewStatistic(bs ButtonService, rs RoomService, css ChatStateService, ss StatisticService, cfg *Config) *Statistic {
	return &Statistic{
		bs:  bs,
		rs:  rs,
		ss:  ss,
		cfg: cfg,
		css: css,
	}
}

// ReactOn keys
func (bot Statistic) HasReact(u *api.Update) bool {
	return isPrivate(u) && hasAction(u, statistics)
}

// OnMessage returns one entry
func (bot *Statistic) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	roomId := u.Button.CallbackData.RoomId
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, userId:%s", roomId)
		return
	}

	totalSpendSum, err := bot.ss.GetAllCostsSum(ctx, roomId)
	if err != nil {
		return
	}
	totalUserSpendSum, err := bot.ss.GetUserCostsSum(ctx, getFrom(u).ID, roomId)
	if err != nil {
		return
	}
	totalDebtSum, err := bot.ss.GetAllDebtsSum(ctx, roomId)
	if err != nil {
		return
	}

	debtorSum, lenderSum, err := bot.ss.GetUserDebtAndLendSum(ctx, getFrom(u).ID, room.ID.Hex())
	if err != nil {
		return
	}
	var debtText string
	if debtorSum != 0 {
		debtText = fmt.Sprintf(string(emoji.RedCircle)+" Вы должны: *%v ₽*", moneySpace(debtorSum))
	} else if lenderSum != 0 {
		debtText = fmt.Sprintf(string(emoji.GreenCircle)+" Вам должны: *%v ₽*", moneySpace(lenderSum))
	} else {
		debtText = fmt.Sprintf(string(emoji.WhiteCircle)) + "️Долгов нет"
	}

	data := &api.CallbackData{RoomId: roomId}
	startB := api.NewButton(viewRoom, data)
	debtOperationsB := api.NewButton(viewAllDebtOperations, data)
	if _, err = bot.bs.SaveAll(ctx, debtOperationsB); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	text := fmt.Sprintf("📊 Статистика тусы: *%s*\n\n\n", room.Name)
	text += fmt.Sprintf("👥 Общий расход тусы: *%s* ₽\n\n", moneySpace(totalSpendSum))
	text += fmt.Sprintf("👤 Ваша доля расходов: *%s* ₽\n\n", moneySpace(totalUserSpendSum))
	text += debtText + "\n\n"
	text += fmt.Sprintf("💸 Общая сумма долгов: *%s* ₽\n\n", moneySpace(totalDebtSum))
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("💸 Выплаченные долги", debtOperationsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("🔝 В комнату", startB.ID.Hex())},
	}

	if _, err := bot.bs.SaveAll(ctx, startB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboard)},
		Send:      true,
	}
}

// ViewAllDebtOperations show screen with donar/recepient buttons
type ViewAllDebtOperations struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewAllDebtOperations(css ChatStateService, bs ButtonService, os OperationService, cfg *Config) *ViewAllDebtOperations {
	return &ViewAllDebtOperations{
		css: css,
		bs:  bs,
		os:  os,
		cfg: cfg,
	}
}

func (bot ViewAllDebtOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllDebtOperations)
}

func (bot ViewAllDebtOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := 5
	skip := page * size

	ops, err := bot.os.GetAllDebtOperations(ctx, roomId)
	if err != nil {
		return
	}

	var toSave []*api.Button
	var text = "*История операций по возврату долгов*\n\n"
	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		text += fmt.Sprintf("%s *%s ₽* ➡ ️%s", userLink(op.Donor), moneySpace(op.Sum), userLink(&(*op.Recipients)[0])+"\n\n")
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllDebtOperations, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(statistics, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", backB.ID.Hex()))
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewAllDebtOperations, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, text, &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
