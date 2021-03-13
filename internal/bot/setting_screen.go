package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

type RoomSetting struct {
	bs  ButtonService
	rs  RoomService
	css ChatStateService
	cfg *Config
}

func NewRoomSetting(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config) *RoomSetting {
	return &RoomSetting{
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (bot RoomSetting) HasReact(u *api.Update) bool {
	return hasAction(u, roomSetting)
}

func (bot *RoomSetting) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	roomId := u.Button.CallbackData.RoomId
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}

	text := I18n(u.User, "scrn_room_setting", room.Name)

	var buttons []tgbotapi.InlineKeyboardButton
	var toSave []*api.Button

	if isArchived(room, getFrom(u)) {
		btn := api.NewButton(unArchiveRoom, &api.CallbackData{RoomId: roomId})
		toSave = append(toSave, btn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_return_from_archive"), btn.ID.Hex()))
	} else {
		btn := api.NewButton(archiveRoom, &api.CallbackData{RoomId: roomId})
		toSave = append(toSave, btn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_do_archive"), btn.ID.Hex()))
	}

	backB := api.NewButton(viewRoom, u.Button.CallbackData)
	toSave = append(toSave, backB)
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))

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
	vsBot *RoomSetting
	cfg   *Config
}

func NewArchiveRoom(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config, viewSetting *RoomSetting) *ArchiveRoom {
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
		if err := bot.rs.ArchiveRoom(ctx, getFrom(u).ID, roomId); err != nil {
			log.Error().Err(err).Msg("")
		}
	} else {
		if err := bot.rs.UnArchiveRoom(ctx, getFrom(u).ID, roomId); err != nil {
			log.Error().Err(err).Msg("")
		}
	}

	u.Button = api.NewButton(roomSetting, u.Button.CallbackData)
	return api.TelegramMessage{
		CallbackConfig: errCallback,
		Redirect:       u,
		Send:           true,
	}
}

type UserSetting struct {
	bs  ButtonService
	us  UserService
	css ChatStateService
	cfg *Config
}

func NewUserSetting(bs ButtonService, us UserService, css ChatStateService, cfg *Config) *UserSetting {
	return &UserSetting{
		bs:  bs,
		us:  us,
		cfg: cfg,
		css: css,
	}
}

func (bot UserSetting) HasReact(u *api.Update) bool {
	return hasAction(u, userSetting) || hasAction(u, selectedLanguage)
}

func (bot *UserSetting) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	lang := api.DefineLang(u.User)
	if u.Button.Action == selectedLanguage {
		lang = u.Button.CallbackData.ExternalId
		u.User.SelectedLang = lang
		if err := bot.us.UpsertLangUser(ctx, u.User.ID, lang); err != nil {
			log.Error().Err(err).Msg("upsert lang failed btn failed")
		}
	}
	langBtn := api.NewButton(chooseLanguage, new(api.CallbackData))
	backBtn := api.NewButton(viewStart, new(api.CallbackData))
	if _, err := bot.bs.SaveAll(ctx, langBtn, backBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	screen := createScreen(u, I18n(u.User, "scrn_user_setting"), &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_language", bot.defineFlag(lang)), langBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backBtn.ID.Hex())},
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

func (bot UserSetting) defineFlag(lang string) string {
	if lang == "ru" {
		return "🇷🇺"
	} else {
		return "🇬🇧"
	}
}

type ChooseLanguage struct {
	bs  ButtonService
	rs  RoomService
	css ChatStateService
	cfg *Config
}

func NewChooseLanguage(bs ButtonService, rs RoomService, css ChatStateService, cfg *Config) *ChooseLanguage {
	return &ChooseLanguage{
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (bot ChooseLanguage) HasReact(u *api.Update) bool {
	return hasAction(u, chooseLanguage)
}

func (bot *ChooseLanguage) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	enBtn := api.NewButton(selectedLanguage, &api.CallbackData{ExternalId: "en"})
	ruBtn := api.NewButton(selectedLanguage, &api.CallbackData{ExternalId: "ru"})
	backBtn := api.NewButton(userSetting, new(api.CallbackData))

	if _, err := bot.bs.SaveAll(ctx, enBtn, ruBtn, backBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	text := I18n(u.User, "scrn_choose_lang")
	screen := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_rus_language"), ruBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_eng_language"), enBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backBtn.ID.Hex())},
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
