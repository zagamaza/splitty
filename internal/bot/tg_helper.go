package bot

import (
	"github.com/almaznur91/splitty/internal/api"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"strings"
)

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

func NewMessageWithoutBtn(chatId int64, text string) tgbotapi.MessageConfig {
	tbMsg := tgbotapi.NewMessage(chatId, text)
	tbMsg.ParseMode = tgbotapi.ModeMarkdown
	return tbMsg
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
	return update.Button != nil &&
		update.Button.Action == action
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
