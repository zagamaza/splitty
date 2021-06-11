package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/gookit/i18n"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"regexp"
	"strconv"
	"strings"
)

type UserService interface {
	FindById(ctx context.Context, id int) (*api.User, error)
	SetUserLang(ctx context.Context, userId int, lang string) error
	SetCountInPage(ctx context.Context, userId int, count int) error
	SetNotificationUser(ctx context.Context, userId int, notification bool) error
}

type RoomService interface {
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	LeaveRoom(ctx context.Context, userId int, roomId string) error
	CreateRoom(ctx context.Context, u *api.Room) (*api.Room, error)
	FindById(ctx context.Context, id string) (*api.Room, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindArchivedRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
}

type RoomStateService interface {
	ArchiveRoom(ctx context.Context, userId int, roomId string) error
	UnArchiveRoom(ctx context.Context, userId int, roomId string) error
	FinishedAddOperation(ctx context.Context, userId int, roomId string) error
	UnFinishedAddOperation(ctx context.Context, userId int, roomId string) error
	PaidOfDebts(ctx context.Context, userIds []int, roomId string) error
	DefinePaidOfDebtsUserIdsAndSave(ctx context.Context, room *api.Room) error
}

type Config struct {
	BotName    string
	SuperUsers []string
}

func NewInlineResultArticle(title, descr, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.InlineQueryResultArticle {
	article := tgbotapi.NewInlineQueryResultArticleMarkdown(primitive.NewObjectID().Hex(), title, text)
	article.Description = descr
	article.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	return article
}

func NewInlineConfig(inlid string, results []interface{}) *tgbotapi.InlineConfig {
	return &tgbotapi.InlineConfig{
		InlineQueryID: inlid,
		IsPersonal:    true,
		CacheTime:     0,
		Results:       results,
	}
}

func NewEditInlineMessage(inlId string, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.EditMessageTextConfig {
	tbMsg := tgbotapi.EditMessageTextConfig{
		Text:      text,
		ParseMode: tgbotapi.ModeMarkdown,
	}
	tbMsg.InlineMessageID = inlId
	tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	return tbMsg
}

func NewEditMessage(chatId int64, msgId int, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.EditMessageTextConfig {
	tbMsg := tgbotapi.EditMessageTextConfig{
		Text:      text,
		ParseMode: tgbotapi.ModeMarkdown,
	}
	tbMsg.ChatID = chatId
	tbMsg.MessageID = msgId
	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	tbMsg.ReplyMarkup = &markup
	return tbMsg
}

func NewMessage(chatId int64, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.MessageConfig {
	tbMsg := tgbotapi.NewMessage(chatId, text)
	tbMsg.ParseMode = tgbotapi.ModeMarkdown
	tbMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	return tbMsg
}

func NewDocumentMessage(chatId int64, text string, fileId string) tgbotapi.DocumentConfig {
	docMsd := tgbotapi.NewDocumentShare(chatId, fileId)
	docMsd.ParseMode = tgbotapi.ModeMarkdown
	docMsd.Caption = text
	return docMsd
}

func NewPhotoMessage(chatId int64, text string, fileId string) tgbotapi.PhotoConfig {
	imageMsg := tgbotapi.NewPhotoShare(chatId, fileId)
	imageMsg.ParseMode = tgbotapi.ModeMarkdown
	imageMsg.Caption = text
	return imageMsg
}

func NewVideoMessage(chatId int64, text string, fileId string) tgbotapi.VideoConfig {
	imageMsg := tgbotapi.NewVideoShare(chatId, fileId)
	imageMsg.ParseMode = tgbotapi.ModeMarkdown
	imageMsg.Caption = text
	return imageMsg
}

func NewButtonSwitchCurrent(text, sw string) tgbotapi.InlineKeyboardButton {
	return tgbotapi.InlineKeyboardButton{
		Text:                         text,
		SwitchInlineQueryCurrentChat: &sw,
	}
}

func getChatID(update *api.Update) int64 {
	var chatId int64
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		chatId = update.CallbackQuery.Message.Chat.ID
	} else if update.CallbackQuery != nil {
		chatId = int64(update.CallbackQuery.From.ID)
	} else {
		chatId = update.Message.Chat.ID
	}
	return chatId
}

func isButton(update *api.Update) bool {
	return update.CallbackQuery != nil &&
		update.Button != nil &&
		update.CallbackQuery.InlineMessageID == ""
}

func isCommand(update *api.Update) bool {
	return update.Message != nil &&
		strings.HasPrefix(update.Message.Text, "/")
}

func isInline(update *api.Update) bool {
	return update.CallbackQuery != nil &&
		update.CallbackQuery.InlineMessageID != ""
}

func hasAction(update *api.Update, action api.Action) bool {
	return (update.Button != nil && update.Button.Action == action) ||
		(update.ChatState != nil && update.ChatState.Action == action)
}

func hasMessage(update *api.Update) bool {
	return update.Message != nil &&
		update.Message.Text != ""
}

func getMessageId(u *api.Update) int {
	return u.CallbackQuery.Message.ID
}

func getInlineId(u *api.Update) string {
	return u.CallbackQuery.InlineMessageID
}

func isPrivate(u *api.Update) bool {
	return u.Message != nil && u.Message.Chat.Type == "private" ||
		u.CallbackQuery != nil && u.CallbackQuery.Message != nil && u.CallbackQuery.Message.Chat.Type == "private"
}

func createScreen(u *api.Update, text string, keyboard *[][]tgbotapi.InlineKeyboardButton) tgbotapi.Chattable {
	if isInline(u) {
		return NewEditInlineMessage(getInlineId(u), text, *keyboard)
	} else if isButton(u) {
		return NewEditMessage(getChatID(u), getMessageId(u), text, *keyboard)
	} else {
		return NewMessage(getChatID(u), text, *keyboard)
	}
}

func createCallback(u *api.Update, text string, showAlert bool) *tgbotapi.CallbackConfig {
	return &tgbotapi.CallbackConfig{
		CallbackQueryID: u.CallbackQuery.ID,
		Text:            text,
		ShowAlert:       showAlert,
		URL:             "",
		CacheTime:       1,
	}
}

func shortName(user *api.User) string {
	sn := []rune(user.DisplayName)

	if len(sn) > 10 {
		split := strings.Split(string(sn), " ")
		if len(split) > 1 && len(split[1]) > 0 {
			sn = []rune(split[0] + " " + string([]rune(split[1])[0:1]) + ".")
		}
	}
	if len(sn) > 10 {
		return string(sn[0:10])
	}
	return string(sn)
}

func userLink(user *api.User) string {
	return fmt.Sprintf("[%s](tg://user?id=%d)", user.DisplayName, user.ID)
}

func moneySpace(sum int) string {
	s := strconv.Itoa(sum)
	re := regexp.MustCompile("(\\d+)(\\d{3})")
	for n := ""; n != s; {
		n = s
		s = re.ReplaceAllString(s, "$1 $2")
	}
	return s
}

func stringForAlign(s string, width int, spacesToEnd bool) string {
	rs := []rune(s)
	if len(rs) > width {
		if len(rs) > 2 {
			return string(rs[0:width-1]) + "..."
		} else {
			return s
		}
	} else if spacesToEnd {
		return s + strings.Repeat(" ", (width-len(rs))*2)
	} else {
		return strings.Repeat(" ", (width-len(rs))*2) + s
	}
}

func isArchived(room *api.Room, user *api.User) bool {
	return containsInt(room.RoomStates.Archived, user.ID)
}

func containsInt(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
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

func optimizeKeyboardButtons(buttons []tgbotapi.InlineKeyboardButton) [][]tgbotapi.InlineKeyboardButton {
	switch {
	case len(buttons) > 8 && len(buttons) <= 24:
		return splitKeyboardButtons(buttons, 3)
	case len(buttons) > 24:
		return splitKeyboardButtons(buttons, 4)
	default:
		return splitKeyboardButtons(buttons, 2)
	}
}

// I18n define text by user lang
func I18n(u *api.User, text string, args ...interface{}) string {
	tr := i18n.Tr(api.DefineLang(u), text, args...)
	return strings.ReplaceAll(tr, "\\n", "\n")
}
