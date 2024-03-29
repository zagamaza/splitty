package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"strconv"
	"strings"
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

	exitRoomBtn := api.NewButton(exitRoom, &api.CallbackData{RoomId: roomId})
	toSave = append(toSave, exitRoomBtn)
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_exit"), exitRoomBtn.ID.Hex()))

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
	rss   RoomStateService
	rs    RoomService
	css   ChatStateService
	vsBot *RoomSetting
	cfg   *Config
}

func NewArchiveRoom(bs ButtonService, rss RoomStateService, rs RoomService, css ChatStateService, cfg *Config, viewSetting *RoomSetting) *ArchiveRoom {
	return &ArchiveRoom{
		bs:    bs,
		rs:    rs,
		rss:   rss,
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
		if err := bot.rss.ArchiveRoom(ctx, getFrom(u).ID, roomId); err != nil {
			log.Error().Err(err).Msg("")
		}
	} else {
		if err := bot.rss.UnArchiveRoom(ctx, getFrom(u).ID, roomId); err != nil {
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
	defer bot.css.CleanChatState(ctx, u.ChatState)

	lang := api.DefineLang(u.User)
	if u.Button.Action == selectedLanguage {
		lang = u.Button.CallbackData.ExternalId
		u.User.SelectedLang = lang
		if err := bot.us.SetUserLang(ctx, u.User.ID, lang); err != nil {
			log.Error().Err(err).Msg("upsert lang failed btn failed")
		}
	}
	langBtn := api.NewButton(chooseLanguage, new(api.CallbackData))
	notificationBtn := api.NewButton(chooseNotification, new(api.CallbackData))
	countInPageBtn := api.NewButton(countInPage, new(api.CallbackData))
	bankDetailsBtn := api.NewButton(bankDetailsView, new(api.CallbackData))
	backBtn := api.NewButton(viewStart, new(api.CallbackData))
	if _, err := bot.bs.SaveAll(ctx, langBtn, notificationBtn, backBtn, bankDetailsBtn, countInPageBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	screen := createScreen(u, I18n(u.User, "scrn_user_setting"), &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_language", bot.defineFlag(lang)), langBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_notification", bot.defineNotification(u.User)), notificationBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_count_in_page", bot.defineNumberEmoji(u)), countInPageBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_bank_details_view"), bankDetailsBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backBtn.ID.Hex())},
	})
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

func (bot *UserSetting) defineNumberEmoji(u *api.Update) string {
	if u.User.CountInPage == 10 {
		return "\U0001f51f"
	}
	return strconv.Itoa(u.User.CountInPage) + "\ufe0f\u20e3"
}

func (bot UserSetting) defineFlag(lang string) string {
	if lang == "ru" {
		return "🇷🇺"
	} else {
		return "🇬🇧"
	}
}

func (bot UserSetting) defineNotification(u *api.User) string {
	if *u.NotificationOn {
		return "🔔"
	} else {
		return "🔕"
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

type ChooseNotification struct {
	bs  ButtonService
	css ChatStateService
	cfg *Config
}

func NewChooseNotification(bs ButtonService, css ChatStateService, cfg *Config) *ChooseNotification {
	return &ChooseNotification{
		bs:  bs,
		cfg: cfg,
		css: css,
	}
}

func (bot ChooseNotification) HasReact(u *api.Update) bool {
	return hasAction(u, chooseNotification) || hasAction(u, chooseNotification)
}

func (bot *ChooseNotification) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	turnBtn := api.NewButton(selectedNotification, &api.CallbackData{})
	backBtn := api.NewButton(userSetting, new(api.CallbackData))

	if _, err := bot.bs.SaveAll(ctx, turnBtn, backBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	text := I18n(u.User, "scrn_choose_notification", bot.defineSwitcher(u.User))
	screen := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(bot.defineBtnSwitcher(u.User), turnBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backBtn.ID.Hex())},
	})

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

func (bot ChooseNotification) defineSwitcher(u *api.User) string {
	if *u.NotificationOn {
		return I18n(u, "msg_on")
	} else {
		return I18n(u, "msg_off")
	}
}

func (bot ChooseNotification) defineBtnSwitcher(u *api.User) string {
	if !*u.NotificationOn {
		return I18n(u, "btn_on")
	} else {
		return I18n(u, "btn_off")
	}
}

type SelectedNotification struct {
	bs  ButtonService
	us  UserService
	css ChatStateService
	cfg *Config
}

func NewSelectedNotification(bs ButtonService, us UserService, css ChatStateService, cfg *Config) *SelectedNotification {
	return &SelectedNotification{
		bs:  bs,
		us:  us,
		cfg: cfg,
		css: css,
	}
}

func (bot SelectedNotification) HasReact(u *api.Update) bool {
	return hasAction(u, selectedNotification)
}

func (bot *SelectedNotification) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	*u.User.NotificationOn = *u.User.NotificationOn == false
	if err := bot.us.SetNotificationUser(ctx, u.User.ID, *u.User.NotificationOn); err != nil {
		log.Error().Err(err).Msg("")
	}

	u.Button = api.NewButton(userSetting, u.Button.CallbackData)
	return api.TelegramMessage{
		Send:     true,
		Redirect: u,
	}
}

type SelectedLeaveRoom struct {
	bs  ButtonService
	us  UserService
	rs  RoomService
	css ChatStateService
	cfg *Config
}

func NewSelectedLeaveRoom(bs ButtonService, us UserService, rs RoomService, css ChatStateService, cfg *Config) *SelectedLeaveRoom {
	return &SelectedLeaveRoom{
		bs:  bs,
		us:  us,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (bot SelectedLeaveRoom) HasReact(u *api.Update) bool {
	return hasAction(u, exitRoom)
}

func (bot *SelectedLeaveRoom) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := bot.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	userID := u.User.ID
	for _, o := range *room.Operations {
		if o.Donor.ID == userID || containsUserId(o.Recipients, userID) {
			callback := createCallback(u, I18n(u.User, "msg_you_can_not_leave"), true)
			return api.TelegramMessage{
				CallbackConfig: callback,
				Send:           true,
			}
		}
	}

	err = bot.rs.LeaveRoom(ctx, userID, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}
	u.Button = api.NewButton(viewStart, u.Button.CallbackData)
	callback := createCallback(u, I18n(u.User, "msg_you_left"), true)
	return api.TelegramMessage{
		Send:           true,
		Redirect:       u,
		CallbackConfig: callback,
	}
}

type ChooseCountInPage struct {
	bs  ButtonService
	css ChatStateService
	us  UserService
	cfg *Config
}

func NewChooseCountInPage(bs ButtonService, css ChatStateService, us UserService, cfg *Config) *ChooseCountInPage {
	return &ChooseCountInPage{
		bs:  bs,
		us:  us,
		cfg: cfg,
		css: css,
	}
}

func (bot ChooseCountInPage) HasReact(u *api.Update) bool {
	return hasAction(u, countInPage)
}

func (bot *ChooseCountInPage) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if u.User.CountInPage == 10 {
		u.User.CountInPage = 5
		if err := bot.us.SetCountInPage(ctx, u.User.ID, u.User.CountInPage); err != nil {
			log.Error().Err(err).Msg("")
			return
		}
	} else {
		u.User.CountInPage = u.User.CountInPage + 1
		if err := bot.us.SetCountInPage(ctx, u.User.ID, u.User.CountInPage); err != nil {
			log.Error().Err(err).Msg("")
			return
		}
	}
	u.Button.Action = userSetting
	return api.TelegramMessage{
		Redirect: u,
		Send:     true,
	}
}

type FinishedAddOperation struct {
	bs  ButtonService
	css ChatStateService
	rs  RoomService
	rss RoomStateService
	us  UserService
	cfg *Config
}

func NewFinishedAddOperation(bs ButtonService, css ChatStateService, rs RoomService, rss RoomStateService, us UserService, cfg *Config) *FinishedAddOperation {
	return &FinishedAddOperation{
		bs:  bs,
		us:  us,
		rs:  rs,
		rss: rss,
		cfg: cfg,
		css: css,
	}
}

func (bot FinishedAddOperation) HasReact(u *api.Update) bool {
	return hasAction(u, finishedAddOperation)
}

func (bot *FinishedAddOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := bot.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	countUsersFinishedAddOperation := len(room.RoomStates.FinishedAddOperation)
	if len(*room.Members) == countUsersFinishedAddOperation && u.Button.CallbackData.ExternalData == "false" {
		callback := createCallback(u, I18n(u.User, "msg_all_members_add_operation"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	if u.Button.CallbackData.ExternalData == "true" {
		countUsersFinishedAddOperation++
		if err = bot.rss.FinishedAddOperation(ctx, u.User.ID, u.Button.CallbackData.RoomId); err != nil {
			log.Error().Err(err).Msg("")
		}
	} else {
		countUsersFinishedAddOperation--
		if err = bot.rss.UnFinishedAddOperation(ctx, u.User.ID, u.Button.CallbackData.RoomId); err != nil {
			log.Error().Err(err).Msg("")
		}
	}

	var buttons []*api.Button
	var messages []tgbotapi.Chattable
	if len(*room.Members) == countUsersFinishedAddOperation {
		for _, user := range *room.Members {
			rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
			viewUserOpsB := api.NewButton(viewUserDebts, &api.CallbackData{RoomId: room.ID.Hex()})
			setBankBtn := api.NewButton(bankDetailsWantSet, &api.CallbackData{RoomId: room.ID.Hex(), ExternalData: string(viewRoom)})
			backB := api.NewButton(viewStart, &api.CallbackData{})
			buttons = append(buttons, rb, viewUserOpsB, setBankBtn, backB)

			user, err := bot.us.FindById(ctx, user.ID)
			if err != nil {
				log.Error().Err(err).Msg("")
				continue
			}
			text := I18n(user, "scrn_all_operations_added", userLink(user), room.Name)
			if user.BankDetails != "" {
				text += I18n(user, "scrn_all_bank_details", user.BankDetails)
			} else {
				text += I18n(user, "scrn_all_operations_ps")
			}
			msg := NewMessage(int64(user.ID), text,
				[][]tgbotapi.InlineKeyboardButton{
					{tgbotapi.NewInlineKeyboardButtonData(I18n(user, "btn_view_room"), rb.ID.Hex())},
					{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_user_debts"), viewUserOpsB.ID.Hex())},
					{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_edit_operation_add"), setBankBtn.ID.Hex())},
					{tgbotapi.NewInlineKeyboardButtonData(I18n(user, "btn_to_start"), backB.ID.Hex())},
				})
			messages = append(messages, msg)
		}
	}

	viewRoomBtn := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.Button.CallbackData.RoomId})
	buttons = append(buttons, viewRoomBtn)
	if _, err := bot.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	u.Button.Action = chooseOperations
	return api.TelegramMessage{
		Redirect:  u,
		Chattable: messages,
		Send:      true,
	}
}

type ViewBankDetails struct {
	bs  ButtonService
	css ChatStateService
	cfg *Config
}

func NewViewBankDetails(bs ButtonService, css ChatStateService, cfg *Config) *ViewBankDetails {
	return &ViewBankDetails{
		bs:  bs,
		cfg: cfg,
		css: css,
	}
}

func (bot ViewBankDetails) HasReact(u *api.Update) bool {
	return hasAction(u, bankDetailsView)
}

func (bot *ViewBankDetails) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	setBankBtn := api.NewButton(bankDetailsWantSet, &api.CallbackData{})
	backBtn := api.NewButton(userSetting, new(api.CallbackData))

	if _, err := bot.bs.SaveAll(ctx, setBankBtn, backBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	text := I18n(u.User, "scrn_bank_details_view", u.User.BankDetails)
	screen := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_edit_operation_edit"), setBankBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backBtn.ID.Hex())},
	})

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

type WantSetBankDetails struct {
	bs  ButtonService
	css ChatStateService
	cfg *Config
}

func NewWantSetBankDetails(bs ButtonService, css ChatStateService, cfg *Config) *WantSetBankDetails {
	return &WantSetBankDetails{
		bs:  bs,
		cfg: cfg,
		css: css,
	}
}

func (bot WantSetBankDetails) HasReact(u *api.Update) bool {
	return hasAction(u, bankDetailsWantSet)
}

func (bot *WantSetBankDetails) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	cs := &api.ChatState{UserId: int(getChatID(u)), Action: bankDetailsSet, CallbackData: &api.CallbackData{}}
	if u.Button.CallbackData != nil {
		cs.CallbackData.RoomId = u.Button.CallbackData.RoomId
		cs.CallbackData.ExternalData = u.Button.CallbackData.ExternalData
	}
	err := bot.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	backBtn := api.NewButton(userSetting, new(api.CallbackData))
	if _, err := bot.bs.SaveAll(ctx, backBtn); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	text := I18n(u.User, "scrn_bank_details_set")
	screen := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), backBtn.ID.Hex())},
	})

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

type SetBankDetails struct {
	bs  ButtonService
	css ChatStateService
	us  UserService
	cfg *Config
}

func NewSetBankDetails(bs ButtonService, us UserService, css ChatStateService, cfg *Config) *SetBankDetails {
	return &SetBankDetails{
		bs:  bs,
		us:  us,
		cfg: cfg,
		css: css,
	}
}

func (bot SetBankDetails) HasReact(u *api.Update) bool {
	return hasAction(u, bankDetailsSet) && hasMessage(u)
}

func (bot *SetBankDetails) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer bot.css.CleanChatState(ctx, u.ChatState)

	if err := bot.us.SetUserBankDetails(ctx, u.User.ID, u.Message.Text); err != nil {
		log.Error().Err(err).Msg("")
		return api.TelegramMessage{}
	}
	user, err := bot.us.FindById(ctx, u.User.ID)
	if err != nil {
		log.Error().Err(err).Msg("")
		return api.TelegramMessage{}
	}
	u.User = user
	u.Button = api.NewButton(bankDetailsView, nil)
	callbackData := *u.ChatState.CallbackData
	//Редирект на экран с комнатой
	if callbackData.ExternalData != "" && strings.Contains(callbackData.ExternalData, "room") {
		u.Message.Text = "/start " + string(viewRoom) + callbackData.RoomId
		u.Button = nil
	}
	u.ChatState = nil
	return api.TelegramMessage{
		Send:     true,
		Redirect: u,
	}
}
