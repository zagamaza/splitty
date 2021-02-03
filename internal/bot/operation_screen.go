package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"strconv"
	"strings"
)

type OperationService interface {
	UpsertOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, operationId primitive.ObjectID) error
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
	if u.Message == nil || u.Message.Chat.Type != "private" {
		return false
	}
	return strings.Contains(u.Message.Text, startOperation)
}

// OnMessage returns one entry
func (s Operation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	roomId := strings.ReplaceAll(u.Message.Text, "/start operation", "")
	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	if !containsUserId(room.Members, getFrom(u).ID) {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{tgbotapi.NewMessage(getChatID(u), "К сожалению, вы не находитесь в этой комнате")},
			Send:      true,
		}
	}

	recipientBtn := &api.Button{Action: recipient, CallbackData: &api.CallbackData{RoomId: roomId}}
	donorBtn := &api.Button{Action: donor, CallbackData: &api.CallbackData{RoomId: roomId}}
	_, err = s.bs.SaveAll(ctx, recipientBtn, donorBtn)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u), "Выбор операции для комнаты *"+room.Name+"*",
			[][]tgbotapi.InlineKeyboardButton{
				{tgbotapi.NewInlineKeyboardButtonData("Расход", donorBtn.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonData("Приход", recipientBtn.ID.Hex())},
				{tgbotapi.NewInlineKeyboardButtonData("❔ Помощь", "http://t.me/"+s.cfg.BotName+"?start=")}}),
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
	return u.Button.Action == donor
}

// OnMessage returns one entry
func (s WantDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId

	cs := &api.ChatState{UserId: int(getChatID(u)), Action: addDonorOperation, ExternalId: roomId}
	err := s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	b := &api.Button{ID: primitive.NewObjectID(), Action: cancel}
	_, err = s.bs.Save(ctx, b)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u),
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
	defer func() {
		err := s.css.DeleteById(ctx, u.ChatState.ID)
		if err != nil {
			log.Error().Err(err).Msg("")
		}
	}()

	purchaseText := s.defineText(u.Message.Text)
	sum, err := s.defineSum(u.Message.Text)
	if err != nil {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
	}

	room, err := s.rs.FindById(ctx, u.ChatState.ExternalId)
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

	rb := &api.Button{ID: primitive.NewObjectID(), Action: viewRoom, CallbackData: &api.CallbackData{RoomId: room.ID.Hex()}}
	buttons = append(buttons, rb)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	keyboardButtons := splitKeyboardButtons(tgButtons, 2)
	keyboardButtons = append(keyboardButtons, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())})

	text := "Отлично. Операция _" + purchaseText + "_ на сумму *" + strconv.Itoa(sum) + "* добавлена.\n\n"
	text += "Теперь выбери участников, которые участвуют в расходе"
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

func (s AddDonorOperation) defineSum(text string) (int, error) {
	words := strings.Fields(text)
	sum, err := strconv.Atoi(words[0])
	if err != nil {
		log.Error().Err(err).Msg("text to int not parsed")
		return 0, err
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

	cb := &api.Button{ID: primitive.NewObjectID(), Action: cancel}
	_, err = s.bs.Save(ctx, cb)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	//if user not created operation we not mast show other buttons
	if operation.Donor.ID != getFrom(u).ID {
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{
				NewEditMessage(getChatID(u), u.CallbackQuery.Message.ID,
					"Операция _"+operation.Description+"_ на сумму *"+strconv.Itoa(operation.Sum)+"*.\n\n",
					[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Готово", cb.ID.Hex())}})},
			Send: true,
		}
	}

	*operation.Recipients = s.addOrDeleteRecipient(operation.Recipients, room.Members, u.Button.CallbackData.UserId)

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
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Удалить операцию", ob.ID.Hex())})

	text := "Операция _" + operation.Description + "_ на сумму *" + strconv.Itoa(operation.Sum) + "*.\n\n"
	text += "Выбери участников, которые участвуют в расходе"
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewEditMessage(getChatID(u), u.CallbackQuery.Message.ID, text, keyboardButtons)},
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
			return "+ "
		}
	}
	return "- "
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
	if err := s.os.DeleteOperation(ctx, u.Button.CallbackData.OperationId); err != nil {
		log.Error().Err(err).Msg("")
		return
	}

	rb := &api.Button{ID: primitive.NewObjectID(), Action: viewRoom, CallbackData: &api.CallbackData{RoomId: u.Button.CallbackData.RoomId}}

	if _, err := s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Msg("save buttons failed")
		return
	}

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{NewEditMessage(getChatID(u), u.CallbackQuery.Message.ID,
			"Отлично. Операция успешно удалена",
			[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData("Готово", rb.ID.Hex())}})},
		Send: true,
	}
}
