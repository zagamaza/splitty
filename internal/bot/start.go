package bot

import (
	"github.com/almaznur91/splitty/internal/service"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
)

// StackOverflow bot, returns from "https://api.stackexchange.com/2.2/questions?order=desc&sort=activity&site=stackoverflow"
// reacts on "so!" prefix, i.e. "so! golang"
type Start struct {
	sc service.Service
}

// NewStackOverflow makes a bot for SO
func NewStart(service service.Service) *Start {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &Start{
		sc: service,
	}
}

// Help returns help message
func (s Start) Help() string {
	return genHelpMsg(s.ReactOn(), "1 случайный вопрос со StackOverflow")
}

// OnMessage returns one entry
func (s Start) OnMessage(msg Message) (response Response) {
	if !contains(s.ReactOn(), msg.Text) {
		return Response{}
	}

	return Response{
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
