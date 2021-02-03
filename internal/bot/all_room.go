package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
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
func (arBot AllRoom) HasReact(u *api.Update) bool {
	if u.InlineQuery == nil {
		return false
	}
	return true
}

// OnMessage returns one entry
func (arBot *AllRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	rooms := arBot.findRoomsByUpdate(ctx, u)

	var results []interface{}
	for _, room := range *rooms {

		joinB := api.NewButton(joinRoom, &api.CallbackData{RoomId: room.ID.Hex()})
		if _, err := arBot.bs.SaveAll(ctx, joinB); err != nil {
			log.Error().Err(err).Msg("create btn failed")
			continue
		}

		article := NewInlineResultArticle(room.Name, "", createRoomInfoText(&room),
			[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+arBot.cfg.BotName+"?start=operation"+room.ID.Hex())},
			})
		results = append(results, article)
	}

	return api.TelegramMessage{
		InlineConfig: NewInlineConfig(u.InlineQuery.ID, results),
		Send:         true,
	}
}

func createRoomInfoText(r *api.Room) string {
	text := "Экран комнаты *" + r.Name + "*\n\nУчастники:\n"
	for _, v := range *r.Members {
		text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
	}
	return text
}

func (arBot AllRoom) findRoomsByUpdate(ctx context.Context, u *api.Update) *[]api.Room {
	rooms, err := arBot.rs.FindRoomsByLikeName(ctx, u.InlineQuery.From.ID, u.InlineQuery.Query)
	if err != nil {
		log.Error().Err(err).Msgf("can't send query to telegram %v", u.InlineQuery.From.ID)
	}
	return rooms
}
