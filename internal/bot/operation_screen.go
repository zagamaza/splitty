package bot

import (
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"strings"
)

// Operation show screen with donar/recepient buttons
type Operation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewOperation(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *Operation {
	return &Operation{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// OnMessage returns one entry
func (s Operation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}
	roomId := strings.ReplaceAll(u.Message.Text, "/start transaction", "")
	room, err := s.rs.FindById(ctx, roomId)

	if containsUserId(room.Members, getFrom(u).ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), "К сожалению, вы не находитесь в этой комнате")},
			Send:      true,
		}
	}

	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Выбор операции для комнаты *"+room.Name+"*")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	cd, err := json.Marshal(map[string]string{"RoomId": room.ID.Hex()})
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}

	recipientBtn := &api.Button{Action: "recipient", CallbackData: cd}
	donorBtn := &api.Button{Action: "donor", CallbackData: cd}
	_, err = s.bs.SaveAll(ctx, recipientBtn, donorBtn)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Расход", recipientBtn.ID.Hex())}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Приход", donorBtn.ID.Hex())}
	button3 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cfg.BotName+"?start=")}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(button1, button2, button3)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s Operation) HasReact(u *api.Update) bool {
	if u.Message == nil || u.Message.Chat.Type != "private" {
		return false
	}
	return strings.Contains(u.Message.Text, "/start transaction")
}

func containsUserId(users *[]api.User, id int) bool {
	for _, u := range *users {
		if u.ID == id {
			return true
		}
	}
	return false
}
