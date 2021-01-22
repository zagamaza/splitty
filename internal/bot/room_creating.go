package bot

import (
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"strings"
)

type RoomCreating struct {
	css ChatStateService
	bs  ButtonService
	cgf *Config
}

// NewRoomCreating makes a bot for screen room creating
func NewRoomCreating(s ChatStateService, bs ButtonService, cfg *Config) *RoomCreating {
	return &RoomCreating{
		css: s,
		bs:  bs,
		cgf: cfg,
	}
}

// OnMessage returns one entry
func (s RoomCreating) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}
	cs := &api.ChatState{UserId: u.CallbackQuery.From.ID, Action: "create_room"}
	err := s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Введите название комнаты и отправьте сообщение.")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	b := &api.Button{Action: "cancel"}
	id, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Отмена", id.Hex())}
	button3 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cgf.BotName+"?start=help")}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(button2, button3)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (s RoomCreating) HasReact(u *api.Update) bool {
	if u.Button == nil || u.CallbackQuery == nil {
		return false
	}
	return strings.Contains(u.Button.Action, "create_room")
}

type RoomSetName struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cgf *Config
}

// NewRoomCreating makes a bot for screen room creating
func NewRoomSetName(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *RoomSetName {
	return &RoomSetName{
		css: s,
		bs:  bs,
		rs:  rs,
		cgf: cfg,
	}
}

// OnMessage returns one entry
func (rs RoomSetName) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !rs.HasReact(u) {
		return api.TelegramMessage{}
	}

	r := &api.Room{
		Members: &[]api.User{u.Message.From},
		Name:    u.Message.Text,
	}

	room, err := rs.rs.CreateRoom(ctx, r)
	if err != nil {
		log.Error().Err(err).Msg("crete room failed")
		return api.TelegramMessage{}
	}

	rId := room.ID.Hex()
	tbMsg := tgbotapi.NewMessage(getChatID(u), "Комната создана, попросить остальных участников присоединиться к группе")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	callbackJson, err := json.Marshal(map[string]string{"RoomId": rId})
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}

	b := &api.Button{Action: "join_room", CallbackData: string(callbackJson)}
	cId, err := rs.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", cId.Hex())}

	cb := &api.Button{Action: "cancel"}
	cancelId, err := rs.bs.Save(ctx, cb)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Отмена", cancelId.Hex())}
	button3 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+rs.cgf.BotName+"?start=help")}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(button1, button2, button3)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (rs RoomSetName) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message.Text == "" {
		return false
	}
	return strings.Contains(u.ChatState.Action, "create_room")
}
