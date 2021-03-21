package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"sort"
	"strconv"
	"strings"
	"time"
)

type OperationService interface {
	UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetAllDebtOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetAllSpendOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetUserSpendOperations(ctx context.Context, userId int, roomId string) (*[]api.Operation, error)
	GetAllDebts(ctx context.Context, roomId string) (*[]api.Debt, error)
	GetUserInvolvedDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error)
	GetUserDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error)
	GetUserDebt(ctx context.Context, debtorId int, lenderId int, roomId string) (*api.Debt, error)
}

// Operation show screen with my and all chooseOperations buttons
type Operation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewOperation(s ChatStateService, bs ButtonService, os OperationService, cfg *Config) *Operation {
	return &Operation{
		css: s,
		bs:  bs,
		os:  os,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (bot Operation) HasReact(u *api.Update) bool {
	if hasAction(u, chooseOperations) {
		return true
	}
	return false
}

// OnMessage returns one entry
func (bot Operation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId

	operations, err := bot.os.GetAllOperations(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("")
		return
	}
	if len(*operations) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_operations"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}
	data := &api.CallbackData{RoomId: roomId}

	viewUserOpsB := api.NewButton(viewUserOperations, data)
	viewAllOpsB := api.NewButton(viewAllOperations, data)
	backB := api.NewButton(viewRoom, data)

	if _, err := bot.bs.SaveAll(ctx, viewUserOpsB, viewAllOpsB, backB); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_user_opt"), viewUserOpsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_all_opt"), viewAllOpsB.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex())},
	}
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, I18n(u.User, "scrn_operations"), &keyboard)},
		Send:      true,
	}
}

func containsUserId(users *[]api.User, id int) bool {
	for _, u := range *users {
		if u.ID == id {
			return true
		}
	}
	return false
}

type WantDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	ts  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewWantDonorOperation(s ChatStateService, bs ButtonService, ts OperationService, rs RoomService, cfg *Config) *WantDonorOperation {
	return &WantDonorOperation{
		css: s,
		bs:  bs,
		ts:  ts,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s WantDonorOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == wantDonorOperation
}

// OnMessage returns one entry
func (s WantDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	if !containsUserId(room.Members, u.User.ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), I18n(u.User, "msg_not_be_in_rooms"))},
			Send:      true,
		}
	}

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: addDonorOperation, CallbackData: &api.CallbackData{RoomId: roomId}}
	err = s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	b := api.NewButton(viewRoom, u.Button.CallbackData)
	_, err = s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewEditMessage(getChatID(u), u.CallbackQuery.Message.ID,
			I18n(u.User, "scrn_add_operation"),
			[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), b.ID.Hex())}})},
		Send: true,
	}
}

type AddDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAddDonorOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, cfg *Config) *AddDonorOperation {
	return &AddDonorOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s AddDonorOperation) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
		return false
	}
	return u.ChatState.Action == addDonorOperation
}

// OnMessage returns one entry
func (s AddDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	sum, err := defineSum(u.Message.Text)
	purchaseText := s.defineText(u.Message.Text)

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId})
	if err != nil {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		if _, err := s.bs.SaveAll(ctx, rb); err != nil {
			log.Error().Err(err).Msg("save buttons failed")
			return
		}
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u),
				I18n(u.User, "msg_wrong_format")+I18n(u.User, "scrn_add_operation"),
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())}})},
			Send: true,
		}
	}
	defer s.css.CleanChatState(ctx, u.ChatState)

	room, err := s.rs.FindById(ctx, u.ChatState.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	operation := &api.Operation{
		ID:               primitive.NewObjectID(),
		Description:      purchaseText,
		Sum:              sum,
		Donor:            &u.Message.From,
		Recipients:       room.Members,
		CreateAt:         time.Now(),
		NotificationSent: []int{},
	}
	if err = s.os.UpsertOperation(ctx, operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	var buttons []*api.Button
	var tgButtons []tgbotapi.InlineKeyboardButton
	for _, v := range *room.Members {
		b := &api.Button{ID: primitive.NewObjectID(),
			Action:       donorOperation,
			Text:         setSmile(room.Members, v.ID) + v.DisplayName,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: v.ID, OperationId: operation.ID}}
		buttons = append(buttons, b)
		tgButtons = append(tgButtons, tgbotapi.NewInlineKeyboardButtonData(b.Text, b.ID.Hex()))
	}

	ob := api.NewButton(deleteDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	db := api.NewButton(addedOperation, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId, OperationId: operation.ID})
	buttons = append(buttons, rb, ob, db)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons,
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), db.ID.Hex())},
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_rm_operation"), ob.ID.Hex())})

	text := I18n(u.User, "scrn_operation_added", purchaseText, moneySpace(sum))
	text += "🗓 " + operation.CreateAt.Format("02 January 2006") + "\n\n"
	text += I18n(u.User, "scrn_mark_members")
	text += I18n(u.User, "scrn_take_part")
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u), text, keyboardButtons)},
		Send:      true,
	}
}

func splitKeyboardButtons(buttons []tgbotapi.InlineKeyboardButton, btnCountInLine int) [][]tgbotapi.InlineKeyboardButton {
	var keyboard [][]tgbotapi.InlineKeyboardButton
	var keyboardLine []tgbotapi.InlineKeyboardButton
	for i, v := range buttons {
		if len(keyboardLine) < btnCountInLine {
			keyboardLine = append(keyboardLine, v)
		}
		if len(keyboardLine) == btnCountInLine || i == len(buttons)-1 {
			keyboard = append(keyboard, keyboardLine)
			keyboardLine = nil
		}
	}
	return keyboard
}

func (s AddDonorOperation) defineText(text string) string {
	words := strings.Fields(text)
	return strings.Join(words[1:], " ")
}

func defineSum(text string) (int, error) {
	words := strings.Fields(text)
	sum, err := strconv.Atoi(words[0])
	if err != nil {
		log.Error().Err(err).Msg("text to int not parsed")
		return 0, err
	}
	if sum < 1 {
		log.Error().Err(err).Msgf("sum can not be les zero $v", sum)
		return 0, errors.New("sum can not be les zero")
	}
	return sum, nil
}

// Operation show screen with donar/recepient buttons
type DonorOperation struct {
	os  OperationService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewDonorOperation(bs ButtonService, os OperationService, rs RoomService, cfg *Config) *DonorOperation {
	return &DonorOperation{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s DonorOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == donorOperation
}

// OnMessage returns one entry
func (s DonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	var operation api.Operation
	for _, o := range *room.Operations {
		if u.Button.CallbackData.OperationId == o.ID {
			operation = o
		}
	}

	//if user not created operation we not mast show other buttons
	if operation.Donor.ID != getFrom(u).ID {
		cb := api.NewButton(viewAllOperations, u.Button.CallbackData)
		_, err = s.bs.Save(ctx, cb)
		if err != nil {
			log.Error().Err(err).Msg("create btn failed")
			return
		}
		text := I18n(u.User, "scrn_operation_on_sum", operation.Description, moneySpace(operation.Sum))
		text += "🗓 " + operation.CreateAt.Format("02 January 2006") + "\n\n"
		text += I18n(u.User, "scrn_user_paid", userLink(operation.Donor))
		for _, v := range *operation.Recipients {
			text += "- " + userLink(&v) + "\n"
		}
		msg := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), cb.ID.Hex())}})

		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{msg},
			Send:      true,
		}
	}

	*operation.Recipients = s.addOrDeleteRecipient(operation.Recipients, room.Members, u.Button.CallbackData.UserId)

	if len(*operation.Recipients) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_choose_one_members"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	if err = s.os.UpsertOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	var buttons []*api.Button
	var tgButtons []tgbotapi.InlineKeyboardButton
	for _, v := range *room.Members {
		b := &api.Button{ID: primitive.NewObjectID(),
			Action:       donorOperation,
			Text:         setSmile(operation.Recipients, v.ID) + v.DisplayName,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: v.ID, OperationId: operation.ID}}
		buttons = append(buttons, b)
		tgButtons = append(tgButtons, tgbotapi.NewInlineKeyboardButtonData(b.Text, b.ID.Hex()))
	}

	rb := api.NewButton(addedOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	ob := api.NewButton(deleteDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	buttons = append(buttons, rb, ob)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons,
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), rb.ID.Hex())},
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_rm_operation"), ob.ID.Hex())})

	text := I18n(u.User, "scrn_operation_on_sum", operation.Description, moneySpace(operation.Sum))
	text += "🗓 " + operation.CreateAt.Format("02 January 2006") + "\n\n"
	text += I18n(u.User, "scrn_mark_members")
	text += I18n(u.User, "scrn_take_part")
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboardButtons)},
		Send:      true,
	}
}

func (s DonorOperation) addOrDeleteRecipient(recipients *[]api.User, members *[]api.User, userId int) []api.User {
	if containsUserId(recipients, userId) {
		return deleteUser(*recipients, userId)
	} else {
		for _, m := range *members {
			if m.ID == userId {
				return append(*recipients, m)
			}
		}
	}
	return *recipients
}

func setSmile(users *[]api.User, id int) string {
	for _, u := range *users {
		if u.ID == id {
			return string(emoji.CheckMarkButton) + " "
		}
	}
	return string(emoji.CrossMark) + " "
}

func deleteUser(users []api.User, userId int) []api.User {
	var index int
	for i, v := range users {
		if v.ID == userId {
			index = i
		}
	}
	copy(users[index:], users[index+1:])
	return users[:len(users)-1]
}

// Operation show screen with donar/recepient buttons
type OperationAdded struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	os  OperationService
	us  UserService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewOperationAdded(s ChatStateService, bs ButtonService, rs RoomService, os OperationService, us UserService, cfg *Config) *OperationAdded {
	return &OperationAdded{
		css: s,
		bs:  bs,
		rs:  rs,
		os:  os,
		us:  us,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s OperationAdded) HasReact(u *api.Update) bool {
	return hasAction(u, addedOperation)
}

// OnMessage returns one entry
func (s OperationAdded) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	var opn api.Operation
	for _, o := range *room.Operations {
		if u.Button.CallbackData.OperationId == o.ID {
			opn = o
		}
	}

	var buttons []*api.Button
	var messages []tgbotapi.Chattable
	for _, user := range *opn.Recipients {
		user, err := s.us.FindById(ctx, user.ID)
		if err != nil {
			log.Error().Err(err).Msg("")
		}
		if !containsInt(opn.NotificationSent, user.ID) && user.NotificationOn && user.ID != u.User.ID {
			rb := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: opn.ID})
			backB := api.NewButton(viewStart, &api.CallbackData{})
			buttons = append(buttons, rb, backB)
			msg := NewMessage(int64(user.ID), I18n(user, "scrn_notification_operation_added", userLink(user), opn.Description, moneySpace(opn.Sum), room.Name),
				[][]tgbotapi.InlineKeyboardButton{
					{tgbotapi.NewInlineKeyboardButtonData(I18n(user, "btn_view_operation"), rb.ID.Hex())},
					{tgbotapi.NewInlineKeyboardButtonData(I18n(user, "btn_to_start"), backB.ID.Hex())},
				})
			opn.NotificationSent = append(opn.NotificationSent, user.ID)
			if err := s.os.UpsertOperation(ctx, &opn, room.ID.Hex()); err != nil {
				log.Error().Err(err).Msg("")
			}
			messages = append(messages, msg)
		}
	}

	viewRoomBtn := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.Button.CallbackData.RoomId})
	buttons = append(buttons, viewRoomBtn)
	if _, err := s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	u.Button.Action = viewRoom
	return api.TelegramMessage{
		Chattable: messages,
		Send:      true,
		Redirect:  u,
	}
}

// Operation show screen with donar/recepient buttons
type DeleteDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewDeleteDonorOperation(s ChatStateService, bs ButtonService, rs OperationService, cfg *Config) *DeleteDonorOperation {
	return &DeleteDonorOperation{
		css: s,
		bs:  bs,
		os:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s DeleteDonorOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == deleteDonorOperation
}

// OnMessage returns one entry
func (s DeleteDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if err := s.os.DeleteOperation(ctx, u.Button.CallbackData.RoomId, u.Button.CallbackData.OperationId); err != nil {
		log.Error().Err(err).Msg("")
		return
	}

	rb := &api.Button{ID: primitive.NewObjectID(), Action: viewAllOperations, CallbackData: &api.CallbackData{RoomId: u.Button.CallbackData.RoomId}}

	if _, err := s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u,
			I18n(u.User, "scrn_operation_deleted"),
			&[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), rb.ID.Hex())}})},
		Send: true,
	}
}

type WantRecepientOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewWantRecepientOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, cfg *Config) *WantRecepientOperation {
	return &WantRecepientOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s WantRecepientOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == wantRecipientOperation
}

// OnMessage returns one entry
func (s WantRecepientOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	userId := getFrom(u).ID
	debts, err := s.os.GetUserDebts(ctx, userId, room.ID.Hex())
	if err != nil {
		log.Error().Err(err).Msg("get user debts failed")
		return
	}
	if len(*debts) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_user_debts"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var buttons []*api.Button
	var tgButtons []tgbotapi.InlineKeyboardButton
	for _, v := range *debts {
		b := &api.Button{ID: primitive.NewObjectID(),
			Action:       chooseRecipient,
			Text:         emoji.Sprintf("%d ₽➡️%s", v.Sum, v.Lender.DisplayName),
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: v.Lender.ID}}
		buttons = append(buttons, b)
		tgButtons = append(tgButtons, tgbotapi.NewInlineKeyboardButtonData(b.Text, b.ID.Hex()))
	}

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	buttons = append(buttons, rb)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())})

	text := I18n(u.User, "scrn_choose_person")
	text += I18n(u.User, "scrn_send_message_choose_user")
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &keyboardButtons)},
		Send:      true,
	}
}

func (s WantRecepientOperation) defineRecipients(userId int, room *api.Room) map[int]api.User {
	donors := make(map[int]api.User)
	for _, o := range *room.Operations {
		for _, u := range *o.Recipients {
			if u.ID == userId && o.Donor.ID != userId {
				donors[userId] = *o.Donor
			}
		}
	}
	return donors

}

type ChooseRecepientOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewChooseRecepientOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, cfg *Config) *ChooseRecepientOperation {
	return &ChooseRecepientOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (s ChooseRecepientOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == chooseRecipient
}

// OnMessage returns one entry
func (s ChooseRecepientOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	lenderUserId := u.Button.CallbackData.UserId

	debt, err := s.os.GetUserDebt(ctx, u.User.ID, lenderUserId, roomId)
	if err != nil || debt == nil {
		log.Error().Err(err).Msg("get user debts failed")
		return
	}

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: addRecipientOperation, CallbackData: &api.CallbackData{RoomId: roomId, UserId: lenderUserId}}
	err = s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	b := api.NewButton(viewRoom, &api.CallbackData{RoomId: roomId})
	_, err = s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	text := I18n(u.User, "scrn_debt_repayment")
	text += I18n(u.User, "scrn_debt_returning_operation", userLink(debt.Lender), moneySpace(debt.Sum))
	text += I18n(u.User, "scrn_send_message_choose_user")
	msg := createScreen(u, text,
		&[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), b.ID.Hex())}})
	return api.TelegramMessage{Chattable: []tgbotapi.Chattable{msg},
		Send: true,
	}
}

type AddRecepientOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	us  UserService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAddRecepientOperation(s ChatStateService, bs ButtonService, os OperationService, us UserService, rs RoomService, cfg *Config) *AddRecepientOperation {
	return &AddRecepientOperation{
		css: s,
		bs:  bs,
		os:  os,
		us:  us,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s AddRecepientOperation) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
		return false
	}
	return u.ChatState.Action == addRecipientOperation
}

// OnMessage returns one entry
func (s AddRecepientOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.ChatState.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	lenderUserId := u.ChatState.CallbackData.UserId
	debt, err := s.os.GetUserDebt(ctx, u.User.ID, lenderUserId, room.ID.Hex())
	if err != nil || debt == nil {
		log.Error().Err(err).Msg("get user debts failed")
		return
	}

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId})
	if _, err = s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	sum, err := defineSum(u.Message.Text)
	if err != nil || sum > debt.Sum {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		text := I18n(u.User, "msg_wrong_format")
		text += I18n(u.User, "scrn_debt_returning_operation", userLink(debt.Lender), moneySpace(debt.Sum))
		text += I18n(u.User, "scrn_send_message_choose_user")
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u), text,
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())}})},
			Send: true,
		}
	}
	defer s.css.CleanChatState(ctx, u.ChatState)

	recipient, err := s.us.FindById(ctx, u.ChatState.CallbackData.UserId)
	if err != nil {
		log.Error().Err(err).Msgf("find user failed %v", u.ChatState.CallbackData.UserId)
		return
	}
	donor := getFrom(u)
	operation := &api.Operation{
		ID:              primitive.NewObjectID(),
		Sum:             sum,
		Donor:           donor,
		Recipients:      &[]api.User{*recipient},
		IsDebtRepayment: true,
		CreateAt:        time.Now(),
	}
	if err = s.os.UpsertOperation(ctx, operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	keyboard := [][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), rb.ID.Hex())}}
	forDonorMsg := createScreen(u, I18n(u.User, "scrn_debt_returned_lender", userLink(recipient), moneySpace(sum)), &keyboard)
	forRecipientMsg := NewMessage(int64(recipient.ID), I18n(u.User, "scrn_debt_returned_recepient", recipient.DisplayName, moneySpace(sum), userLink(donor)), keyboard)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{forDonorMsg, forRecipientMsg},
		Send:      true,
	}
}

// Operation show screen with donar/recepient buttons
type ViewAllOperations struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewAllOperations(s ChatStateService, bs ButtonService, rs OperationService, cfg *Config) *ViewAllOperations {
	return &ViewAllOperations{
		css: s,
		bs:  bs,
		os:  rs,
		cfg: cfg,
	}
}

func (bot ViewAllOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllOperations)
}

func (bot ViewAllOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := 5
	skip := page * size

	ops, err := bot.os.GetAllSpendOperations(ctx, roomId)
	if err != nil {
		return
	}

	var toSave []*api.Button
	var keyboard [][]tgbotapi.InlineKeyboardButton
	sort.SliceStable(*ops, func(i, j int) bool {
		return (*ops)[j].CreateAt.Before((*ops)[i].CreateAt)
	})
	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		opB := api.NewButton(donorOperation, &api.CallbackData{RoomId: roomId, Page: page, OperationId: op.ID})
		text := fmt.Sprintf("🛒%s %s₽ %s",
			stringForAlign(op.Description, 11, true),
			stringForAlign("💰"+moneySpace(op.Sum), 6, false),
			stringForAlign("👤"+shortName(op.Donor), 10, false))
		toSave = append(toSave, opB)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(text, opB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllOperations, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(chooseOperations, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewAllOperations, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_all_operations"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

// ViewMyOperations show screen with user chooseOperations
type ViewMyOperations struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewViewMyOperations(s ChatStateService, bs ButtonService, rs OperationService, cfg *Config) *ViewMyOperations {
	return &ViewMyOperations{
		css: s,
		bs:  bs,
		os:  rs,
		cfg: cfg,
	}
}

func (bot ViewMyOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewUserOperations)
}

func (bot ViewMyOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := 5
	skip := page * size

	ops, err := bot.os.GetUserSpendOperations(ctx, u.User.ID, roomId)
	if err != nil {
		return
	}
	if len(*ops) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_user_operations"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var toSave []*api.Button
	var keyboard [][]tgbotapi.InlineKeyboardButton
	sort.SliceStable(*ops, func(i, j int) bool {
		return (*ops)[j].CreateAt.Before((*ops)[i].CreateAt)
	})
	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		opB := api.NewButton(donorOperation, &api.CallbackData{RoomId: roomId, Page: page, OperationId: op.ID})
		text := fmt.Sprintf("🛒%s %s₽ %s",
			stringForAlign(op.Description, 11, true),
			stringForAlign("💰"+moneySpace(op.Sum), 6, false),
			stringForAlign("👤"+shortName(op.Donor), 10, false))
		toSave = append(toSave, opB)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(text, opB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewUserOperations, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(chooseOperations, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewUserOperations, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_my_operations"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
