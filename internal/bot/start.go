package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	zlog "github.com/rs/zerolog/log"
	"log"
	"strings"
)

// send /room, after click on the button 'Присоединиться'
type Start struct {
	us *service.UserService
	rs *service.RoomService
}

// NewStackOverflow makes a bot for SO
func NewStart(s *service.UserService, rs *service.RoomService) *Start {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &Start{
		us: s,
		rs: rs,
	}
}

// OnMessage returns one entry
func (s Start) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if !s.HasReact(u) {
		return api.TelegramMessage{}
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
	us *service.UserService
	rs *service.RoomService
}

// NewRoom makes a bot for create new room
func NewRoom(s *service.UserService, rs *service.RoomService) *Room {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &Room{
		us: s,
		rs: rs,
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

		//TODO сделать чтобы наименование бота тянулось из конфигов
		buttons := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить бота в свой чат", "http://t.me/ZagaMaza1_bot?startgroup=true")}
		tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons)

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tbMsg},
			Send:      true,
		}
	}

	room, err := r.rs.CreateRoom(ctx, u.Message.From)
	if err != nil {
		zlog.Error().Err(err).Msg("crete room failed")
		return api.TelegramMessage{}
	}
	rId := room.ID.Hex()
	tbMsg := tgbotapi.NewMessage(getChatID(u), "Приветики")
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	//TODO сделать чтобы наименование бота тянулось из конфигов
	buttons := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Присоединиться", "http://t.me/ZagaMaza1_bot?start="+rId)}
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// ReactOn keys
func (r Room) HasReact(u *api.Update) bool {
	return strings.Contains(u.Message.Text, "/room")
}

func getChatID(update *api.Update) int64 {
	var chatId int64
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		chatId = update.CallbackQuery.Message.Chat.ID
	}
	chatId = update.Message.Chat.ID
	return chatId
}
