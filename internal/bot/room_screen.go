package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"strings"
)

// send /room, after click on the button 'Присоединиться'
type JoinRoom struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewJoinRoom(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *JoinRoom {
	return &JoinRoom{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (bot JoinRoom) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == joinRoom
}

// OnMessage returns one entry
func (bot JoinRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId

	err := bot.rs.JoinToRoom(ctx, u.CallbackQuery.From, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("join room failed %v", roomId)
		return
	}

	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("get room failed %v", roomId)
		return
	}

	data := &api.CallbackData{RoomId: room.ID.Hex()}

	joinB := api.NewButton(joinRoom, data)
	viewOpsB := api.NewButton(viewAllOperations, data)
	viewDbtB := api.NewButton(viewAllDebts, data)

	if _, err := bot.bs.SaveAll(ctx, joinB, viewOpsB, viewDbtB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	text := createRoomInfoText(room)
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonURL("Начать работу", "http://t.me/"+bot.cfg.BotName+"?start="+string(viewRoom)+room.ID.Hex())},
	}
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboard)},
		Send:      true,
	}
}

// send /room, after click on the button 'Присоединиться'
type ViewRoom struct {
	bs  ButtonService
	rs  RoomService
	css ChatStateService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewRoom(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config) *ViewRoom {
	return &ViewRoom{
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

// ReactOn keys
func (bot ViewRoom) HasReact(u *api.Update) bool {
	return isPrivate(u) && (hasAction(u, viewRoom) ||
		hasMessage(u) && strings.Contains(u.Message.Text, string(viewRoom)))
}

// OnMessage returns one entry
func (bot *ViewRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	var roomId string
	if isButton(u) {
		roomId = u.Button.CallbackData.RoomId
	} else {
		roomId = strings.ReplaceAll(u.Message.Text, "/start "+string(viewRoom), "")
	}

	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}

	if !containsUserId(room.Members, getFrom(u).ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), "К сожалению, вы не находитесь в этой тусе")},
			Send:      true,
		}
	}

	data := &api.CallbackData{RoomId: roomId}
	viewOpsB := api.NewButton(viewAllOperations, data)
	viewDbtB := api.NewButton(viewAllDebts, data)
	startB := api.NewButton(viewStart, data)
	startOpB := api.NewButton(viewStartOperation, data)
	staticsB := api.NewButton(statistics, data)

	text := createRoomInfoText(room)
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("💰 Операции", viewOpsB.ID.Hex()),
			tgbotapi.NewInlineKeyboardButtonData("💸 Долги", viewDbtB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("➕ Добавить операцию", startOpB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonSwitch("📢 Отправить в чат", room.Name)},
		{tgbotapi.NewInlineKeyboardButtonData("📊 Статистика", staticsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("🔝 В начало", startB.ID.Hex())},
	}

	if _, err := bot.bs.SaveAll(ctx, viewOpsB, viewDbtB, startB, startOpB, staticsB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboard)},
		Send:      true,
	}
}
