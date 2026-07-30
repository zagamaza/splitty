package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"html"
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
	totalUserSpendSum, err := bot.ss.GetUserCostsSum(ctx, u.User.ID, roomId)
	if err != nil {
		return
	}
	totalDebtSum, err := bot.ss.GetAllDebtsSum(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("GetAllDebtsSum, userId:%s", roomId)
		return
	}

	debtorSum, lenderSum, err := bot.ss.GetUserDebtAndLendSum(ctx, u.User.ID, room.ID.Hex())
	if err != nil {
		return
	}
	var debtText string
	if debtorSum != 0 {
		debtText = I18n(u.User, "msg_you_debt", moneySpace(debtorSum, room.Currency))
	} else if lenderSum != 0 {
		debtText = I18n(u.User, "msg_lend_you", moneySpace(lenderSum, room.Currency))
	} else {
		debtText = I18n(u.User, "msg_you_not_debt")
	}

	data := &api.CallbackData{RoomId: roomId}
	startB := api.NewButton(viewRoom, data)
	debtOperationsB := api.NewButton(viewAllDebtOperations, data)
	if _, err = bot.bs.SaveAll(ctx, debtOperationsB); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	text := fmt.Sprintf(I18n(u.User, "scrn_statistic", html.EscapeString(room.Name)) + "\n\n\n")
	text += fmt.Sprintf(I18n(u.User, "msg_common_spend", moneySpace(totalSpendSum, room.Currency)) + "\n\n")
	text += fmt.Sprintf(I18n(u.User, "msg_you_spend", moneySpace(totalUserSpendSum, room.Currency)) + "\n\n")
	text += debtText + "\n\n"
	text += fmt.Sprintf(I18n(u.User, "msg_common_debt", moneySpace(totalDebtSum, room.Currency)) + "\n\n")
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_paid_debt"), debtOperationsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), startB.ID.Hex())},
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
	rs  RoomService
	bs  ButtonService
	os  OperationService
	us  UserService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewAllDebtOperations(css ChatStateService, rs RoomService, bs ButtonService, os OperationService, us UserService, cfg *Config) *ViewAllDebtOperations {
	return &ViewAllDebtOperations{
		css: css,
		rs:  rs,
		bs:  bs,
		os:  os,
		us:  us,
		cfg: cfg,
	}
}

func (bot ViewAllDebtOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllDebtOperations)
}

func (bot ViewAllDebtOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := u.User.CountInPage
	skip := page * size

	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, userId:%s", roomId)
		return
	}

	ops, err := bot.os.GetAllDebtOperations(ctx, roomId)
	if err != nil {
		return
	}

	var toSave []*api.Button
	var text = I18n(u.User, "scrn_debt_history") + "\n\n"
	var keyboard [][]tgbotapi.InlineKeyboardButton

	// участники истории приходят из снимков операций, где telegram_id нет
	// никогда — упоминания собираем по каноническим документам, одним запросом
	// на страницу
	cu := canonical(ctx, bot.us)
	var pageUserIds []int
	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		if op.Donor != nil {
			pageUserIds = append(pageUserIds, op.Donor.ID)
		}
		if len(op.RecipientsWithSum) > 0 {
			pageUserIds = append(pageUserIds, op.RecipientsWithSum[0].User.ID)
		}
	}
	cu.warm(pageUserIds)

	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		text += fmt.Sprintf("%s  <b>%s</b> ➡ ️%s", cu.link(op.Donor), moneySpace(op.Sum, room.Currency), cu.link(&(op.RecipientsWithSum)[0].User)+"\n\n")
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllDebtOperations, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(statistics, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewAllDebtOperations, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	screen := createScreen(u, text, &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
