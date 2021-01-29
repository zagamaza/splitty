package bot

import (
	"context"
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

// ReactOn keys
func (s RoomCreating) HasReact(u *api.Update) bool {
	if u.Button != nil && u.CallbackQuery != nil {
		return strings.Contains(u.Button.Action, "create_room")
	} else if u.Message != nil && u.Message.Chat.Type == "private" {
		return strings.Contains(u.Message.Text, "/start create_room")
	}
	return false
}

// OnMessage returns one entry
func (s RoomCreating) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}
	cs := &api.ChatState{UserId: int(getChatID(u)), Action: "create_room"}
	err := s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	b := &api.Button{Action: "cancel"}
	id, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Отмена", id.Hex())}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cgf.BotName+"?start=help")}

	if u.CallbackQuery != nil {
		tbMsg := tgbotapi.NewEditMessageText(getChatID(u), u.CallbackQuery.Message.ID, "Введите название комнаты и отправьте сообщение.")
		var keyboard [][]tgbotapi.InlineKeyboardButton
		tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: append(keyboard, button1, button2)}

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tbMsg},
			Send:      true,
		}
	} else {
		tbMsg := tgbotapi.NewMessage(getChatID(u), "Введите название комнаты и отправьте сообщение.")
		tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(button1, button2)

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tbMsg},
			Send:      true,
		}
	}

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

// ReactOn keys
func (rs RoomSetName) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message.Text == "" {
		return false
	}
	return strings.Contains(u.ChatState.Action, "create_room")
}

// OnMessage returns one entry
func (rs RoomSetName) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !rs.HasReact(u) {
		return api.TelegramMessage{}
	}
	defer func() {
		err := rs.css.DeleteById(ctx, u.ChatState.ID)
		if err != nil {
			log.Error().Err(err).Msg("")
		}
	}()

	r := &api.Room{
		Members: &[]api.User{u.Message.From},
		Name:    u.Message.Text,
	}

	room, err := rs.rs.CreateRoom(ctx, r)
	if err != nil {
		log.Error().Err(err).Msg("crete room failed")
		return api.TelegramMessage{}
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Комната *"+room.Name+"* создана, теперь добавьте бота в группу")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonSwitch("Опубликовать комнату в свой чат", room.Name)}

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
