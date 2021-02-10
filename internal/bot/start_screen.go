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
	CleanChatState(ctx context.Context, state *api.ChatState)
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
	if hasAction(u, viewStart) {
		return true
	} else if isPrivate(u) {
		return u.Message != nil && u.Message.Text == start
	} else {
		return u.Message != nil && u.Message.Text == start+"@"+s.cfg.BotName
	}
}

// OnMessage returns one entry
func (s *StartScreen) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer s.css.CleanChatState(ctx, u.ChatState)

	var createB tgbotapi.InlineKeyboardButton
	var allRoomB tgbotapi.InlineKeyboardButton
	if isPrivate(u) {
		cb := api.NewButton(createRoom, new(api.CallbackData))
		arb := api.NewButton(viewAllRooms, new(api.CallbackData))
		if _, err := s.bs.SaveAll(ctx, cb, arb); err != nil {
			log.Error().Err(err).Msg("save btn failed")
			return
		}
		allRoomB = tgbotapi.NewInlineKeyboardButtonData("Все тусы", arb.ID.Hex())
		createB = tgbotapi.NewInlineKeyboardButtonData("Создать новую тусу", cb.ID.Hex())
	} else {
		allRoomB = NewButtonSwitchCurrent("Все тусы", "")
		createB = tgbotapi.NewInlineKeyboardButtonURL("Создать новую тусу", "http://t.me/"+s.cfg.BotName+"?start=create_room")
	}

	screen := createScreen(u, "Главный экран", &[][]tgbotapi.InlineKeyboardButton{
		{allRoomB},
		{createB},
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
