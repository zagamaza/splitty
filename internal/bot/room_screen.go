package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
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
	startB := api.NewButton(viewStart, data)

	if _, err := bot.bs.SaveAll(ctx, joinB, viewOpsB, viewDbtB, startB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	text := createRoomInfoText(room)
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("Просмотр операций", viewOpsB.ID.Hex())}}
	if isPrivate(u) {
		keyboard = append(keyboard, [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("Мои долги", viewDbtB.ID.Hex())}}...)
	}
	keyboard = append(keyboard, [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+bot.cfg.BotName+"?start=operation"+room.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("В начало", startB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonSwitch("Опубликовать комнату в свой чат", room.Name)}}...)

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
	return hasAction(u, viewRoom)
}

// OnMessage returns one entry
func (bot *ViewRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	roomId := u.Button.CallbackData.RoomId
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}

	data := u.Button.CallbackData

	joinB := api.NewButton(joinRoom, data)
	viewOpsB := api.NewButton(viewAllOperations, data)
	viewDbtB := api.NewButton(viewAllDebts, &api.CallbackData{RoomId: roomId})
	startB := api.NewButton(viewStart, data)

	if _, err := bot.bs.SaveAll(ctx, joinB, viewOpsB, viewDbtB, startB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	text := createRoomInfoText(room)
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("Просмотр операций", viewOpsB.ID.Hex())}}
	if isPrivate(u) {
		keyboard = append(keyboard, [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("Мои долги", viewDbtB.ID.Hex())}}...)
	}
	keyboard = append(keyboard, [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+bot.cfg.BotName+"?start=operation"+room.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("В начало", startB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonSwitch("Опубликовать комнату в свой чат", room.Name)}}...)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboard)},
		Send:      true,
	}
}
