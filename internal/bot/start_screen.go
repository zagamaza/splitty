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

// ReactOn keys
func (s StartScreen) HasReact(u *api.Update) bool {
	if u.IsPrivat() {
		return u.Message != nil && u.Message.Text == start
	} else {
		return u.Message != nil && u.Message.Text == start+"@"+s.cfg.BotName
	}
}

// OnMessage returns one entry
func (s *StartScreen) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	var createB tgbotapi.InlineKeyboardButton
	if u.IsPrivat() {
		cb := api.NewButton(createRoom, nil)
		if _, err := s.bs.SaveAll(ctx, cb); err != nil {
			log.Error().Err(err).Msg("save btn failed")
			return
		}
		createB = tgbotapi.NewInlineKeyboardButtonData("Создать новую комнату", cb.ID.Hex())
	} else {
		createB = tgbotapi.NewInlineKeyboardButtonURL("Создать новую комнату", "http://t.me/"+s.cfg.BotName+"?start=create_room")
	}

	tbMsg := NewMessage(getChatID(u), "Главный экран",
		[][]tgbotapi.InlineKeyboardButton{
			{NewButtonSwitchCurrent("Все комнаты", "")},
			{createB},
		})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}
