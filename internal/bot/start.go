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

// StackOverflow bot, returns from "https://api.stackexchange.com/2.2/questions?order=desc&sort=activity&site=stackoverflow"
// reacts on "so!" prefix, i.e. "so! golang"
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
	err := s.rs.JoinToRoom(context.Background(), msg.From, roomId)
	if err != nil {
		zlog.Error().Err(err).Msg("join to room failed")
		return api.Response{}
	}
	return api.Response{
		Text:    "Ништяк, Присоединился",
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

// NewRoom makes a bot for create new room
func NewRoom(s *service.UserService, rs *service.RoomService) *Room {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &Room{
		us: s,
		rs: rs,
	}
}

// Help returns help message
func (r Room) Help() string {
	return genHelpMsg(r.ReactOn(), "1 случайный вопрос со StackOverflow")
}

// OnMessage returns one entry
func (r Room) OnMessage(msg api.Message) (response api.Response) {
	if !contains(r.ReactOn(), msg.Text) {
		return api.Response{}
	}
	room, err := r.rs.CreateRoom(context.Background(), msg.From)
	if err != nil {
		zlog.Error().Err(err).Msg("crete room failed")
		return api.Response{}
	}
	rId := room.ID.Hex()
	return api.Response{
		Text:    "Приветики",
		Button:  []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Присоединиться", "http://t.me/ZagaMaza1_bot?start="+rId)},
		Send:    true,
		Preview: false,
	}
}

// ReactOn keys
func (r Room) ReactOn() []string {
	return []string{"/room"}
}
