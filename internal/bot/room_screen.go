package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

// send /room, after click on the button 'Присоединиться'
type JoinRoom struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewJoinRoom(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *JoinRoom {
	return &JoinRoom{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (s JoinRoom) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == joinRoom
}

// OnMessage returns one entry
func (s JoinRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId

	err := s.rs.JoinToRoom(ctx, u.CallbackQuery.From, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("join room failed %v", roomId)
		return
	}

	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msgf("get room failed %v", roomId)
		return
	}
	text := "Экран комнаты *" + room.Name + "*\n\nУчастники:\n"
	for _, v := range *room.Members {
		text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
	}

	tbMsg := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			InlineMessageID: u.CallbackQuery.InlineMessageID,
		},
		Text: text,
	}
	tbMsg.ParseMode = tgbotapi.ModeMarkdown

	b := &api.Button{Action: joinRoom, CallbackData: u.Button.CallbackData}
	cId, err := s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	button1 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", cId.Hex())}
	button2 := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+s.cfg.BotName+"?start=operation"+room.ID.Hex())}

	var keyboard [][]tgbotapi.InlineKeyboardButton
	keyboard = append(keyboard, button1, button2)
	tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// send /room, after click on the button 'Присоединиться'
type ViewRoom struct {
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewRoom(bs ButtonService, rs RoomService, cfg *Config) *ViewRoom {
	return &ViewRoom{
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (vr ViewRoom) HasReact(u *api.Update) bool {
	return u.Button != nil && u.Button.Action == viewRoom
}

// OnMessage returns one entry
func (vr *ViewRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	room, err := vr.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}
	joinB := api.NewButton(joinRoom, u.Button.CallbackData)
	if _, err := vr.bs.SaveAll(ctx, joinB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	screen := BuildScreen(
		&ScreenTemplate{
			ChatId:    getChatID(u),
			MessageId: u.CallbackQuery.Message.ID,
			Text:      createRoomInfoText(room),
			Keyboard: &[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Присоединиться", joinB.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonURL("Добавить операцию", "http://t.me/"+vr.cfg.BotName+"?start=operation"+room.ID.Hex())},
			},
		}, editMessage)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
