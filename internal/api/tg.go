package api

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"time"
)

// Response describes bot'service answer on particular message
type Response struct {
	Text        string
	Button      []tgbotapi.InlineKeyboardButton
	Send        bool          // status
	Pin         bool          // enable pin
	Unpin       bool          // enable unpin
	Preview     bool          // enable web preview
	BanInterval time.Duration // bots banning user set the interval
}

// Message is primary record to pass data from/to bots
type Message struct {
	ID       int
	From     User
	ChatID   int64
	Sent     time.Time
	HTML     string    `json:",omitempty"`
	Text     string    `json:",omitempty"`
	Entities *[]Entity `json:",omitempty"`
	Image    *Image    `json:",omitempty"`
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

// User defines user info of the Message
type User struct {
	ID          int
	Username    string
	DisplayName string
}
