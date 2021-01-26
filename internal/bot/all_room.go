package bot

import (
	"context"
	"encoding/json"
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

// OnMessage returns one entry
func (s AllRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}

	var rooms *[]api.Room
	var err error
	if u.InlineQuery.Query != "" {
		rooms, err = s.rs.FindRoomsByLikeName(ctx, u.InlineQuery.Query)
		if err != nil {
			log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
		}
	} else {
		rooms, err = s.rs.FindRoomsByUserId(ctx, u.InlineQuery.From.ID)
		if err != nil {
			log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
		}
	}
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
		callbackJson, err := json.Marshal(map[string]string{"RoomId": v.ID.Hex()})
		if err != nil {
			log.Error().Err(err).Msg("")
			return
		}
		b := &api.Button{Action: "join_room", CallbackData: callbackJson}
		cId, err := s.bs.Save(ctx, b)
		if err != nil {
			log.Error().Err(err).Msg("create btn failed")
			return
		}
		button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", cId.Hex())}
		button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+s.cfg.BotName+"?start=transaction")}

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

// ReactOn keys
func (s AllRoom) HasReact(u *api.Update) bool {
	if u.InlineQuery == nil {
		return false
	}
	return true
}
