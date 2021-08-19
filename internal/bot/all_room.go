package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

// send /room, after click on the button 'Присоединиться'
type AllRoomInline struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	ss  StatisticService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAllRoomInline(s ChatStateService, bs ButtonService, rs RoomService, ss StatisticService, cfg *Config) *AllRoomInline {
	return &AllRoomInline{
		css: s,
		bs:  bs,
		rs:  rs,
		ss:  ss,
		cfg: cfg,
	}
}

// ReactOn keys
func (bot AllRoomInline) HasReact(u *api.Update) bool {
	return u.InlineQuery != nil
}

// OnMessage returns one entry
func (bot *AllRoomInline) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	rooms := bot.findRoomsByUpdate(ctx, u)
	userId := getFrom(u).ID

	var results []interface{}
	for _, room := range *rooms {
		debtorSum, lenderSum, err := bot.ss.GetUserDebtAndLendSum(ctx, userId, room.ID.Hex())
		if err != nil {
			return
		}
		var debtText string
		if debtorSum != 0 {
			debtText = I18n(u.User, "msg_you_debt", moneySpace(debtorSum))
		} else if lenderSum != 0 {
			debtText = I18n(u.User, "msg_lend_you", moneySpace(lenderSum))
		} else {
			debtText = I18n(u.User, "msg_you_not_debt")
		}

		data := &api.CallbackData{RoomId: room.ID.Hex()}
		joinB := api.NewButton(joinRoom, data)
		viewOpsB := api.NewButton(viewAllOperations, data)
		viewDbtB := api.NewButton(viewAllDebts, data)
		startB := api.NewButton(viewStart, data)

		if _, err := bot.bs.SaveAll(ctx, joinB, viewOpsB, viewDbtB, startB); err != nil {
			log.Error().Err(err).Msg("create btn failed")
			continue
		}

		article := NewInlineResultArticle(room.Name, debtText, createRoomInfoText(&room, u), [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_join"), joinB.ID.Hex())},
			{tgbotapi.NewInlineKeyboardButtonURL(I18n(u.User, "btn_start"), "http://t.me/"+bot.cfg.BotName+"?start=room"+room.ID.Hex())},
		})

		results = append(results, article)
	}

	return api.TelegramMessage{
		InlineConfig: NewInlineConfig(u.InlineQuery.ID, results),
		Send:         true,
	}
}

// send /room, after click on the button 'Присоединиться'
type AllRoom struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAllRoom(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *AllRoom {
	return &AllRoom{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (bot AllRoom) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllRooms)
}

// OnMessage returns one entry
func (bot *AllRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	page := u.Button.CallbackData.Page
	size := u.User.CountInPage
	skip := page * size

	rooms, err := bot.rs.FindRoomsByUserId(ctx, getFrom(u).ID)
	if err != nil {
		log.Error().Err(err).Msgf("cannot find rooms")
		return
	}
	if len(*rooms) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_rooms"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var toSave []*api.Button
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for i := skip; i < skip+size && i < len(*rooms); i++ {
		roomB := api.NewButton(viewRoom, &api.CallbackData{RoomId: (*rooms)[i].ID.Hex()})
		toSave = append(toSave, roomB)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData((*rooms)[i].Name, roomB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllRooms, &api.CallbackData{Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	if skip+size < len(*rooms) {
		nextB := api.NewButton(viewAllRooms, &api.CallbackData{Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	if len(navRow) != 0 {
		keyboard = append(keyboard, navRow)
	}

	backB := api.NewButton(viewStart, u.Button.CallbackData)
	toSave = append(toSave, backB)
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_to_start"), backB.ID.Hex()),
	})

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_my_rooms"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

// send /room, after click on the button 'Присоединиться'
type ArchivedRooms struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewArchivedRooms(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *ArchivedRooms {
	return &ArchivedRooms{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (bot ArchivedRooms) HasReact(u *api.Update) bool {
	return hasAction(u, viewArchivedRooms) ||
		isPrivate(u) && isCommand(u) && u.Message.Text == "/archived"
}

// OnMessage returns one entry
func (bot *ArchivedRooms) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	var page int
	if isButton(u) {
		page = u.Button.CallbackData.Page
	}
	size := 5
	skip := page * size

	rooms, err := bot.rs.FindArchivedRoomsByUserId(ctx, getFrom(u).ID)
	if err != nil {
		log.Error().Err(err).Msgf("cannot find rooms")
		return
	}
	if len(*rooms) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_archive"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var toSave []*api.Button
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for i := skip; i < skip+size && i < len(*rooms); i++ {
		roomB := api.NewButton(viewRoom, &api.CallbackData{RoomId: (*rooms)[i].ID.Hex()})
		toSave = append(toSave, roomB)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData((*rooms)[i].Name, roomB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllRooms, &api.CallbackData{Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	if skip+size < len(*rooms) {
		nextB := api.NewButton(viewAllRooms, &api.CallbackData{Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	if len(navRow) != 0 {
		keyboard = append(keyboard, navRow)
	}

	backB := api.NewButton(viewStart, &api.CallbackData{})
	toSave = append(toSave, backB)
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_to_start"), backB.ID.Hex()),
	})

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_archive_rooms"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

type PartyType int

const (
	OperationAdding PartyType = iota
	DebtDistributing
	Finished
)

func createRoomInfoText(r *api.Room, u *api.Update) string {
	finishedAddOperationCount := len(r.RoomStates.FinishedAddOperation)
	paidOffDebtCunt := len(r.RoomStates.PaidOffDebt)
	memberCount := len(*r.Members)

	partyType := definePartyType(finishedAddOperationCount, memberCount, paidOffDebtCunt)

	var partyStatus string
	switch partyType {
	case OperationAdding:
		partyStatus = I18n(u.User, "scrn_party_type_operation_adding")
	case DebtDistributing:
		partyStatus = I18n(u.User, "scrn_party_type_debt_distributing")
	case Finished:
		partyStatus = I18n(u.User, "scrn_party_type_finished")
	}
	text := I18n(u.User, "scrn_room", r.Name, partyStatus)

	for _, v := range *r.Members {
		text += "- " + userLink(&v)
		if containsInt(r.RoomStates.PaidOffDebt, v.ID) && DebtDistributing == partyType {
			text += " 🤝"
		} else if containsInt(r.RoomStates.FinishedAddOperation, v.ID) && OperationAdding == partyType {
			text += " 🏁"
		}
		text += "\n"
	}
	if paidOffDebtCunt != 0 && partyType == DebtDistributing {
		text += I18n(u.User, "scrn_debt_legend")
	} else if finishedAddOperationCount != 0 && partyType == OperationAdding {
		text += I18n(u.User, "scrn_finished_added_legend")
	}
	return text
}

func definePartyType(finishedAddOperationCount int, memberCount int, paidOffDebtCunt int) PartyType {
	var partyType PartyType
	if finishedAddOperationCount != memberCount {
		partyType = OperationAdding
	} else if finishedAddOperationCount == memberCount && paidOffDebtCunt != memberCount {
		partyType = DebtDistributing
	} else {
		partyType = Finished
	}
	return partyType
}

func (bot AllRoomInline) findRoomsByUpdate(ctx context.Context, u *api.Update) *[]api.Room {
	rooms, err := bot.rs.FindRoomsByLikeName(ctx, u.InlineQuery.From.ID, u.InlineQuery.Query)
	if err != nil {
		log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
	}
	return rooms
}
