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
	"strconv"
	"strings"
)

type OperationService interface {
	UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetAllDebts(ctx context.Context, roomId string) (*[]api.Debt, error)
	GetAllUsersDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error)
}

// Operation show screen with donar/recepient buttons
type Operation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewOperation(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *Operation {
	return &Operation{
		css: s,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s Operation) HasReact(u *api.Update) bool {
	if hasAction(u, viewStartOperation) {
		return true
	}
	if u.Message == nil || u.Message.Chat.Type != "private" {
		return false
	}
	return strings.Contains(u.Message.Text, startOperation)
}

// OnMessage returns one entry
func (s Operation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	var roomId string
	if isButton(u) {
		roomId = u.Button.CallbackData.RoomId
	} else {
		roomId = strings.ReplaceAll(u.Message.Text, "/start operation", "")
	}
	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	if !containsUserId(room.Members, getFrom(u).ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), "К сожалению, вы не находитесь в этой тусе")},
			Send:      true,
		}
	}

	recipientBtn := &api.Button{Action: wantRecipientOperation, CallbackData: &api.CallbackData{RoomId: roomId}}
	donorBtn := &api.Button{Action: wantDonorOperation, CallbackData: &api.CallbackData{RoomId: roomId}}
	viewRoomB := api.NewButton(viewRoom, &api.CallbackData{RoomId: roomId})
	_, err = s.bs.SaveAll(ctx, recipientBtn, donorBtn, viewRoomB)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, "Выбор операции для тусы *"+room.Name+"*",
			&[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Расход", donorBtn.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonData("Вернуть долг", recipientBtn.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonData("Отмена", viewRoomB.ID.Hex())}}),
		},
		Send: true,
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

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: addDonorOperation, CallbackData: &api.CallbackData{RoomId: roomId}}
	err := s.css.Save(ctx, cs)
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
			"Отлично. Теперь введите сумму и цель покупки через пробел и отправьте боту\n\nНапример:\n_1000 Расходы на бензин_",
			[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Отмена", b.ID.Hex())}})},
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
				string(emoji.Warning)+" Неверный формат данных. Введите сумму и цель покупки через пробел и отправьте боту\n\nНапример:\n_1000 Расходы на бензин_",
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Отмена", rb.ID.Hex())}})},
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
		ID:          primitive.NewObjectID(),
		Description: purchaseText,
		Sum:         sum,
		Donor:       &u.Message.From,
		Recipients:  room.Members,
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
	buttons = append(buttons, rb, ob)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons,
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())},
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(string(emoji.Wastebasket)+" Удалить операцию", ob.ID.Hex())})

	text := "Отлично. Операция *" + purchaseText + "* на сумму *" + strconv.Itoa(sum) + "* добавлена.\n\n"
	text += "Теперь выбери участников, которые участвуют в расходе, нажми *Готово* если все участники участвуют в расходе"
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
	if operation.Donor.ID != getFrom(u).ID || !isPrivate(u) {
		cb := api.NewButton(viewAllOperations, u.Button.CallbackData)
		_, err = s.bs.Save(ctx, cb)
		if err != nil {
			log.Error().Err(err).Msg("create btn failed")
			return
		}
		text := "Операция _" + operation.Description + "_ на сумму *" + strconv.Itoa(operation.Sum) + "*.\n\n" +
			"Заплатил: " + fmt.Sprintf("[%s](tg://user?id=%d)\n", operation.Donor.DisplayName, operation.Donor.ID) + "\nУчастники:\n"
		for _, v := range *operation.Recipients {
			text += fmt.Sprintf("- [%s](tg://user?id=%d)\n", v.DisplayName, v.ID)
		}
		msg := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Готово", cb.ID.Hex())}})
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{msg},
			Send:      true,
		}
	}

	*operation.Recipients = s.addOrDeleteRecipient(operation.Recipients, room.Members, u.Button.CallbackData.UserId)

	if len(*operation.Recipients) < 1 {
		callback := createCallback(u, string(emoji.Warning)+"Выберите хотя бы одного человека", true)
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

	rb := &api.Button{ID: primitive.NewObjectID(), Action: viewRoom, CallbackData: &api.CallbackData{RoomId: room.ID.Hex()}}
	ob := &api.Button{ID: primitive.NewObjectID(), Action: deleteDonorOperation, CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID}}
	buttons = append(buttons, rb, ob)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons,
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())},
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(string(emoji.Wastebasket)+" Удалить операцию", ob.ID.Hex())})

	text := "Операция *" + operation.Description + "* на сумму *" + strconv.Itoa(operation.Sum) + "*.\n\n"
	text += "Выбери участников, которые участвуют в расходе, нажми *Готово* если все участники участвуют в расходе"
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

	rb := &api.Button{ID: primitive.NewObjectID(), Action: viewRoom, CallbackData: &api.CallbackData{RoomId: u.Button.CallbackData.RoomId}}

	if _, err := s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u,
			"Отлично. Операция успешно удалена",
			&[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())}})},
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
	donors := s.defineRecipients(userId, room)
	if len(donors) < 1 {
		callback := createCallback(u, string(emoji.Warning)+"У вас отсутствуют долги", true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var buttons []*api.Button
	var tgButtons []tgbotapi.InlineKeyboardButton
	for _, v := range donors {
		b := &api.Button{ID: primitive.NewObjectID(),
			Action:       chooseRecipient,
			Text:         v.DisplayName,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: v.ID}}
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
	keyboardButtons = append(keyboardButtons, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Отмена", rb.ID.Hex())})

	text := "Нажмите на кнопку с именем человека, которому вы хотите вернуть долг.\n\n"
	text += "_P.S. Выбранному человеку придет уведомления от бота_"
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
	ts  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewChooseRecepientOperation(s ChatStateService, bs ButtonService, ts OperationService, rs RoomService, cfg *Config) *ChooseRecepientOperation {
	return &ChooseRecepientOperation{
		css: s,
		bs:  bs,
		ts:  ts,
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
	donorUserId := u.Button.CallbackData.UserId

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: addRecipientOperation, CallbackData: &api.CallbackData{RoomId: roomId, UserId: donorUserId}}
	err := s.css.Save(ctx, cs)
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
	msg := createScreen(u, "Отлично. Теперь введите сумму которую вы вернули выбранному человеку и отправьте боту\n\nНапример: _1000_",
		&[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Отмена", b.ID.Hex())}})
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

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId})
	if _, err = s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	sum, err := defineSum(u.Message.Text)
	if err != nil {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u),
				string(emoji.Warning)+" Неверный формат данных.\nВведите сумму которую вы вернули выбранному человеку и отправьте боту\n\nНапример:\n_1000_",
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Отмена", rb.ID.Hex())}})},
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
	}
	if err = s.os.UpsertOperation(ctx, operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	keyboard := [][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())}}
	forDonorMsg := createScreen(u, "Отлично. Долг для "+fmt.Sprintf("[%s](tg://user?id=%d)\n", recipient.DisplayName, recipient.ID)+" на сумму *"+strconv.Itoa(sum)+"* возвращен.\n\n", &keyboard)
	forRecipientMsg := NewMessage(int64(recipient.ID), recipient.DisplayName+"\nВам был возвращен долг на сумму *"+strconv.Itoa(sum)+"* от "+fmt.Sprintf("[%s](tg://user?id=%d)\n", donor.DisplayName, donor.ID)+"", keyboard)

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

	ops, err := bot.os.GetAllOperations(ctx, roomId)
	if err != nil {
		return
	}

	var toSave []*api.Button
	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i := skip; i < skip+size && i < len(*ops); i++ {
		op := (*ops)[i]
		opB := api.NewButton(donorOperation, &api.CallbackData{RoomId: roomId, Page: page, OperationId: op.ID})
		toSave = append(toSave, opB)
		text := fmt.Sprintf("%s %s₽ %s",
			stringForAlign(op.Description, 16, true),
			stringForAlign(thousandSpace(op.Sum), 6, false),
			stringForAlign("["+shortName(op.Donor)+"]", 13, false))
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(text, opB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewAllOperations, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewAllOperations, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	if len(navRow) != 0 {
		keyboard = append(keyboard, navRow)
	}

	backB := api.NewButton(viewRoom, u.Button.CallbackData)
	toSave = append(toSave, backB)
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Назад", backB.ID.Hex()),
	})

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	screen := createScreen(u, "Операции тусы", &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}
