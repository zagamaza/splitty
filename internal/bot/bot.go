package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/go-pkgz/syncs"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
	"runtime/debug"
	"strings"
)

const start string = "/start"

//actions
const (
	joinRoom               api.Action = "join_room"
	createRoom             api.Action = "create_room"
	wantReturnDebt         api.Action = "want_return_debt"
	wantDonorOperation     api.Action = "want_donor_operation"
	setDebtSum             api.Action = "set_debt_sum"
	debtReturned           api.Action = "debt_returned"
	addDonorOperation      api.Action = "add_donor_operation"
	addRecipientOperation  api.Action = "add_recipient_operation"
	deleteDonorOperation   api.Action = "delete_donor_operation"
	editDonorOperation     api.Action = "edit_donor_operation"
	donorOperation         api.Action = "donor_operation"
	addedOperation         api.Action = "added_operation"
	addFileToOperation     api.Action = "add_file_to_operation"
	wantAddFileToOperation api.Action = "want_add_file_to_operation"
	viewFileOperation      api.Action = "view_file_operation"
	viewRoom               api.Action = "room"
	viewStart              api.Action = "start"
	viewAllOperations      api.Action = "all_operations"
	viewOperationsWithMe   api.Action = "operations_with_me"
	viewUserOperations     api.Action = "user_operations"
	viewAllDebtOperations  api.Action = "all_dept_operations"
	viewAllRooms           api.Action = "all_rooms"
	viewArchivedRooms      api.Action = "archived_rooms"
	viewUserDebts          api.Action = "user_debts"
	viewAllDebts           api.Action = "all_debts"
	statistics             api.Action = "statistics"
	chooseOperations       api.Action = "choose_operations"
	chooseDebts            api.Action = "choose_debts"
	roomSetting            api.Action = "room_setting"
	userSetting            api.Action = "user_setting"
	archiveRoom            api.Action = "archive_room"
	exitRoom               api.Action = "exit_room"
	finishedAddOperation   api.Action = "finished_add_operation"
	countInPage            api.Action = "count_in_page"
	bankDetailsView        api.Action = "bank_details_view"
	bankDetailsWantSet     api.Action = "bank_details_want_set"
	bankDetailsSet         api.Action = "bank_details_set"
	unArchiveRoom          api.Action = "unarchive_room"
	chooseLanguage         api.Action = "choose_language"
	chooseNotification     api.Action = "choose_notification"
	selectedLanguage       api.Action = "selected_language"
	selectedNotification   api.Action = "selected_notification"
)

const (
	image    api.FileType = "image"
	video    api.FileType = "video"
	document api.FileType = "document"
)

// Interface is a bot reactive spec. response will be sent if "send" result is true
type Interface interface {
	OnMessage(ctx context.Context, update *api.Update) (response api.TelegramMessage)
	HasReact(update *api.Update) bool
}

// SuperUser defines interface checking ig user name in su list
type SuperUser interface {
	IsSuper(userName string) bool
}

// MultiBot combines many bots to one virtual
type MultiBot []Interface

// OnMessage pass msg to all bots and collects reposnses (combining all of them)
//noinspection GoShadowedVar
func (b MultiBot) OnMessage(ctx context.Context, update *api.Update) (response api.TelegramMessage) {

	resps := make(chan api.TelegramMessage)
	btn := make(chan []tgbotapi.InlineKeyboardButton)

	wg := syncs.NewSizedGroup(4)
	for _, bot := range b {
		bot := bot
		wg.Go(func(ctx context.Context) {
			defer handlePanic(bot)
			if bot.HasReact(update) {
				if resp := bot.OnMessage(ctx, update); resp.Send {
					resps <- resp
				}
			}
		})
	}

	go func() {
		wg.Wait()
		close(resps)
		close(btn)
	}()

	message := &api.TelegramMessage{Chattable: []tgbotapi.Chattable{}}
	for r := range resps {
		log.Debug().Msgf("collect %v", r)
		message.Chattable = append(message.Chattable, r.Chattable...)
		message.InlineConfig = r.InlineConfig
		message.CallbackConfig = r.CallbackConfig
		message.Redirect = r.Redirect
		message.Send = true
	}

	return *message
}
func handlePanic(bot Interface) {
	if err := recover(); err != nil {
		switch e := err.(type) {
		case error:
			log.Error().Err(e).Stack().Msgf("panic! bot: %T, stack: %s", bot, string(debug.Stack()))
		default:
			log.Error().Stack().Msgf("panic! bot: %t, err: %v, stack: %s", bot, err, string(debug.Stack()))
		}
	}
}

func (b MultiBot) HasReact(u *api.Update) bool {
	var hasReact bool
	for _, bot := range b {
		hasReact = hasReact && bot.HasReact(u)
	}
	return hasReact
}

func contains(s []string, e string) bool {
	e = strings.TrimSpace(e)
	for _, a := range s {
		if strings.EqualFold(a, e) {
			return true
		}
	}
	return false
}

func getFrom(update *api.Update) *api.User {
	var user api.User
	if update.CallbackQuery != nil {
		user = update.CallbackQuery.From
	} else if update.Message != nil {
		user = update.Message.From
	} else {
		user = update.InlineQuery.From
	}
	return &user
}
