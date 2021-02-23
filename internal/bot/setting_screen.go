package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

type ViewSetting struct {
	bs  ButtonService
	rs  RoomService
	css ChatStateService
	cfg *Config
}

func NewViewSetting(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config) *ViewSetting {
	return &ViewSetting{
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (bot ViewSetting) HasReact(u *api.Update) bool {
	return hasAction(u, viewSetting)
}

func (bot *ViewSetting) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	roomId := u.Button.CallbackData.RoomId
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}

	if !containsUserId(room.Members, getFrom(u).ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), "К сожалению, вы не находитесь в этой тусе")},
			Send:      true,
		}
	}

	text := "Настройки тусы *" + room.Name + "*"

	var buttons []tgbotapi.InlineKeyboardButton
	var toSave []*api.Button

	if isArchived(room, getFrom(u)) {
		btn := api.NewButton(unArchiveRoom, &api.CallbackData{RoomId: roomId})
		toSave = append(toSave, btn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Вернуть из архива", btn.ID.Hex()))
	} else {
		btn := api.NewButton(archiveRoom, &api.CallbackData{RoomId: roomId})
		toSave = append(toSave, btn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Архивировать", btn.ID.Hex()))
	}

	backB := api.NewButton(viewRoom, u.Button.CallbackData)
	toSave = append(toSave, backB)
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", backB.ID.Hex()))

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	keyboard := splitKeyboardButtons(buttons, 1)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboard)},
		Send:      true,
	}
}

type ArchiveRoom struct {
	bs    ButtonService
	rs    RoomService
	css   ChatStateService
	vsBot *ViewSetting
	cfg   *Config
}

func NewArchiveRoom(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config, viewSetting *ViewSetting) *ArchiveRoom {
	return &ArchiveRoom{
		bs:    bs,
		rs:    rs,
		cfg:   cfg,
		css:   css,
		vsBot: viewSetting,
	}
}

func (bot ArchiveRoom) HasReact(u *api.Update) bool {
	return hasAction(u, archiveRoom) || hasAction(u, unArchiveRoom)
}

func (bot *ArchiveRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	roomId := u.Button.CallbackData.RoomId

	var errCallback *tgbotapi.CallbackConfig = nil
	if hasAction(u, archiveRoom) {
		bot.rs.ArchiveRoom(ctx, getFrom(u).ID, roomId)
	} else {
		bot.rs.UnArchiveRoom(ctx, getFrom(u).ID, roomId)
	}

	u.Button = api.NewButton(viewSetting, u.Button.CallbackData)
	return api.TelegramMessage{
		CallbackConfig: errCallback,
		Redirect:       u,
		Send:           true,
	}
}
