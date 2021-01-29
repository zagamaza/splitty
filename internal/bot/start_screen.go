package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatStateService interface {
	Save(ctx context.Context, u *api.ChatState) error
	DeleteById(ctx context.Context, id primitive.ObjectID) error
	FindByUserId(ctx context.Context, userId int) (*api.ChatState, error)
}

type ButtonService interface {
	Save(ctx context.Context, u *api.Button) (primitive.ObjectID, error)
	SaveAll(ctx context.Context, b ...*api.Button) ([]*api.Button, error)
}

// send /room, after click on the button 'Присоединиться'
type StartScreen struct {
	css ChatStateService
	bs  ButtonService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewStartScreen(s ChatStateService, bs ButtonService, cfg *Config) *StartScreen {
	return &StartScreen{
		css: s,
		bs:  bs,
		cfg: cfg,
	}
}

// OnMessage returns one entry
func (s StartScreen) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Главный экран")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	empty := ""
	kb := tgbotapi.InlineKeyboardButton{
		SwitchInlineQueryCurrentChat: &empty,
		Text:                         "Все комнаты",
	}
	button1 := []tgbotapi.InlineKeyboardButton{kb}

	var button2 []tgbotapi.InlineKeyboardButton
	if u.Message.Chat.Type != "private" {
		button2 = []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Создать новую комнату", "http://t.me/"+s.cfg.BotName+"?start=create_room")}

	} else {
		b := &api.Button{Action: "create_room"}
		id, err := s.bs.Save(ctx, b)
		if err != nil {
			log.Error().Err(err).Msg("create btn failed")
			return
		}
		button2 = []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Создать новую комнату", id.Hex())}
	}

	button3 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cfg.BotName+"?start=")}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(button1, button2, button3)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (s StartScreen) HasReact(u *api.Update) bool {
	if u.Message == nil {
		return false
	}
	if u.Message.Chat.Type != "private" {
		return u.Message.Text == start
	} else {
		return u.Message.Text == start+s.cfg.BotName
	}
}
