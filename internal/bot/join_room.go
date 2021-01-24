package bot

import (
	"context"
	"encoding/json"
	"fmt"
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
}

// NewStackOverflow makes a bot for SO
func NewJoinRoom(s ChatStateService, bs ButtonService, rs RoomService) *JoinRoom {
	return &JoinRoom{
		css: s,
		bs:  bs,
		rs:  rs,
	}
}

// OnMessage returns one entry
func (s JoinRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}

	var cd map[string]string
	err := json.Unmarshal(u.Button.CallbackData, &cd)
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}

	err = s.rs.JoinToRoom(ctx, u.CallbackQuery.From, cd["RoomId"])
	if err != nil {
		log.Error().Err(err).Msgf("join room failed %v", cd["RoomId"])
		return
	}

	room, err := s.rs.FindById(ctx, cd["RoomId"])
	if err != nil {
		log.Error().Err(err).Msgf("get room failed %v", cd["RoomId"])
		return
	}
	text := "Экран комнаты *" + room.Name + "*\n\nУчастники:\n"
	for _, v := range *room.Members {
		text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
	}

	tbMsg := tgbotapi.NewEditMessageText(getChatID(u), u.CallbackQuery.Message.ID, text)
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	b := &api.Button{Action: "join_room", CallbackData: u.Button.CallbackData}
	cId, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", cId.Hex())}

	var keyboard [][]tgbotapi.InlineKeyboardButton
	keyboard = append(keyboard, button1)
	tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (s JoinRoom) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return strings.Contains(u.Button.Action, "join_room")
}
