package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/sdk"
	"github.com/enescakir/emoji"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type OperationService interface {
	UpdateOperation(ctx context.Context, o *api.Operation, roomId string) error
	CreateOperation(ctx context.Context, o *api.Operation, roomId string) error
	DeleteOperation(ctx context.Context, roomId string, operationId primitive.ObjectID) error
	GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetAllDebtOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetAllSpendOperations(ctx context.Context, roomId string) (*[]api.Operation, error)
	GetUserSpendOperations(ctx context.Context, userId int, roomId string) (*[]api.Operation, error)
	GetUserParticipateInOperations(ctx context.Context, userId int, roomId string) (*[]api.Operation, error)
	GetAllDebts(ctx context.Context, roomId string) ([]api.Debt, error)
	GetUserInvolvedDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error)
	GetUserDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error)
	GetUserDebt(ctx context.Context, debtorId int, lenderId int, roomId string) (*api.Debt, error)
}

// Operation show screen with my and all chooseOperations buttons
type Operation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

func NewOperation(s ChatStateService, bs ButtonService, rs RoomService, cfg *Config) *Operation {
	return &Operation{
		css: s,
		bs:  bs,
		rs:  rs,
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
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("cannot find room, id:%s", roomId)
		return
	}
	operations := room.Operations
	if len(*operations) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_operations"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}
	data := &api.CallbackData{RoomId: roomId}

	var toSave []*api.Button
	viewUserOpsB := api.NewButton(viewUserOperations, data)
	viewAllOpsB := api.NewButton(viewAllOperations, data)
	viewWithMeOpsB := api.NewButton(viewOperationsWithMe, data)
	toSave = append(toSave, viewUserOpsB, viewAllOpsB, viewWithMeOpsB)

	var buttons []tgbotapi.InlineKeyboardButton
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_user_opt"), viewUserOpsB.ID.Hex()))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_opt_with_me"), viewWithMeOpsB.ID.Hex()))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_all_opt"), viewAllOpsB.ID.Hex()))

	if !slices.Contains(room.RoomStates.FinishedAddOperation, u.User.ID) {
		finishedAddOperationBtn := api.NewButton(finishedAddOperation, &api.CallbackData{RoomId: roomId, ExternalData: "true"})
		toSave = append(toSave, finishedAddOperationBtn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_finished_add_operation"), finishedAddOperationBtn.ID.Hex()))
	} else {
		notFinishedAddOperationBtn := api.NewButton(finishedAddOperation, &api.CallbackData{RoomId: roomId, ExternalData: "false"})
		toSave = append(toSave, notFinishedAddOperationBtn)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_not_finished_add_operation"), notFinishedAddOperationBtn.ID.Hex()))
	}

	backB := api.NewButton(viewRoom, data)
	toSave = append(toSave, backB)
	buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	keyboard := splitKeyboardButtons(buttons, 1)
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

func containsRecipient(users []api.RecipientWithSum, id int) bool {
	for _, u := range users {
		if u.User.ID == id {
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

	//validation, if all members finished added operation
	if len(room.RoomStates.FinishedAddOperation) == len(*room.Members) {
		callback := createCallback(u, I18n(u.User, "msg_can_not_add_operations"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
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

// AddDonorOperation screen with added operation
type AddSplitTypeDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	rss RoomStateService
	cfg *Config
}

func NewSplitTypeDonorOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, rss RoomStateService, cfg *Config) *AddSplitTypeDonorOperation {
	return &AddSplitTypeDonorOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		rss: rss,
		cfg: cfg,
	}
}

func (s AddSplitTypeDonorOperation) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
		return false
	}
	return u.ChatState.Action == addDonorOperation
}

// OnMessage returns one entry
func (s AddSplitTypeDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	sum, err := defineSum(u.Message.Text)
	purchaseText := s.defineText(u.Message.Text)

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId})
	if err != nil || purchaseText == "" {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		if _, err := s.bs.SaveAll(ctx, rb); err != nil {
			log.Error().Err(err).Stack().Msg("save buttons failed")
			return
		}
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u),
				I18n(u.User, "msg_wrong_format")+I18n(u.User, "scrn_add_operation"),
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())}})},
			Send: true,
		}
	}
	s.css.CleanChatState(ctx, u.ChatState)
	u.ChatState.CallbackData.ExternalData = purchaseText
	u.ChatState.CallbackData.Page = sum
	err = s.css.Save(ctx, &api.ChatState{UserId: int(getChatID(u)), Action: addedOperation, CallbackData: u.ChatState.CallbackData})
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	var buttons []*api.Button
	equallyBtn := api.NewButton(chooseSplitTypeOperation, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId, ExternalData: string(equally)})
	notEquallyBtn := api.NewButton(chooseSplitTypeOperation, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId, ExternalData: string(by_exact_amount)})
	buttons = append(buttons, rb, equallyBtn, notEquallyBtn)

	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	tgButtons := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("🟰 Поровну", equallyBtn.ID.Hex()),
			tgbotapi.NewInlineKeyboardButtonData("✍️ Указать вручную", notEquallyBtn.ID.Hex()),
		},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())},
	}

	text := `
Как вы хотите распределить расход?

1. Поровну между участниками.
2. Указать вручную для каждого участника.
`

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{
			NewMessage(getChatID(u), text, tgButtons),
		},
		Send: true,
	}
}

func (s AddSplitTypeDonorOperation) defineText(text string) string {
	words := strings.Fields(text)
	return strings.Join(words[1:], " ")
}

// AddDonorOperation screen with added operation
type AddDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	rss RoomStateService
	cfg *Config
}

func NewAddDonorOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, rss RoomStateService, cfg *Config) *AddDonorOperation {
	return &AddDonorOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		rss: rss,
		cfg: cfg,
	}
}

func (s AddDonorOperation) HasReact(u *api.Update) bool {
	if u.ChatState == nil {
		return false
	}
	return hasAction(u, addedOperation) && hasAction(u, chooseSplitTypeOperation)
}

// OnMessage returns one entry
func (s AddDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	room, err := s.rs.FindById(ctx, u.ChatState.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	recipientsWithSum := make([]api.RecipientWithSum, 0)
	var splitType api.SplitType
	if u.Button.CallbackData.ExternalData == string(equally) {
		splitType = equally
		for _, user := range *room.Members {
			recipientsWithSum = append(recipientsWithSum, api.RecipientWithSum{User: user, Sum: float64(u.ChatState.CallbackData.Page) / float64(len(*room.Members))})
		}
	} else {
		splitType = by_exact_amount
	}

	operation := &api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       u.ChatState.CallbackData.ExternalData,
		Sum:               u.ChatState.CallbackData.Page,
		Donor:             u.User,
		RecipientsWithSum: recipientsWithSum,
		CreateAt:          time.Now(),
		NotificationSent:  []int{},
		Status:            draft,
		SplitType:         splitType,
		Files:             []api.File{},
	}

	if err = s.os.CreateOperation(ctx, operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	//async calculate paidOfDebtsUserIds for room, after added operation
	go func() {
		err := s.rss.DefinePaidOfDebtsUserIdsAndSave(ctx, room)
		if err != nil {
			log.Error().Err(err).Msg("calculate paidOfDebtsUserIds failed")
		}
	}()

	screen, _ := showOperation(ctx, u, *room, *operation, err, s.bs)
	s.css.CleanChatState(ctx, u.ChatState)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
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
	if sum < 0 {
		log.Error().Err(err).Msgf("sum can not be les zero %v", sum)
		return 0, errors.New("sum can not be les zero")
	}
	return sum, nil
}

// Operation show screen with donar/recepient buttons
type EditDonorOperation struct {
	os  OperationService
	bs  ButtonService
	css ChatStateService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewEditDonorOperation(bs ButtonService, css ChatStateService, os OperationService, rs RoomService, cfg *Config) *EditDonorOperation {
	return &EditDonorOperation{
		os:  os,
		css: css,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s EditDonorOperation) HasReact(u *api.Update) bool {
	return hasButtonAction(u, chooseDonorOperation) ||
		hasButtonAction(u, editDonorOperation) ||
		hasButtonAction(u, addingOperation) ||
		(u.Button != nil && u.Button.Action == saveSumDonorOperation) ||
		(u.Button != nil && hasAction(u, addDonorOperation))
}

// OnMessage returns one entry
func (s EditDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	// Получаем идентификаторы пользователя, операции и данные комнаты
	UserId, OperationId, room, err, done := getStateIdentifires(ctx, s.rs, u)
	if done {
		return
	}

	// Если все участники уже завершили добавление операций – редактирование недоступно
	countUsersFinishedAddOperation := len(room.RoomStates.FinishedAddOperation)
	if len(*room.Members) == countUsersFinishedAddOperation {
		callback := createCallback(u, I18n(u.User, "msg_not_editable_all_operations_added"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	operation := findOperationByID(room, OperationId)
	if hasAction(u, editDonorOperation) && operation.OldOperationId == nil {
		oldId := operation.ID
		newOp := operation // копирование по значению (shallow copy)
		newOp.OldOperationId = &oldId
		newOp.ID = primitive.NewObjectID() // генерируем новый идентификатор для черновой записи
		newOp.Status = draft               // устанавливаем статус черновика
		newOp.CreateAt = time.Now()        // обновляем дату создания для новой записи

		// Сохраняем новую операцию (черновик) в БД
		if err := s.os.CreateOperation(ctx, &newOp, room.ID.Hex()); err != nil {
			log.Error().Err(err).Stack().Msg("save buttons failed")
			return
		}
		operation.Status = archive
		if err := s.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
			log.Error().Err(err).Stack().Msg("save buttons failed")
			return
		}
		operation = newOp
		OperationId = newOp.ID
	}

	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	if _, err = s.bs.SaveAll(ctx, rb); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	if hasButtonAction(u, chooseDonorOperation) {
		operation.RecipientsWithSum = s.addOrDeleteRecipient(operation, room, UserId, operation.Sum)
	} else if hasAction(u, saveSumDonorOperation) {
		sum := u.ChatState.CallbackData.Page
		for i := range operation.RecipientsWithSum {
			if operation.RecipientsWithSum[i].User.ID == UserId {
				operation.RecipientsWithSum[i].Sum = float64(sum)
			}
		}
	}

	if err = s.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}
	s.css.CleanChatState(ctx, u.ChatState)

	screen, done := showOperation(ctx, u, *room, operation, err, s.bs)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

func showOperation(ctx context.Context, u *api.Update, room api.Room, operation api.Operation, err error, s ButtonService) (tgbotapi.Chattable, bool) {
	var buttons []*api.Button
	var tgButtons [][]tgbotapi.InlineKeyboardButton
	if operation.SplitType == equally {
		var button *api.Button
		switch len(operation.RecipientsWithSum) {
		case 0:
			button = api.NewButton(
				enableAllDonor,
				&api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
			button.Text = "👥 Выбрать всех"
		default:
			button = api.NewButton(
				disableAllDonor,
				&api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
			button.Text = "🚫 Убрать всех"
		}

		buttons = append(buttons, button)
		tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(button.Text, button.ID.Hex()),
		})
	}

	for _, member := range *room.Members {
		userTitleBtn := &api.Button{
			ID:           primitive.NewObjectID(),
			Action:       chooseDonorOperation,
			Text:         setSmileRecipient(operation.RecipientsWithSum, member.ID) + member.DisplayName,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: member.ID, OperationId: operation.ID},
		}
		buttons = append(buttons, userTitleBtn)

		if operation.SplitType == by_exact_amount {
			var recipientWithSum api.RecipientWithSum
			for _, r := range operation.RecipientsWithSum {
				if r.User.ID == member.ID {
					recipientWithSum = r
					break
				}
			}
			priceBtn := fmt.Sprintf("💵 %s", moneySpace(int(recipientWithSum.Sum), room.Currency))

			setSumBtn := &api.Button{
				ID:           primitive.NewObjectID(),
				Action:       editSumDonorOperation,
				Text:         priceBtn,
				CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: member.ID, OperationId: operation.ID},
			}
			buttons = append(buttons, setSumBtn)

			tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(userTitleBtn.Text, userTitleBtn.ID.Hex()),
				tgbotapi.NewInlineKeyboardButtonData(setSumBtn.Text, setSumBtn.ID.Hex()),
			})
		} else {
			tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(userTitleBtn.Text, userTitleBtn.ID.Hex()),
			})
		}
	}

	doneBtn := api.NewButton(addedOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	changePayerOperationBtn := api.NewButton(changePayerOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	deleteBtn := api.NewButton(deleteDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	addFileBtn := api.NewButton(wantAddFileToOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	unsupportedBtn := api.NewButton(unsupported, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	addingOperationBtn := api.NewButton(addingOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID, Expand: true})
	buttons = append(buttons, doneBtn, deleteBtn, addFileBtn, changePayerOperationBtn, addingOperationBtn, unsupportedBtn)

	fileBtn := I18n(u.User, "btn_add_file")
	if len(operation.Files) > 0 {
		fileBtn = I18n(u.User, "btn_edit_file")
	}
	if u.Button.CallbackData.Expand {
		tgButtons = append(tgButtons,
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_choose_payer"), changePayerOperationBtn.ID.Hex())},
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("💵 Изменить цену операции", unsupportedBtn.ID.Hex())},
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("📝 Изменить текст операции", unsupportedBtn.ID.Hex())},
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(fileBtn, addFileBtn.ID.Hex())})
	} else {
		tgButtons = append(tgButtons,
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_expand"), addingOperationBtn.ID.Hex())})
	}

	tgButtons = append(tgButtons,
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_rm_operation"), deleteBtn.ID.Hex())},
		[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_save"), doneBtn.ID.Hex())})

	if _, err = s.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return nil, true
	}

	unallocatedSum := float64(operation.Sum)
	for _, recipientWithSum := range operation.RecipientsWithSum {
		unallocatedSum -= recipientWithSum.Sum
	}

	slivce := []string{
		" " + moneySpace(operation.Sum, room.Currency),
		" " + operation.Description,
		" ",
		"  " + I18n(u.User, "scrn_payer", operation.Donor.DisplayName),
		""}
	tb := NewTableBuilder('-', " | ")
	tb.AddColumnSimple(Center, func(i int) string {
		s2 := slivce[i]
		return s2
	})
	tb.WithFrame()
	result := tb.Build()

	text := " " + operation.CreateAt.Format("02 January 2006")
	text += "\n\n%s\n%s"
	text = fmt.Sprintf(
		text,
		result,
		func() string {
			if operation.SplitType == equally {
				return ""
			}
			if RoundToTwoDecimalPlaces(unallocatedSum) > 0 {
				return fmt.Sprintf("⚠️ Осталось распределить %s", moneySpace(int(unallocatedSum), room.Currency))
			} else if unallocatedSum < 0 {
				return fmt.Sprintf("🔴 Избыток: %s", moneySpace(int(-unallocatedSum), room.Currency))
			} else {
				return "Все средства распределены 💪"
			}
		}())

	screen := createScreen(u, text, &tgButtons)
	return screen, false
}

func getStateIdentifires(ctx context.Context, rs RoomService, u *api.Update) (int, primitive.ObjectID, *api.Room, error, bool) {
	var RoomId string
	var UserId int
	var OperationId primitive.ObjectID

	if u.Button != nil {
		RoomId = u.Button.CallbackData.RoomId
		UserId = u.Button.CallbackData.UserId
		OperationId = u.Button.CallbackData.OperationId

	} else if u.ChatState != nil {
		RoomId = u.ChatState.CallbackData.RoomId
		UserId = u.ChatState.CallbackData.UserId
		OperationId = u.ChatState.CallbackData.OperationId
	}
	room, err := rs.FindById(ctx, RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return 0, primitive.ObjectID{}, nil, nil, true
	}
	return UserId, OperationId, room, err, false
}

func (s EditDonorOperation) addOrDeleteRecipient(
	operation api.Operation,
	room *api.Room,
	userId int,
	totalSum int,
) []api.RecipientWithSum {
	members := room.Members
	recipients := operation.RecipientsWithSum

	if containsRecipient(recipients, userId) {
		// Удаляем пользователя
		recipients = deleteUser(recipients, userId)
	} else {
		// Ищем пользователя по ID и добавляем
		for _, m := range *members {
			if m.ID == userId {
				recipients = append(recipients, api.RecipientWithSum{User: m})
				break
			}
		}
	}

	// Пересчитываем долю для каждого участника:
	if len(recipients) > 0 && operation.SplitType == equally {
		share := float64(totalSum) / float64(len(recipients))
		for i := range recipients {
			recipients[i].Sum = share
		}
	}

	return recipients
}

func setSmileRecipient(recipients []api.RecipientWithSum, id int) string {
	for _, u := range recipients {
		if id == u.User.ID {
			return "🟢 "
		}
	}
	return "🔘 "
}

func deleteUser(users []api.RecipientWithSum, userId int) []api.RecipientWithSum {
	var index int
	for i, v := range users {
		if v.User.ID == userId {
			index = i
		}
	}
	copy(users[index:], users[index+1:])
	return users[:len(users)-1]
}

type EditDonorAmountHandler struct {
	os  OperationService
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

func NewEditDonorAmountHandler(bs ButtonService, os OperationService, css ChatStateService, rs RoomService, cfg *Config) *EditDonorAmountHandler {
	return &EditDonorAmountHandler{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (h EditDonorAmountHandler) HasReact(u *api.Update) bool {
	return hasAction(u, editSumDonorOperation)
}

// OnMessage processes the update and prepares the response message
func (h EditDonorAmountHandler) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	_, operationId, room, err, done := getStateIdentifires(ctx, h.rs, u)
	if done {
		return
	}

	operation := findOperationByID(room, operationId)

	var recipient api.User
	for _, r := range operation.RecipientsWithSum {
		if r.User.ID == u.Button.CallbackData.UserId {
			recipient = r.User
			break
		}
	}
	if recipient.ID == 0 {
		for _, member := range *room.Members {
			if member.ID == u.Button.CallbackData.UserId {
				operation.RecipientsWithSum = append(operation.RecipientsWithSum, api.RecipientWithSum{User: member})
				recipient = member
				break
			}
		}
	}
	if err = h.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	h.css.CleanChatState(ctx, u.ChatState)
	cs := &api.ChatState{UserId: int(getChatID(u)),
		CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: recipient.ID, OperationId: operation.ID},
		Action:       setSumDonorOperation}

	err = h.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	cancelBtn := api.NewButton(addDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	if _, err = h.bs.Save(ctx, cancelBtn); err != nil {
		log.Error().Err(err).Msg("save button failed")
		return
	}
	tgButtons := [][]tgbotapi.InlineKeyboardButton{{
		tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), cancelBtn.ID.Hex()),
	}}
	unallocatedSum := float64(operation.Sum)
	for _, recipientWithSum := range operation.RecipientsWithSum {
		unallocatedSum -= recipientWithSum.Sum
	}

	tb := sdk.NewTableBuilder('-', " | ")
	tb.AddColumn(sdk.Left, sdk.Monospaced, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			if operation.RecipientsWithSum[i].User.ID == recipient.ID {
				return "-> " + operation.RecipientsWithSum[i].User.DisplayName
			}
			return operation.RecipientsWithSum[i].User.DisplayName
		} else if i == len(operation.RecipientsWithSum) {
			return "Итого"
		}
		return ""
	})
	tb.AddColumn(sdk.Right, sdk.NumberWithTinySpaces, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			return moneySpace(int(operation.RecipientsWithSum[i].Sum), room.Currency)
		} else if i == len(operation.RecipientsWithSum) {
			total := 0
			for _, recipient := range operation.RecipientsWithSum {
				total += int(recipient.Sum)
			}
			return moneySpace(total, room.Currency)
		}
		return ""
	})
	tb.AddSeparatorRow(0)
	tb.AddSeparatorRow(len(operation.RecipientsWithSum))
	text := fmt.Sprintf(`
🔷 Введите сколько должен: [%s]

💼 Операция: %s
💵 Общая сумма: %s

📊 Распределение:
%s`,
		recipient.DisplayName,
		operation.Description,
		moneySpace(operation.Sum, room.Currency),
		tb.Build(),
	)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &tgButtons)},
		Send:      true,
	}
}

type ChangePayerHandler struct {
	os  OperationService
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

func NewChangePayerHandler(bs ButtonService, os OperationService, css ChatStateService, rs RoomService, cfg *Config) *ChangePayerHandler {
	return &ChangePayerHandler{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (h ChangePayerHandler) HasReact(u *api.Update) bool {
	return hasAction(u, changePayerOperation)
}

// OnMessage processes the update and prepares the response message
func (h ChangePayerHandler) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := h.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	operation := findOperationByID(room, u.Button.CallbackData.OperationId)

	var buttons []*api.Button
	var tgButtons [][]tgbotapi.InlineKeyboardButton
	for _, member := range *room.Members {
		text := member.DisplayName
		if member.ID == operation.Donor.ID {
			text = "💳 " + text
		}
		userTitleBtn := &api.Button{
			ID:           primitive.NewObjectID(),
			Action:       choosePayerOperation,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), UserId: member.ID, OperationId: operation.ID},
		}
		buttons = append(buttons, userTitleBtn)
		tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(text, userTitleBtn.ID.Hex()),
		})
	}

	deleteBtn := api.NewButton(deleteDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	addingOperationBtn := api.NewButton(addingOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: operation.ID})
	buttons = append(buttons, deleteBtn, addingOperationBtn)
	if _, err = h.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Msg("save button failed")
		return
	}
	tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔙 "+I18n(u.User, "btn_cancel"), addingOperationBtn.ID.Hex()),
	})
	tgButtons = append(tgButtons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_rm_operation"), deleteBtn.ID.Hex()),
	})

	text := I18n(u.User, "scrn_choose_payer", operation.Donor.DisplayName)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{createScreen(u, text, &tgButtons)},
		Send:      true,
	}
}

type ChangedPayerHandler struct {
	os  OperationService
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

func NewChangedPayerHandler(bs ButtonService, os OperationService, css ChatStateService, rs RoomService, cfg *Config) *ChangedPayerHandler {
	return &ChangedPayerHandler{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (h ChangedPayerHandler) HasReact(u *api.Update) bool {
	return hasAction(u, choosePayerOperation)
}

// OnMessage processes the update and prepares the response message
func (h ChangedPayerHandler) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := h.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	var payer api.User
	for i, m := range *room.Members {
		if m.ID == u.Button.CallbackData.UserId {
			payer = (*room.Members)[i]
			break
		}
	}
	operation := findOperationByID(room, u.Button.CallbackData.OperationId)
	operation.Donor = &payer
	if err = h.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}
	u.Button.Action = addingOperation
	return api.TelegramMessage{
		Send:     true,
		Redirect: u,
	}
}

type DisableEnableAllDonorHandler struct {
	os  OperationService
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

func NewDisableEnableAllDonorHandler(bs ButtonService, os OperationService, css ChatStateService, rs RoomService, cfg *Config) *DisableEnableAllDonorHandler {
	return &DisableEnableAllDonorHandler{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
		css: css,
	}
}

func (h DisableEnableAllDonorHandler) HasReact(u *api.Update) bool {
	return hasAction(u, disableAllDonor) || hasAction(u, enableAllDonor)
}

// OnMessage processes the update and prepares the response message
func (h DisableEnableAllDonorHandler) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := h.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	operation := findOperationByID(room, u.Button.CallbackData.OperationId)
	if hasAction(u, disableAllDonor) {
		operation.RecipientsWithSum = []api.RecipientWithSum{}
	} else {
		for _, member := range *room.Members {
			operation.RecipientsWithSum = append(operation.RecipientsWithSum, api.RecipientWithSum{User: member, Sum: float64(operation.Sum) / float64(len(*room.Members))})
		}
	}

	if err = h.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}
	u.Button.Action = addingOperation
	return api.TelegramMessage{
		Send:     true,
		Redirect: u,
	}
}

type AddedDonorAmountOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	rss RoomStateService
	cfg *Config
}

func NewAddedDonorAmountOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, rss RoomStateService, cfg *Config) *AddedDonorAmountOperation {
	return &AddedDonorAmountOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		rss: rss,
		cfg: cfg,
	}
}

func (s AddedDonorAmountOperation) HasReact(u *api.Update) bool {
	if u.ChatState == nil || u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
		return false
	}
	return u.ChatState.Action == setSumDonorOperation
}

// OnMessage returns one entry
func (s AddedDonorAmountOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	room, err := s.rs.FindById(ctx, u.ChatState.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("failed to retrieve room details")
		return
	}
	operation := findOperationByID(room, u.ChatState.CallbackData.OperationId)

	sum, err := defineSum(u.Message.Text)
	if err != nil {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		text := I18n(u.User, "msg_wrong_format")

		rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId})
		if _, err := s.bs.SaveAll(ctx, rb); err != nil {
			log.Error().Err(err).Stack().Msg("save buttons failed")
			return
		}
		return api.TelegramMessage{
			Chattable: []tgbotapi.Chattable{NewMessage(getChatID(u), text,
				[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), rb.ID.Hex())}})},
			Send: true,
		}
	}
	for i := range operation.RecipientsWithSum {
		if operation.RecipientsWithSum[i].User.ID == u.ChatState.CallbackData.UserId {
			operation.RecipientsWithSum[i].Sum = float64(sum)
		}
	}

	s.css.CleanChatState(ctx, u.ChatState)
	u.ChatState.CallbackData.Page = sum
	cs := &api.ChatState{UserId: int(getChatID(u)),
		CallbackData: u.ChatState.CallbackData,
		Action:       saveSumDonorOperation,
	}
	if err = s.css.Save(ctx, cs); err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	var recipient api.User
	for _, r := range operation.RecipientsWithSum {
		if r.User.ID == u.ChatState.CallbackData.UserId {
			recipient = r.User
			break
		}
	}

	var buttons []*api.Button
	cancelBtn := api.NewButton(addDonorOperation, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId, OperationId: operation.ID})
	saveBtn := api.NewButton(saveSumDonorOperation, &api.CallbackData{RoomId: u.ChatState.CallbackData.RoomId, UserId: recipient.ID, OperationId: operation.ID})
	editSumDonorOperationBtn := api.NewButton(editSumDonorOperation, &api.CallbackData{RoomId: room.ID.Hex(), UserId: recipient.ID, OperationId: operation.ID})
	buttons = append(buttons, cancelBtn, saveBtn, editSumDonorOperationBtn)
	if _, err = s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}
	tgButtons := [][]tgbotapi.InlineKeyboardButton{{
		tgbotapi.NewInlineKeyboardButtonData("❌Изменить", editSumDonorOperationBtn.ID.Hex()),
	}, {
		tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", saveBtn.ID.Hex()),
	}, {
		tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), cancelBtn.ID.Hex()),
	}}
	unallocatedSum := float64(operation.Sum)
	for _, recipientWithSum := range operation.RecipientsWithSum {
		unallocatedSum -= recipientWithSum.Sum
	}

	tb := sdk.NewTableBuilder('-', " | ")
	tb.AddColumn(sdk.Left, sdk.Monospaced, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			if operation.RecipientsWithSum[i].User.ID == recipient.ID {
				return "-> " + operation.RecipientsWithSum[i].User.DisplayName
			}
			return operation.RecipientsWithSum[i].User.DisplayName
		} else if i == len(operation.RecipientsWithSum) {
			return "Итого"
		}
		return ""
	})
	tb.AddColumn(sdk.Right, sdk.NumberWithTinySpaces, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			return moneySpace(int(operation.RecipientsWithSum[i].Sum), room.Currency)
		} else if i == len(operation.RecipientsWithSum) {
			total := 0
			for _, recipient := range operation.RecipientsWithSum {
				total += int(recipient.Sum)
			}
			return moneySpace(total, room.Currency)
		}
		return ""
	})
	tb.AddSeparatorRow(0)
	tb.AddSeparatorRow(len(operation.RecipientsWithSum))

	text := fmt.Sprintf(`
🔷 Проверьте введённые данные по [%s]:
💵 Сумма операции: %s
🔢 Введено: %s

%s

📊Распределение:
%s
    `,
		recipient.DisplayName,
		moneySpace(operation.Sum, room.Currency),
		moneySpace(sum, room.Currency),
		func() string {
			if RoundToTwoDecimalPlaces(unallocatedSum) > 0 {
				return fmt.Sprintf("⚠️ Осталось распределить: %s", moneySpace(int(unallocatedSum), room.Currency))
			} else if RoundToTwoDecimalPlaces(unallocatedSum) < 0 {
				return fmt.Sprintf("🔴Избыток: %s", moneySpace(int(-unallocatedSum), room.Currency))
			} else {
				return "Все средства распределены 💪"
			}
		}(),
		tb.Build(),
	)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{
			NewMessage(getChatID(u), text, tgButtons),
		},
		Send: true,
	}
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
	return u.Button != nil && u.Button.Action == addedOperation
	//hasAction(u, addedOperation)
}

// OnMessage returns one entry
func (s OperationAdded) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	opn := findOperationByID(room, u.Button.CallbackData.OperationId)
	if len(opn.RecipientsWithSum) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_choose_one_members"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	unallocatedSum := float64(opn.Sum)
	for _, recipientWithSum := range opn.RecipientsWithSum {
		unallocatedSum -= recipientWithSum.Sum
	}
	if RoundToTwoDecimalPlaces(unallocatedSum) != 0.00 {
		var text = "⚠️Ошибка при добавление операции:\n\n"
		if unallocatedSum > 0 {
			text += fmt.Sprintf("🔴Не распределено: %s", moneySpace(int(unallocatedSum), room.Currency))
		} else if unallocatedSum < 0 {
			text += fmt.Sprintf("🔴Избыток распределения: %s", moneySpace(int(-unallocatedSum), room.Currency))
		}

		callback := createCallback(u, text, true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var buttons []*api.Button
	var messages []tgbotapi.Chattable

	rb := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: opn.ID})
	backB := api.NewButton(viewStart, &api.CallbackData{})
	buttons = append(buttons, rb, backB)

	var oldOp api.Operation
	if opn.OldOperationId != nil {
		oldOp = findOperationByID(room, *opn.OldOperationId)
		if err := s.os.DeleteOperation(ctx, room.ID.Hex(), oldOp.ID); err != nil {
			log.Error().Err(err).Msg("upsert operation failed")
			return
		}
		newOp := opn
		buttons, messages = notificationWhenUpdateOperation(u, oldOp, newOp, room, buttons, messages)
	} else {
		messages = s.notificationWhenCreateOperation(ctx, u, opn, room, rb, backB, messages)
	}
	opn.Status = active
	opn.OldOperationId = nil
	if err = s.os.UpdateOperation(ctx, &opn, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	viewRoomBtn := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.Button.CallbackData.RoomId})
	buttons = append(buttons, viewRoomBtn)
	if _, err := s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	u.Button.Action = viewRoom
	return api.TelegramMessage{
		Chattable: messages,
		Send:      true,
		Redirect:  u,
	}
}

func (s OperationAdded) notificationWhenCreateOperation(ctx context.Context, u *api.Update, opn api.Operation, room *api.Room, rb *api.Button, backB *api.Button, messages []tgbotapi.Chattable) []tgbotapi.Chattable {

	if opn.Donor.ID != getFrom(u).ID {
		keyboard := [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData(I18n(opn.Donor, "btn_view_operation"), rb.ID.Hex())},
			{tgbotapi.NewInlineKeyboardButtonData(I18n(opn.Donor, "btn_to_start"), backB.ID.Hex())},
		}
		msg := NewMessage(int64(opn.Donor.ID),
			I18n(opn.Donor, "scrn_notification_payer_changed", userLink(opn.Donor), userLink(getFrom(u)), opn.Description, moneySpace(opn.Sum, room.Currency), room.Name),
			keyboard)
		messages = append(messages, msg)
		opn.NotificationSent = append(opn.NotificationSent, opn.Donor.ID)
	}

	for _, recipientsWithSum := range opn.RecipientsWithSum {
		if !slices.Contains(opn.NotificationSent, recipientsWithSum.User.ID) &&
			(recipientsWithSum.User.NotificationOn == nil || *recipientsWithSum.User.NotificationOn) &&
			recipientsWithSum.User.ID != getFrom(u).ID &&
			recipientsWithSum.Sum != 0 {
			var recipientWithSum api.RecipientWithSum
			for _, r := range opn.RecipientsWithSum {
				if r.User.ID == recipientsWithSum.User.ID {
					recipientWithSum = r
					break
				}
			}
			msg := NewMessage(int64(recipientsWithSum.User.ID), I18n(&recipientsWithSum.User, "scrn_notification_operation_added", userLink(&recipientsWithSum.User), userLink(getFrom(u)), opn.Description, moneySpace(opn.Sum, room.Currency), room.Name, moneySpace(int(recipientWithSum.Sum), room.Currency)),
				[][]tgbotapi.InlineKeyboardButton{
					{tgbotapi.NewInlineKeyboardButtonData(I18n(&recipientsWithSum.User, "btn_view_operation"), rb.ID.Hex())},
					{tgbotapi.NewInlineKeyboardButtonData(I18n(&recipientsWithSum.User, "btn_to_start"), backB.ID.Hex())},
				})
			opn.NotificationSent = append(opn.NotificationSent, recipientsWithSum.User.ID)
			if err := s.os.UpdateOperation(ctx, &opn, room.ID.Hex()); err != nil {
				log.Error().Err(err).Msg("upsert operation failed")
			}
			messages = append(messages, msg)
		}
	}
	return messages
}

func notificationWhenUpdateOperation(u *api.Update, oldOp api.Operation, newOp api.Operation, room *api.Room, buttons []*api.Button, messages []tgbotapi.Chattable) ([]*api.Button, []tgbotapi.Chattable) {
	diff := computeOperationDiff(oldOp, newOp)
	if diff == nil {
		return buttons, messages
	}
	donOpBut := api.NewButton(donorOperation, &api.CallbackData{RoomId: room.ID.Hex(), OperationId: newOp.ID})
	viewRoomBut := api.NewButton(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	buttons = append(buttons, donOpBut, viewRoomBut)
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_operation"), donOpBut.ID.Hex()),
		}, {
			tgbotapi.NewInlineKeyboardButtonData(I18n(newOp.Donor, "btn_view_room"), viewRoomBut.ID.Hex()),
		},
	}

	messages = append(messages, buildUpdateOperationMessages(getFrom(u), u.User, diff, oldOp, newOp, room, keyboard)...)
	return buttons, messages
}

// buildUpdateOperationMessages собирает уведомления об изменении операции — общая
// часть экранного сценария (notificationWhenUpdateOperation) и REST-уведомлений
// (Notifier). editor — кто внёс изменение (его пропускаем и упоминаем в текстах),
// langUser — чей язык используется в блоке «Было/Стало» (в боте это u.User)
func buildUpdateOperationMessages(editor *api.User, langUser *api.User, diff *api.OperationDiff, oldOp api.Operation, newOp api.Operation, room *api.Room, keyboard [][]tgbotapi.InlineKeyboardButton) []tgbotapi.Chattable {
	var messages []tgbotapi.Chattable
	editorUserId := editor.ID

	if oldOp.Donor.ID != newOp.Donor.ID {
		var targetUser *api.User
		if editorUserId != newOp.Donor.ID {
			targetUser = newOp.Donor
		} else if editorUserId != oldOp.Donor.ID {
			targetUser = oldOp.Donor
		}

		text := I18n(newOp.Donor, "scrn_notification_operation_updated_all",
			userLink(targetUser), newOp.Description, userLink(editor), "")
		text += "\nБыло:\n" +
			I18n(langUser, "scrn_user_paid", userLink(oldOp.Donor)) +
			tableWithPayments(oldOp, room)
		text += "\n\nСтало:\n" +
			I18n(langUser, "scrn_user_paid", userLink(newOp.Donor)) +
			tableWithPayments(newOp, room)

		for _, donor := range []*api.User{newOp.Donor, oldOp.Donor} {
			if editorUserId != donor.ID {
				msg := NewMessage(int64(donor.ID), text, keyboard)
				messages = append(messages, msg)
			}
		}
	}

	// 6.1 Если изменилось название или добавлено фото – уведомляем всех участников
	if diff.NameChanged || diff.PhotoAdded {
		var changeDetails string
		if diff.NameChanged {
			changeDetails += fmt.Sprintf("Название изменено: %s -> %s\n", oldOp.Description, newOp.Description)
		}
		if diff.PhotoAdded {
			changeDetails += "Добавлено фото.\n"
		}

		// Уведомляем донора, если он существует и у него включены уведомления
		notifiedUsers := make(map[int]bool)
		if newOp.Donor.NotificationOn == nil || (newOp.Donor.NotificationOn != nil && *newOp.Donor.NotificationOn) {
			text := I18n(newOp.Donor, "scrn_notification_operation_updated_all", userLink(newOp.Donor), newOp.Description, userLink(editor), changeDetails)
			msg := NewMessage(int64(newOp.Donor.ID), text, keyboard)
			messages = append(messages, msg)
			notifiedUsers[newOp.Donor.ID] = true
		}

		// Уведомляем всех получателей
		for _, r := range newOp.RecipientsWithSum {
			if (r.User.NotificationOn == nil || (r.User.NotificationOn != nil && *r.User.NotificationOn)) && !notifiedUsers[r.User.ID] {
				msg := NewMessage(int64(r.User.ID),
					I18n(&r.User, "scrn_notification_operation_updated_all", userLink(&r.User), newOp.Description, userLink(editor), changeDetails), keyboard)
				messages = append(messages, msg)
				notifiedUsers[r.User.ID] = true
			}
		}
	}

	// 6.2 Для добавленных получателей – уведомляем их, что их добавили в операцию
	for _, rAdded := range diff.RecipientsAdded {
		if ((rAdded.User.NotificationOn != nil && *rAdded.User.NotificationOn) ||
			rAdded.User.NotificationOn == nil) &&
			rAdded.User.ID != editorUserId &&
			rAdded.User.ID != newOp.Donor.ID &&
			rAdded.User.ID != oldOp.Donor.ID {
			msg := NewMessage(int64(rAdded.User.ID),
				I18n(&rAdded.User, "scrn_notification_operation_recipient_added", userLink(&rAdded.User), userLink(editor), newOp.Description, moneySpace(newOp.Sum, room.Currency), room.Name, moneySpace(int(rAdded.Sum), room.Currency)), keyboard)
			messages = append(messages, msg)
		}
	}

	// 6.3 Для получателей, у которых изменилась доля – уведомляем их об изменении
	for _, change := range diff.RecipientsShareChanged {
		if ((change.User.NotificationOn != nil && *change.User.NotificationOn) ||
			change.User.NotificationOn == nil) &&
			change.User.ID != editorUserId &&
			change.User.ID != newOp.Donor.ID &&
			change.User.ID != oldOp.Donor.ID {
			msg := NewMessage(int64(change.User.ID),
				I18n(&change.User, "scrn_notification_operation_share_changed", userLink(&change.User), newOp.Description, userLink(editor), moneySpace(newOp.Sum, room.Currency), room.Name, fmt.Sprintf("%.2f -> %.2f", change.OldSum, change.NewSum)), keyboard)
			messages = append(messages, msg)
		}
	}

	// 6.4 Для удалённых получателей – уведомляем их о том, что их убрали из операции
	for _, rRemoved := range diff.RecipientsRemoved {
		if ((rRemoved.User.NotificationOn != nil && *rRemoved.User.NotificationOn) ||
			rRemoved.User.NotificationOn == nil) &&
			rRemoved.User.ID != editorUserId &&
			rRemoved.User.ID != newOp.Donor.ID &&
			rRemoved.User.ID != oldOp.Donor.ID {
			msg := NewMessage(int64(rRemoved.User.ID),
				I18n(&rRemoved.User, "scrn_notification_operation_recipient_removed", userLink(&rRemoved.User), userLink(editor), newOp.Description, moneySpace(newOp.Sum, room.Currency), room.Name), keyboard)
			messages = append(messages, msg)
		}
	}
	return messages
}

// Функция для вычисления разницы между операциями
func computeOperationDiff(oldOp, newOp api.Operation) *api.OperationDiff {
	if oldOp.ID == primitive.NilObjectID || newOp.ID == primitive.NilObjectID {
		return nil
	}
	diff := api.OperationDiff{}

	// Изменение названия
	if oldOp.Description != newOp.Description {
		diff.NameChanged = true
	}

	// Добавлено фото (если количество файлов увеличилось)
	if len(newOp.Files) > len(oldOp.Files) {
		diff.PhotoAdded = true
	}

	// Для получателей формируем карты по ID для удобного сравнения
	oldRecipients := make(map[int]api.RecipientWithSum)
	for _, r := range oldOp.RecipientsWithSum {
		oldRecipients[r.User.ID] = r
	}
	newRecipients := make(map[int]api.RecipientWithSum)
	for _, r := range newOp.RecipientsWithSum {
		newRecipients[r.User.ID] = r
	}

	// Проверка: добавлены ли новые получатели или изменилась их доля
	for _, rNew := range newOp.RecipientsWithSum {
		if rOld, exists := oldRecipients[rNew.User.ID]; !exists {
			diff.RecipientsAdded = append(diff.RecipientsAdded, rNew)
		} else if rOld.Sum != rNew.Sum {
			diff.RecipientsShareChanged = append(diff.RecipientsShareChanged, api.RecipientShareChange{
				User:   rNew.User,
				OldSum: rOld.Sum,
				NewSum: rNew.Sum,
			})
		}
	}

	// Проверка: удалены ли получатели
	for _, rOld := range oldOp.RecipientsWithSum {
		if _, exists := newRecipients[rOld.User.ID]; !exists {
			diff.RecipientsRemoved = append(diff.RecipientsRemoved, rOld)
		}
	}

	return &diff
}

// Operation show screen with donar/recepient buttons
type ViewDonorOperation struct {
	os  OperationService
	bs  ButtonService
	rs  RoomService
	cfg *Config
}

// NewViewDonorOperation makes a bot for SO
func NewViewDonorOperation(bs ButtonService, os OperationService, rs RoomService, cfg *Config) *ViewDonorOperation {
	return &ViewDonorOperation{
		os:  os,
		bs:  bs,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start transaction600e68d102ddac9888d0193e
func (s ViewDonorOperation) HasReact(u *api.Update) bool {
	return hasAction(u, donorOperation)
}

// ViewDonorOperation only view operation information
func (s ViewDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	operation := findOperationByID(room, u.Button.CallbackData.OperationId)
	if (operation.ID == primitive.ObjectID{}) {
		callback := createCallback(u, "Операция удалена", true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	var btns []*api.Button
	var viewFileBtn *api.Button
	if len(operation.Files) > 0 {
		viewFileBtn = api.NewButton(viewFileOperation, u.Button.CallbackData)
		btns = append(btns, viewFileBtn)
	}
	editBtn := api.NewButton(editDonorOperation, u.Button.CallbackData)
	btns = append(btns, editBtn)

	cb := api.NewButton(chooseOperations, u.Button.CallbackData)
	btns = append(btns, cb)
	_, err = s.bs.SaveAll(ctx, btns...)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	text := I18n(u.User, "scrn_operation_on_sum", operation.Description, moneySpace(operation.Sum, room.Currency))
	text += I18n(u.User, "scrn_user_paid", userLink(operation.Donor))

	text += tableWithPayments(operation, room)

	text += "\n🗓 " + operation.CreateAt.Format("02 January 2006") + "\n"
	text += s.defineFileMessage(u.User, operation)
	var keyboard [][]tgbotapi.InlineKeyboardButton
	if viewFileBtn != nil {
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_view_file"), viewFileBtn.ID.Hex())})
	}
	if editBtn != nil {
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_edit_operation"), editBtn.ID.Hex())})
	}
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), cb.ID.Hex())})
	msg := createScreen(u, text, &keyboard)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{msg},
		Send:      true,
	}
}

func tableWithPayments(operation api.Operation, room *api.Room) string {
	tb := sdk.NewTableBuilder('-', " | ")
	tb.AddHeader("Участник")
	tb.AddHeader("Сумма")
	tb.AddColumn(sdk.Left, sdk.Monospaced, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			return operation.RecipientsWithSum[i].User.DisplayName
		}
		return ""
	})
	tb.AddColumn(sdk.Right, sdk.NumberWithTinySpaces, func(i int) string {
		if i < len(operation.RecipientsWithSum) {
			return moneySpace(int(operation.RecipientsWithSum[i].Sum), room.Currency)
		}
		return ""
	})
	return tb.Build()
}

func (s ViewDonorOperation) defineFileMessage(user *api.User, operation api.Operation) string {
	if len(operation.Files) > 0 {
		if operation.Files[0].Type == image {
			return I18n(user, "scrn_attach_photo")
		} else if operation.Files[0].Type == document {
			return I18n(user, "scrn_attach_file")
		} else if operation.Files[0].Type == video {
			return I18n(user, "scrn_attach_video")
		}
	}
	return ""
}

// DeleteDonorOperation show screen with deleted information and deleting donor operation
type DeleteDonorOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	rs  RoomService
	cfg *Config
}

func NewDeleteDonorOperation(s ChatStateService, bs ButtonService, os OperationService, rs RoomService, cfg *Config) *DeleteDonorOperation {
	return &DeleteDonorOperation{
		css: s,
		bs:  bs,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

func (s DeleteDonorOperation) HasReact(u *api.Update) bool {
	if u.Button == nil {
		return false
	}
	return u.Button.Action == deleteDonorOperation
}

func (s DeleteDonorOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	operation := findOperationByID(room, u.Button.CallbackData.OperationId)

	var buttons = []*api.Button{}
	var messages = []tgbotapi.Chattable{}
	rb := api.NewButton(viewRoom, &api.CallbackData{RoomId: u.Button.CallbackData.RoomId})
	buttons = append(buttons, rb)
	if operation.OldOperationId != nil {
		oldOperation := findOperationByID(room, u.Button.CallbackData.OperationId)
		operation.RecipientsWithSum = []api.RecipientWithSum{}
		buttons, messages = notificationWhenUpdateOperation(u, oldOperation, operation, room, buttons, messages)

		if err := s.os.DeleteOperation(ctx, u.Button.CallbackData.RoomId, oldOperation.ID); err != nil {
			log.Error().Err(err).Msg("delete operation failed")
			return api.TelegramMessage{}
		}
	}

	if err := s.os.DeleteOperation(ctx, u.Button.CallbackData.RoomId, u.Button.CallbackData.OperationId); err != nil {
		log.Error().Err(err).Msg("delete operation failed")
		return
	}

	if _, err := s.bs.SaveAll(ctx, buttons...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	keyboard := &[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), rb.ID.Hex())}}
	screen := createScreen(u, I18n(u.User, "scrn_operation_deleted"), keyboard)
	messages = append(messages, screen)

	return api.TelegramMessage{
		Chattable: messages,
		Send:      true,
	}
}

// WantAddFileToOperation screen with message please send me file for add to operation
type WantAddFileToOperation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	os  OperationService
	cfg *Config
}

func NewWantAddFileToOperation(s ChatStateService, bs ButtonService, rs RoomService, os OperationService, cfg *Config) *WantAddFileToOperation {
	return &WantAddFileToOperation{
		css: s,
		bs:  bs,
		rs:  rs,
		os:  os,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s WantAddFileToOperation) HasReact(u *api.Update) bool {
	return hasAction(u, wantAddFileToOperation)
}

// OnMessage returns one entry
func (s WantAddFileToOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	cancelBtn := api.NewButton(addingOperation, &api.CallbackData{RoomId: u.Button.CallbackData.RoomId, OperationId: u.Button.CallbackData.OperationId})
	_, err := s.bs.SaveAll(ctx, cancelBtn)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	cs := &api.ChatState{UserId: u.User.ID,
		Action: addFileToOperation,
		CallbackData: &api.CallbackData{
			RoomId:      u.Button.CallbackData.RoomId,
			OperationId: u.Button.CallbackData.OperationId,
		}}
	err = s.css.Save(ctx, cs)
	if err != nil {
		log.Error().Err(err).Msg("create chat state failed")
		return
	}

	keyboard := &[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), cancelBtn.ID.Hex())}}
	msg := createScreen(u, I18n(u.User, "scrn_send_file_for_opn"), keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{msg},
		Send:      true,
	}
}

// AddFileToOperation screen with save file and redirect to donor operation
type AddFileToOperation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewAddFileToOperation(s ChatStateService, bs ButtonService, rs RoomService, os OperationService, cfg *Config) *AddFileToOperation {
	return &AddFileToOperation{
		css: s,
		bs:  bs,
		rs:  rs,
		os:  os,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s AddFileToOperation) HasReact(u *api.Update) bool {
	return hasAction(u, addFileToOperation) && u.Message != nil &&
		(u.Message.Document != nil || u.Message.Image != nil || u.Message.Video != nil)
}

// OnMessage returns one entry
func (s AddFileToOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.ChatState.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	operation := findOperationByID(room, u.ChatState.CallbackData.OperationId)

	operation.Files = []api.File{}
	if u.Message.Image != nil {
		operation.Files = append(operation.Files, api.File{Type: image, FileId: u.Message.Image.FileID})
	} else if u.Message.Document != nil {
		operation.Files = append(operation.Files, api.File{Type: document, FileId: u.Message.Document.FileID})
	} else {
		operation.Files = append(operation.Files, api.File{Type: video, FileId: u.Message.Video.FileID})
	}

	if err = s.os.UpdateOperation(ctx, &operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}
	defer s.css.CleanChatState(ctx, u.ChatState)
	u.Button = api.NewButton(addingOperation, u.ChatState.CallbackData)
	u.ChatState = nil
	return api.TelegramMessage{
		Redirect: u,
		Send:     true,
	}
}

// AddFileToOperation screen with save file and redirect to donor operation
type ViewFileOperation struct {
	css ChatStateService
	bs  ButtonService
	rs  RoomService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewFileOperation(s ChatStateService, bs ButtonService, rs RoomService, os OperationService, cfg *Config) *ViewFileOperation {
	return &ViewFileOperation{
		css: s,
		bs:  bs,
		rs:  rs,
		os:  os,
		cfg: cfg,
	}
}

// ReactOn keys, example = /start operation600e68d102ddac9888d0193e
func (s ViewFileOperation) HasReact(u *api.Update) bool {
	return hasAction(u, viewFileOperation)
}

// OnMessage returns one entry
func (s ViewFileOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	operation := findOperationByID(room, u.Button.CallbackData.OperationId)

	var msg tgbotapi.Chattable
	file := operation.Files[0]
	chatId := getChatID(u)
	text := I18n(u.User, "scrn_operation_info", operation.Description, room.Name)
	if file.Type == document {
		message := NewDocumentMessage(chatId, text, file.FileId)
		message.ReplyToMessageID = getMessageId(u)
		msg = message
	} else if file.Type == image {
		message := NewPhotoMessage(chatId, text, file.FileId)
		message.ReplyToMessageID = getMessageId(u)
		msg = message
	} else if file.Type == video {
		message := NewVideoMessage(chatId, text, file.FileId)
		message.ReplyToMessageID = getMessageId(u)
		msg = message
	}

	viewRoomBtn := api.NewButton(donorOperation, u.Button.CallbackData)
	_, err = s.bs.SaveAll(ctx, viewRoomBtn)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}
	keyboard := &[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), viewRoomBtn.ID.Hex())}}
	backMsg := NewMessage(getChatID(u), I18n(u.User, "scrn_view_file"), *keyboard)
	return api.TelegramMessage{Chattable: []tgbotapi.Chattable{msg, backMsg},
		Send: true,
	}
}

// WantReturnDebt screen for debt returning
type WantReturnDebt struct {
	css ChatStateService
	bs  ButtonService
	us  UserService
	os  OperationService
	rs  RoomService
	cfg *Config
}

func NewWantReturnDebt(s ChatStateService, us UserService, bs ButtonService, os OperationService, rs RoomService, cfg *Config) *WantReturnDebt {
	return &WantReturnDebt{
		css: s,
		bs:  bs,
		us:  us,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

func (s WantReturnDebt) HasReact(u *api.Update) bool {
	return hasAction(u, wantReturnDebt)
}

// OnMessage returns one entry
func (s WantReturnDebt) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	lenderUserId := u.Button.CallbackData.UserId

	room, err := s.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}
	countUsersFinishedAddOperation := len(room.RoomStates.FinishedAddOperation)
	if len(*room.Members) != countUsersFinishedAddOperation {
		callback := createCallback(u, I18n(u.User, "msg_not_back_debt_operations_no_added"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}

	debt, err := s.os.GetUserDebt(ctx, u.User.ID, lenderUserId, roomId)
	if err != nil || debt == nil {
		log.Error().Err(err).Msg("get user debts failed")
		return
	}
	debtReturnedBtn := api.NewButton(debtReturned, &api.CallbackData{RoomId: roomId, UserId: lenderUserId, ExternalId: strconv.Itoa(debt.Sum)})
	setSumBtn := api.NewButton(setDebtSum, &api.CallbackData{RoomId: roomId, UserId: lenderUserId})
	cancelBtn := api.NewButton(viewRoom, &api.CallbackData{RoomId: roomId})
	_, err = s.bs.SaveAll(ctx, debtReturnedBtn, setSumBtn, cancelBtn)
	if err != nil {
		log.Error().Err(err).Msg("create btn failed")
		return
	}

	text := I18n(u.User, "scrn_debt_repayment")
	text += I18n(u.User, "scrn_debt_returning", userLink(debt.Lender), moneySpace(debt.Sum, room.Currency), GetCurrencySymbol(room.Currency))

	lender, err := s.us.FindById(ctx, debt.Lender.ID)
	if err == nil && lender != nil && lender.BankDetails != "" {
		text += I18n(u.User, "scrn_debt_returning_bank", lender.BankDetails)
	}
	text += I18n(u.User, "scrn_send_message_choose_user")

	msg := createScreen(u, text, &[][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_debt_sum_return", moneySpace(debt.Sum, room.Currency)), debtReturnedBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_debt_custom_sum_return"), setSumBtn.ID.Hex())},
		{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), cancelBtn.ID.Hex())}})
	return api.TelegramMessage{Chattable: []tgbotapi.Chattable{msg},
		Send: true,
	}
}

// DebtReturned for redirect on the AddRecepientOperation bot
type DebtReturned struct {
}

// NewStackOverflow makes a bot for SO
func NewDebtReturned() *DebtReturned {
	return &DebtReturned{}
}

func (s DebtReturned) HasReact(u *api.Update) bool {
	return hasAction(u, debtReturned)
}

// OnMessage returns one entry
func (s DebtReturned) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	u.ChatState = &api.ChatState{
		UserId:       u.User.ID,
		Action:       addRecipientOperation,
		CallbackData: &api.CallbackData{UserId: u.Button.CallbackData.UserId, RoomId: u.Button.CallbackData.RoomId}}
	u.Message = &api.Message{Text: u.Button.CallbackData.ExternalId, Chat: &api.Chat{Type: "private"}}
	return api.TelegramMessage{
		Send:     true,
		Redirect: u,
	}
}

type ChooseRecepientOperation struct {
	css ChatStateService
	bs  ButtonService
	us  UserService
	os  OperationService
	rs  RoomService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewChooseRecepientOperation(s ChatStateService, bs ButtonService, us UserService, os OperationService, rs RoomService, cfg *Config) *ChooseRecepientOperation {
	return &ChooseRecepientOperation{
		css: s,
		bs:  bs,
		us:  us,
		os:  os,
		rs:  rs,
		cfg: cfg,
	}
}

// ReactOn keys
func (s ChooseRecepientOperation) HasReact(u *api.Update) bool {
	return hasAction(u, setDebtSum)
}

// OnMessage returns one entry
func (s ChooseRecepientOperation) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	lenderUserId := u.Button.CallbackData.UserId

	room, err := s.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
		return
	}

	debt, err := s.os.GetUserDebt(ctx, u.User.ID, lenderUserId, roomId)
	if err != nil || debt == nil {
		log.Error().Err(err).Msg("get user debts failed")
		return
	}

	cs := &api.ChatState{UserId: int(getChatID(u)),
		Action:       addRecipientOperation,
		CallbackData: &api.CallbackData{RoomId: roomId, UserId: lenderUserId}}
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
	text += I18n(u.User, "scrn_debt_returning_operation", userLink(debt.Lender), moneySpace(debt.Sum, room.Currency), GetCurrencySymbol(room.Currency))

	lender, err := s.us.FindById(ctx, debt.Lender.ID)
	if err == nil && lender != nil && lender.BankDetails != "" {
		text += I18n(u.User, "scrn_debt_returning_bank", lender.BankDetails)
	}

	text += I18n(u.User, "scrn_send_message_choose_user")
	msg := createScreen(u, text,
		&[][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_cancel"), b.ID.Hex())}})
	return api.TelegramMessage{Chattable: []tgbotapi.Chattable{msg},
		Send: true,
	}
}

// AddRecepientOperation screen for debt returned or screen with wrong message
type AddRecepientOperation struct {
	css ChatStateService
	bs  ButtonService
	os  OperationService
	us  UserService
	rs  RoomService
	rss RoomStateService
	cfg *Config
}

func NewAddRecepientOperation(s ChatStateService, bs ButtonService, os OperationService, us UserService, rs RoomService, rss RoomStateService, cfg *Config) *AddRecepientOperation {
	return &AddRecepientOperation{
		css: s,
		bs:  bs,
		os:  os,
		us:  us,
		rs:  rs,
		rss: rss,
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
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	sum, err := defineSum(u.Message.Text)
	if err != nil || sum > debt.Sum {
		log.Error().Err(err).Msgf("not parsed %v", u.Message.Text)
		text := I18n(u.User, "msg_wrong_format")
		text += I18n(u.User, "scrn_debt_returning_operation", userLink(debt.Lender), moneySpace(debt.Sum, room.Currency), GetCurrencySymbol(room.Currency))

		lender, err := s.us.FindById(ctx, debt.Debtor.ID)
		if err == nil && lender != nil && lender.BankDetails != "" {
			text += I18n(u.User, "scrn_debt_returning_bank", lender.BankDetails)
		}

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
		ID:                primitive.NewObjectID(),
		Sum:               sum,
		Donor:             donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: *recipient, Sum: float64(sum)}},
		IsDebtRepayment:   true,
		Status:            active,
		CreateAt:          time.Now(),
	}
	if err = s.os.CreateOperation(ctx, operation, room.ID.Hex()); err != nil {
		log.Error().Err(err).Msg("upsert operation failed")
		return
	}

	//async calculate paidOfDebtsUserIds for room, after debt operation
	go func() {
		err := s.rss.DefinePaidOfDebtsUserIdsAndSave(ctx, room)
		if err != nil {
			log.Error().Err(err).Msg("DefinePaidOfDebtsUserIdsAndSave failed")
		}
	}()

	keyboard := [][]tgbotapi.InlineKeyboardButton{{tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_done"), rb.ID.Hex())}}
	forDonorMsg := createScreen(u, I18n(u.User, "scrn_debt_returned_lender", userLink(recipient), moneySpace(sum, room.Currency)), &keyboard)
	forRecipientMsg := NewMessage(int64(recipient.ID), I18n(u.User, "scrn_debt_returned_recepient", recipient.DisplayName, moneySpace(sum, room.Currency), userLink(donor)), keyboard)

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{forDonorMsg, forRecipientMsg},
		Send:      true,
	}
}

// Operation show screen with donar/recepient buttons
type ViewAllOperations struct {
	css ChatStateService
	rs  RoomService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

// NewStackOverflow makes a bot for SO
func NewViewAllOperations(s ChatStateService, rs RoomService, bs ButtonService, os OperationService, cfg *Config) *ViewAllOperations {
	return &ViewAllOperations{
		css: s,
		rs:  rs,
		bs:  bs,
		os:  os,
		cfg: cfg,
	}
}

func (bot ViewAllOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewAllOperations)
}

func (bot ViewAllOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := u.User.CountInPage
	skip := page * size

	ops, err := bot.os.GetAllSpendOperations(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get all operations failed")
		return
	}
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
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
		text := fmt.Sprintf("%s%s %s %s",
			func() string {
				if op.Status == active {
					return "🛒"
				}
				return "📝"
			}(),
			stringForAlign(op.Description, 11, true),
			stringForAlign("💰"+moneySpace(op.Sum, room.Currency), 8, false),
			stringForAlign("👤"+shortName(op.Donor), 8, false))
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
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	text := I18n(u.User, "scrn_all_operations")
	text += "\n\n🛒 Активная операция\n📝 Черновик\n"
	screen := createScreen(u, text, &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

// ViewMyOperations show screen with user chooseOperations
type ViewMyOperations struct {
	css ChatStateService
	rs  RoomService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewViewMyOperations(s ChatStateService, rs RoomService, bs ButtonService, os OperationService, cfg *Config) *ViewMyOperations {
	return &ViewMyOperations{
		css: s,
		rs:  rs,
		bs:  bs,
		os:  os,
		cfg: cfg,
	}
}

func (bot ViewMyOperations) HasReact(u *api.Update) bool {
	return hasAction(u, viewUserOperations)
}

func (bot ViewMyOperations) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := u.User.CountInPage
	skip := page * size

	ops, err := bot.os.GetUserSpendOperations(ctx, u.User.ID, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get user operations failed")
		return
	}
	room, err := bot.rs.FindById(ctx, u.Button.CallbackData.RoomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
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
		text := fmt.Sprintf("%s%s %s %s",
			func() string {
				if op.Status == active {
					return "🛒"
				}
				return "📝"
			}(),
			stringForAlign(op.Description, 8, true),
			stringForAlign("💰"+moneySpace(op.Sum, room.Currency), 8, false),
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
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	text := I18n(u.User, "scrn_my_operations")
	text += "\n\n🛒 Активная операция\n📝 Черновик\n"
	screen := createScreen(u, text, &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

// ViewOperationsWithMe show screen with user chooseOperations
type ViewOperationsWithMe struct {
	css ChatStateService
	rs  RoomService
	bs  ButtonService
	os  OperationService
	cfg *Config
}

func NewViewOperationsWithMe(s ChatStateService, rs RoomService, bs ButtonService, os OperationService, cfg *Config) *ViewOperationsWithMe {
	return &ViewOperationsWithMe{
		css: s,
		bs:  bs,
		rs:  rs,
		os:  os,
		cfg: cfg,
	}
}

func (bot ViewOperationsWithMe) HasReact(u *api.Update) bool {
	return hasAction(u, viewOperationsWithMe)
}

func (bot ViewOperationsWithMe) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	roomId := u.Button.CallbackData.RoomId
	page := u.Button.CallbackData.Page
	size := u.User.CountInPage
	skip := page * size

	ops, err := bot.os.GetUserParticipateInOperations(ctx, u.User.ID, roomId)
	if err != nil {
		return
	}
	if len(*ops) < 1 {
		callback := createCallback(u, I18n(u.User, "msg_have_not_operations_with_me"), true)
		return api.TelegramMessage{
			CallbackConfig: callback,
			Send:           true,
		}
	}
	room, err := bot.rs.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Msg("get room failed")
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
		text := fmt.Sprintf("🛒%s %s %s",
			stringForAlign(op.Description, 11, true),
			stringForAlign("💰"+moneySpace(op.Sum, room.Currency), 10, false),
			stringForAlign("👤"+shortName(op.Donor), 8, false))
		toSave = append(toSave, opB)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(text, opB.ID.Hex())})
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page != 0 {
		prevB := api.NewButton(viewOperationsWithMe, &api.CallbackData{RoomId: roomId, Page: page - 1})
		toSave = append(toSave, prevB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.LeftArrow), prevB.ID.Hex()))
	}
	backB := api.NewButton(chooseOperations, u.Button.CallbackData)
	toSave = append(toSave, backB)
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(I18n(u.User, "btn_back"), backB.ID.Hex()))
	if skip+size < len(*ops) {
		nextB := api.NewButton(viewOperationsWithMe, &api.CallbackData{RoomId: roomId, Page: page + 1})
		toSave = append(toSave, nextB)
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(string(emoji.RightArrow), nextB.ID.Hex()))
	}
	keyboard = append(keyboard, navRow)

	if _, err := bot.bs.SaveAll(ctx, toSave...); err != nil {
		log.Error().Err(err).Stack().Msg("save buttons failed")
		return
	}

	screen := createScreen(u, I18n(u.User, "scrn_operations_with_me"), &keyboard)
	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{screen},
		Send:      true,
	}
}

func findOperationByID(room *api.Room, id primitive.ObjectID) api.Operation {
	var operation api.Operation
	for _, o := range *room.Operations {
		if id == o.ID {
			operation = o
			break
		}
	}
	return operation
}

func RoundToTwoDecimalPlaces(num float64) float64 {
	return math.Round(num*100) / 100
}
