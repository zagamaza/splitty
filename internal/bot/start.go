package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	zlog "github.com/rs/zerolog/log"
	"log"
	"strings"
)

type UserService interface {
	UpsertUser(ctx context.Context, u api.User) error
}

type RoomService interface {
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	CreateRoom(ctx context.Context, u *api.Room) (*api.Room, error)
	FindById(ctx context.Context, id string) (*api.Room, error)
}

type Config struct {
	BotName    string
	SuperUsers []string
}

// send /room, after click on the button 'Присоединиться'
type Start struct {
	us  UserService
	rs  RoomService
	cgf *Config
}

func NewStart(s UserService, rs RoomService, cfg *Config) *Start {
	return &Start{
		us:  s,
		rs:  rs,
		cgf: cfg,
	}
}

// OnMessage returns one entry
func (s Start) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}

	//todo: думаю нужен отдельный бот и выставить его в самое начало, т.к. выполняется для всех запросов без условия
	if err := s.us.UpsertUser(ctx, u.Message.From); err != nil {
		log.Printf("[WARN] failed to respond on update, %v", err)
	}

	roomId := strings.ReplaceAll(u.Message.Text, "/start ", "")
	err := s.rs.JoinToRoom(ctx, u.Message.From, roomId)
	if err != nil {
		zlog.Error().Err(err).Msg("join to room failed")
		return api.TelegramMessage{}
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), "Ништяк, Присоединился")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (s Start) HasReact(u *api.Update) bool {
	if u.Message == nil {
		return false
	}
	return strings.Contains(u.Message.Text, "/start ")
}

type Room struct {
	us  UserService
	rs  RoomService
	cgf *Config
}

// NewRoom makes a bot for create new room
func NewRoom(us UserService, rs RoomService, cfg *Config) *Room {
	return &Room{
		us:  us,
		rs:  rs,
		cgf: cfg,
	}
}

// OnMessage returns one entry
func (r Room) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if !r.HasReact(u) {
		return api.TelegramMessage{}
	}
	if u.Message.Chat.Type == "private" {

		tbMsg := tgbotapi.NewMessage(getChatID(u), "*Используйте эту команду в групповом чате*\n\nПосле добавления в группу не забудьте дать права администратора и повторно отправить команду /room")
		tbMsg.ParseMode = tgbotapi.ModeMarkdown

		joinUrl := fmt.Sprintf("http://t.me/%s?startgroup=true", r.cgf.BotName)
		buttons := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить бота в свой чат", joinUrl)}
		tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons)

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tbMsg},
			Send:      true,
		}
	}

	room, err := r.rs.CreateRoom(ctx, &api.Room{})
	if err != nil {
		zlog.Error().Err(err).Msg("crete room failed")
		return api.TelegramMessage{}
	}
	rId := room.ID.Hex()
	tbMsg := tgbotapi.NewMessage(getChatID(u), "Приветики")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	joinUrl := fmt.Sprintf("http://t.me/%s?start=%s", r.cgf.BotName, rId)
	buttons := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Присоединиться", joinUrl)}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (r Room) HasReact(u *api.Update) bool {
	if u.Message == nil {
		return false
	}
	return strings.Contains(u.Message.Text, "/room")
}

func getChatID(update *api.Update) int64 {
	var chatId int64
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		chatId = update.CallbackQuery.Message.Chat.ID
	} else {
		chatId = update.Message.Chat.ID
	}
	return chatId
}
