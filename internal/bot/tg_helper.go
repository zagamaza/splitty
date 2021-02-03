package bot

import (
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
