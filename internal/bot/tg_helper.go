package bot

import (
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ScreenTemplateType string

const (
	newMessage  ScreenTemplateType = "NEW_MESSAGE"
	editMessage ScreenTemplateType = "EDIT_MESSAGE"
	editInline  ScreenTemplateType = "EDIT_INLINE"
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

type ScreenTemplate struct {
	InlineId  string
	ChatId    int64
	MessageId int
	Text      string
	Keyboard  *[][]tgbotapi.InlineKeyboardButton
}

func BuildScreen(templ *ScreenTemplate, t ScreenTemplateType) tgbotapi.Chattable {
	switch t {
	case editMessage:
		return NewEditMessage(templ.ChatId, templ.MessageId, templ.Text, *templ.Keyboard)
	case editInline:
		return NewEditInlineMessage(templ.InlineId, templ.Text, *templ.Keyboard)
	default:
		return NewMessage(templ.ChatId, templ.Text, *templ.Keyboard)
	}
}
