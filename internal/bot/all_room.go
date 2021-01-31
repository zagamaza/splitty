package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// send /room, after click on the button 'Присоединиться'
type AllRoom struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAllRoom(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *AllRoom {
	return &AllRoom{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (s AllRoom) HasReact(u *api.Update) bool {
	if u.InlineQuery == nil {
		return false
	}
	return true
}

// OnMessage returns one entry
func (s AllRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	rooms := s.findRoomsByUpdate(ctx, u)

	var results []interface{}
	for _, v := range *rooms {
		text := "Экран комнаты *" + v.Name + "*\n\nУчастники:\n"
		for _, v := range *v.Members {
			text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
		}
		article := tgbotapi.NewInlineQueryResultArticle(primitive.NewObjectID().Hex(), v.Name, text)
		article.InputMessageContent = tgbotapi.InputTextMessageContent{
			Text:      text,
			ParseMode: tgbotapi.ModeMarkdown,
		}
		article.Description = "text"

		b, err := s.generateBtn(ctx, "join_room", &api.CallbackData{RoomId: v.ID.Hex()})
		if err != nil {
			continue
		}

		button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", b.ID.Hex())}
		button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+s.cfg.BotName+"?start=transaction"+v.ID.Hex())}

		var keyboard [][]tgbotapi.InlineKeyboardButton
		keyboard = append(keyboard, button1, button2)
		article.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		}

		results = append(results, article)
	}

	inlineConfig := &tgbotapi.InlineConfig{
		InlineQueryID: u.InlineQuery.ID,
		IsPersonal:    true,
		CacheTime:     0,
		Results:       results,
	}

	return api.TelegramMessage{
		InlineConfig: inlineConfig,
		Send:         true,
	}
}

func (s AllRoom) findRoomsByUpdate(ctx context.Context, u *api.Update) *[]api.Room {
	var err error
	var rooms *[]api.Room
	if u.InlineQuery.Query != "" {
		rooms, err = s.rs.FindRoomsByLikeName(ctx, u.InlineQuery.From.ID, u.InlineQuery.Query)
		if err != nil {
			log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
		}
	} else {
		rooms, err = s.rs.FindRoomsByUserId(ctx, u.InlineQuery.From.ID)
		if err != nil {
			log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
		}
	}
	return rooms

}

func (s AllRoom) generateBtn(ctx context.Context, action string, cd *api.CallbackData) (*api.Button, error) {
	b := &api.Button{Action: action, CallbackData: cd}
	cId, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return nil, err
	}
	b.ID = cId
	return b, nil
}
