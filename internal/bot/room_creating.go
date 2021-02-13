package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"strings"
	"time"
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
		return u.Button.Action == createRoom
	} else if u.Message != nil && u.Message.Chat.Type == "private" {
		return strings.Contains(u.Message.Text, "/start "+string(createRoom))
	}
	return false
}

// OnMessage returns one entry
func (s RoomCreating) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: createRoom}
	err := s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	b := api.NewButton(viewStart, nil)
	id, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Отмена", id.Hex())}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cgf.BotName+"?start=help")}

	if u.CallbackQuery != nil {
		tbMsg := tgbotapi.NewEditMessageText(getChatID(u), u.CallbackQuery.Message.ID, "Введите название тусы и отправьте сообщение.")
		var keyboard [][]tgbotapi.InlineKeyboardButton
		tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: append(keyboard, button1, button2)}

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tbMsg},
			Send:      true,
		}
	} else {
		tbMsg := tgbotapi.NewMessage(getChatID(u), "Введите название тусы и отправьте сообщение.")
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
	if u.ChatState == nil || u.Message == nil {
		return false
	}
	return u.ChatState.Action == createRoom
}

// OnMessage returns one entry
func (rs RoomSetName) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	defer func() {
		err := rs.css.DeleteById(ctx, u.ChatState.ID)
		if err != nil {
			log.Error().Err(err).Msg("")
		}
	}()

	r := &api.Room{
		Members:    &[]api.User{u.Message.From},
		Name:       u.Message.Text,
		Operations: &[]api.Operation{},
		CreateAt:   time.Now(),
	}

	room, err := rs.rs.CreateRoom(ctx, r)
	if err != nil {
		log.Error().Err(err).Msg("crete room failed")
		return api.TelegramMessage{}
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Туса *"+room.Name+"* создана, теперь добавьте бота в группу")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonSwitch("Опубликовать тусу в свой чат", room.Name)}

	cb := api.NewButton(viewStart, nil)
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
