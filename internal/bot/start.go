package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
	"strings"
)

// StackOverflow bot, returns from "https://api.stackexchange.com/2.2/questions?order=desc&sort=activity&site=stackoverflow"
// reacts on "so!" prefix, i.e. "so! golang"
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

// Help returns help message
func (s Start) Help() string {
	return genHelpMsg(s.ReactOn(), "1 случайный вопрос со StackOverflow")
}

// OnMessage returns one entry
func (s Start) OnMessage(msg api.Message) (response api.Response) {
	if !strings.Contains(msg.Text, s.ReactOn()[0]) {
		return api.Response{}
	}
	roomId := strings.ReplaceAll(msg.Text, "/start ", "")
	s.rs.JoinToRoom(context.Background(), &tgbotapi.User{}, roomId)
	return api.Response{
		Text:    "Приветики",
		Button:  []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Приветики", "http://t.me/ZagaMaza1_bot?start=G_LTQxNjk1MDk3N19JNQ==")},
		Send:    true,
		Preview: false,
	}
}

// ReactOn keys
func (s Start) ReactOn() []string {
	return []string{"start"}
}

type Room struct {
	us *service.UserService
	rs *service.RoomService
}

// NewStackOverflow makes a bot for SO
func NewRoom(s *service.UserService, rs *service.RoomService) *Start {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &Start{
		us: s,
		rs: rs,
	}
}

// Help returns help message
func (s Room) Help() string {
	return genHelpMsg(s.ReactOn(), "1 случайный вопрос со StackOverflow")
}

// OnMessage returns one entry
func (s Room) OnMessage(msg api.Message) (response api.Response) {
	if !contains(s.ReactOn(), msg.Text) {
		return api.Response{}
	}
	return api.Response{
		Text:    "Приветики",
		Button:  []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Приветики", "http://t.me/ZagaMaza1_bot?start=G_LTQxNjk1MDk3N19JNQ==")},
		Send:    true,
		Preview: false,
	}
}

// ReactOn keys
func (s Room) ReactOn() []string {
	return []string{"/start"}
}
