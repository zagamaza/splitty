package api

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"golang.org/x/text/language"
	"time"
)

// Response describes bot'service answer on particular message
type Response struct {
	Text        string
	Button      tgbotapi.InlineKeyboardMarkup //buttons
	Send        bool                          // status
	Pin         bool                          // enable pin
	Unpin       bool                          // enable unpin
	Preview     bool                          // enable web preview
	BanInterval time.Duration                 // bots banning user set the interval
}

// Update is an update response, from GetUpdates.
type Update struct {
	UpdateID    int          `json:"update_id"`
	Message     *Message     `json:"message"`
	InlineQuery *InlineQuery `json:"inline_query"`
	//ChosenInlineResult *ChosenInlineResult `json:"chosen_inline_result"`
	CallbackQuery *CallbackQuery `json:"callback_query"`

	ChatState    *ChatState
	Button       *Button
	User         *User
	FromRedirect bool
}

// Message is primary record to pass data from/to bots
type Message struct {
	ID       int
	From     User
	Chat     *Chat
	Sent     time.Time
	HTML     string    `json:",omitempty"`
	Text     string    `json:",omitempty"`
	Entities *[]Entity `json:",omitempty"`
	Image    *Image    `json:",omitempty"`
	Document *Document `json:",omitempty"`
	Video    *Video    `json:",omitempty"`
}

// Entity represents one special entity in a text message.
// For example, hashtags, usernames, URLs, etc.
type Entity struct {
	Type   string
	Offset int
	Length int
	URL    string `json:",omitempty"` // For “text_link” only, url that will be opened after user taps on the text
	User   *User  `json:",omitempty"` // For “text_mention” only, the mentioned user
}

// Image represents image
type Image struct {
	// FileID corresponds to Telegram file_id
	FileID   string
	Width    int
	Height   int
	Caption  string    `json:",omitempty"`
	Entities *[]Entity `json:",omitempty"`
}

// Documents represents image
type Document struct {
	FileID   string
	FileSize int
	MimeType string
}

type Video struct {
	FileID   string
	FileSize int
	MimeType string
}

// User defines user info of the Message
type User struct {
	ID             int    `json:"id" bson:"_id"`
	Username       string `json:"userName" bson:"user_name"`
	DisplayName    string `json:"displayName" bson:"display_name"`
	UserLang       string `json:"userLang" bson:"user_lang"`
	SelectedLang   string `json:"selectedLang" bson:"selected_lang"`
	NotificationOn *bool  `json:"notificationOn" bson:"notification_on,omitempty"`
	CountInPage    int    `json:"countInPage" bson:"count_in_page,omitempty"`
	BankDetails    string `json:"bankDetails" bson:"bank_details,omitempty"`
	// Notify — тонкие настройки уведомлений (категория × канал) из приложения;
	// nil — пользователь их не менял, действуют легаси-правила (см. AllowsTelegram)
	Notify *NotifySettings `json:"notify" bson:"notify,omitempty"`
}

// ChannelPrefs каналы доставки уведомлений одной категории;
// nil-поле — «не задано», действует значение по умолчанию
type ChannelPrefs struct {
	Telegram *bool `json:"telegram" bson:"telegram,omitempty"`
	Push     *bool `json:"push" bson:"push,omitempty"`
}

// NotifySettings настройки уведомлений по категориям событий
type NotifySettings struct {
	// Operations — добавление/изменение расходов в тусах пользователя
	Operations ChannelPrefs `json:"operations" bson:"operations,omitempty"`
	// Debts — возвраты долгов (и будущие напоминания)
	Debts ChannelPrefs `json:"debts" bson:"debts,omitempty"`
}

// NotifyCategory категория уведомления для проверки настроек
type NotifyCategory string

const (
	NotifyOperations NotifyCategory = "operations"
	NotifyDebts      NotifyCategory = "debts"
)

// AllowsTelegram слать ли пользователю telegram-уведомление категории.
// Приоритет: явная настройка категории → легаси-правила (operations —
// глобальный NotificationOn бота, debts — исторически слались всегда)
func (u *User) AllowsTelegram(category NotifyCategory) bool {
	if u == nil {
		return false
	}
	if u.Notify != nil {
		prefs := u.Notify.Operations
		if category == NotifyDebts {
			prefs = u.Notify.Debts
		}
		if prefs.Telegram != nil {
			return *prefs.Telegram
		}
	}
	if category == NotifyDebts {
		return true
	}
	return u.NotificationOn == nil || *u.NotificationOn
}

// WantsPush хочет ли пользователь push категории (доставка появится с APNs/FCM)
func (u *User) WantsPush(category NotifyCategory) bool {
	if u == nil || u.Notify == nil {
		return false
	}
	prefs := u.Notify.Operations
	if category == NotifyDebts {
		prefs = u.Notify.Debts
	}
	return prefs.Push != nil && *prefs.Push
}

func DefineLang(u *User) string {
	if u.SelectedLang != "" {
		return u.SelectedLang
	} else {
		if u.UserLang == language.English.String() || u.UserLang == language.Russian.String() {
			return u.UserLang
		} else {
			return language.English.String()
		}
	}
}

// InlineQuery is a Query from Telegram for an inline request.
type InlineQuery struct {
	ID     string `json:"id"`
	From   User   `json:"from"`
	Query  string `json:"query"`
	Offset string `json:"offset"`
}

// CallbackQuery is data sent when a keyboard button with callback data
// is clicked.
type CallbackQuery struct {
	ID              string   `json:"id"`
	From            User     `json:"from"`
	Message         *Message `json:"message"`           // optional
	InlineMessageID string   `json:"inline_message_id"` // optional
	ChatInstance    string   `json:"chat_instance"`
	Data            string   `json:"data"` // calback information
}

// Chat contains information about the place a message was sent.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type TelegramMessage struct {
	Chattable      []tgbotapi.Chattable
	InlineConfig   *tgbotapi.InlineConfig
	CallbackConfig *tgbotapi.CallbackConfig
	Redirect       *Update
	Send           bool // status
}
