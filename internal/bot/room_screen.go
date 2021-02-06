package bot

import (
	"context"
	"fmt"
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
func (s JoinRoom) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == joinRoom
}

// OnMessage returns one entry
func (s JoinRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId

	err := s.rs.JoinToRoom(ctx, u.CallbackQuery.From, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("join room failed %v", roomId)
		return
	}

	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("get room failed %v", roomId)
		return
	}
	text := "Экран комнаты *" + room.Name + "*\n\nУчастники:\n"
	for _, v := range *room.Members {
		text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
	}

	b := &api.Button{Action: joinRoom, CallbackData: u.Button.CallbackData}
	cId, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", cId.Hex())}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+s.cfg.BotName+"?start=operation"+room.ID.Hex())}

	screen := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		button1,
		button2,
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
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
	joinB := api.NewButton(joinRoom, u.Button.CallbackData)
	viewOpsB := api.NewButton(viewAllOperations, u.Button.CallbackData)
	if _, err := bot.bs.SaveAll(ctx, joinB, viewOpsB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	tgMsg := createScreen(u, createRoomInfoText(room), &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData("Просмотр операций", viewOpsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+bot.cfg.BotName+"?start=operation"+room.ID.Hex())},
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tgMsg},
		Send:      true,
	}
}
