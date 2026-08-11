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

// actions
const (
	joinRoom                 api.Action = "join_room"
	createRoom               api.Action = "create_room"
	wantReturnDebt           api.Action = "want_return_debt"
	wantDonorOperation       api.Action = "want_donor_operation"
	setDebtSum               api.Action = "set_debt_sum"
	debtReturned             api.Action = "debt_returned"
	addDonorOperation        api.Action = "add_donor_operation"
	addRecipientOperation    api.Action = "add_recipient_operation"
	deleteDonorOperation     api.Action = "delete_donor_operation"
	editDonorOperation       api.Action = "edit_donor_operation"
	addingOperation          api.Action = "adding_operation"
	changePayerOperation     api.Action = "change_payer_operation"
	choosePayerOperation     api.Action = "choose_payer_operation"
	chooseDonorOperation     api.Action = "choose_donor_Operation"
	enableAllDonor           api.Action = "enable_all_donor"
	disableAllDonor          api.Action = "disable_all_donor"
	editSumDonorOperation    api.Action = "edit_sum_donor_operation"
	setSumDonorOperation     api.Action = "set_sum_donor_operation"
	saveSumDonorOperation    api.Action = "save_sum_donor_operation"
	donorOperation           api.Action = "donor_operation"
	addedOperation           api.Action = "added_operation"
	chooseSplitTypeOperation api.Action = "choose_split_type_operation"
	addFileToOperation       api.Action = "add_file_to_operation"
	wantAddFileToOperation   api.Action = "want_add_file_to_operation"
	viewFileOperation        api.Action = "view_file_operation"
	viewRoom                 api.Action = "room"
	viewStart                api.Action = "start"
	viewAllOperations        api.Action = "all_operations"
	viewOperationsWithMe     api.Action = "operations_with_me"
	viewUserOperations       api.Action = "user_operations"
	viewAllDebtOperations    api.Action = "all_dept_operations"
	viewAllRooms             api.Action = "all_rooms"
	viewArchivedRooms        api.Action = "archived_rooms"
	viewUserDebts            api.Action = "user_debts"
	viewAllDebts             api.Action = "all_debts"
	statistics               api.Action = "statistics"
	chooseOperations         api.Action = "choose_operations"
	chooseDebts              api.Action = "choose_debts"
	roomSetting              api.Action = "room_setting"
	userSetting              api.Action = "user_setting"
	archiveRoom              api.Action = "archive_room"
	exitRoom                 api.Action = "exit_room"
	chooseCurrency           api.Action = "choose_currency"
	finishedAddOperation     api.Action = "finished_add_operation"
	countInPage              api.Action = "count_in_page"
	bankDetailsView          api.Action = "bank_details_view"
	bankDetailsWantSet       api.Action = "bank_details_want_set"
	bankDetailsSet           api.Action = "bank_details_set"
	unArchiveRoom            api.Action = "unarchive_room"
	chooseLanguage           api.Action = "choose_language"
	chooseNotification       api.Action = "choose_notification"
	selectedLanguage         api.Action = "selected_language"
	choseCurrency            api.Action = "chose_language"
	selectedNotification     api.Action = "selected_notification"
	unsupported              api.Action = "unsupported"
)

const (
	image    api.FileType = "image"
	video    api.FileType = "video"
	document api.FileType = "document"
)

// Статусы операции — из api: правило «какая операция действующая» общее
// с REST, и вторая копия констант уже приводила к расхождению трактовок
const (
	draft   = api.StatusDraft
	active  = api.StatusActive
	archive = api.StatusArchive
)

const (
	equally         api.SplitType = "equally"
	by_exact_amount api.SplitType = "by_exact_amount"
)

// currencyMap общий справочник валют — вынесен в api.Currencies,
// чтобы REST и бот использовали один словарь
var currencyMap = api.Currencies

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
// noinspection GoShadowedVar
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

// getFrom возвращает СЫРОГО пользователя из входящего telegram-апдейта: его ID —
// telegram user id, а НЕ номер пользователя Splitty.
//
// Как доменный id это значение использовать нельзя: у аккаунта, пришедшего через
// Google и привязавшего telegram, _id ≥ 10^12 при telegram id порядка 10^9, и
// поиск комнат/долгов/статистики по нему вернёт чужие или пустые данные, а
// запись в Operation.Donor испортит входные данные расчёта долгов. Канонический
// пользователь лежит в update.User (его проставляет UpsertTelegramUser).
//
// Законное применение одно — когда нужен именно telegram id входящего апдейта
// (см. getChatID в tg_helper.go:129).
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

func GetCurrency(code string) api.CurrencyInfo {
	info, _ := currencyMap[code]
	return info
}

func GetCurrencyFlag(code string) string {
	info, ok := currencyMap[code]
	if !ok {
		return currencyMap["RUB"].Flag
	}
	return info.Flag
}

func GetCurrencySymbol(code string) string {
	info, ok := currencyMap[code]
	if !ok {
		return currencyMap["RUB"].Symbol
	}
	return info.Symbol
}
