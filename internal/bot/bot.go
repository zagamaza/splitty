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
const startOperation = start + " operation"

//actions
const (
	joinRoom               api.Action = "join_room"
	createRoom             api.Action = "create_room"
	wantRecipientOperation api.Action = "want_recipient_operation"
	wantDonorOperation     api.Action = "want_donor_operation"
	addDonorOperation      api.Action = "add_donor_operation"
	addRecipientOperation  api.Action = "add_recipient_operation"
	deleteDonorOperation   api.Action = "delete_donor_operation"
	donorOperation         api.Action = "donor_operation"
	chooseRecipient        api.Action = "choose_recipient"
	viewRoom               api.Action = "room"
	viewStart              api.Action = "start"
	viewAllOperations      api.Action = "all_operations"
	viewAllDebtOperations  api.Action = "all_dept_operations"
	viewAllRooms           api.Action = "all_rooms"
	viewUserDebts          api.Action = "user_debts"
	viewAllDebts           api.Action = "all_debts"
	viewStartOperation     api.Action = "start_operation"
	statistics             api.Action = "statistics"
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
