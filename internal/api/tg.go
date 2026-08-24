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
	// ID — НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY, а не telegram user id. Исторически они
	// совпадали (telegram был единственным способом входа), поэтому у старых
	// аккаунтов _id действительно равен telegram id. Telegram-личность теперь
	// живёт в отдельном поле TelegramID, а пользователи, пришедшие через
	// Google/Apple, получают синтетический номер из аллокатора и telegram id не
	// имеют вовсе. В Telegram API можно передавать только TelegramID.
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
	// Aliases прозвища пользователя для AI-матчинга имён из речи («Саня» →
	// Александр Петров). Глобальные (не по комнате), пополняются, когда
	// пользователь разрешает нераспознанное имя в UI
	Aliases []string `json:"aliases,omitempty" bson:"aliases,omitempty"`
	// PushTokens — FCM-токены устройств пользователя (по одному на девайс).
	// Пополняются при логине/refresh, чистятся на logout и при отбраковке FCM
	// (UNREGISTERED). Дедуп по token (см. UserRepository.AddPushToken).
	PushTokens []PushToken `json:"-" bson:"push_tokens,omitempty"`
	// TokensValidFrom — отсечка отзыва: токены, выпущенные раньше, не работают.
	// Ставится «выйти на всех устройствах». Пусто — не отзывали никогда, и
	// установленные сборки продолжают работать
	TokensValidFrom *time.Time `json:"-" bson:"tokens_valid_from,omitempty"`
	// TelegramID — telegram user id, если аккаунт связан с telegram. nil у
	// пользователей, вошедших через Google/Apple. Unique sparse в mongo.
	TelegramID *int `json:"-" bson:"telegram_id,omitempty"`
	// GoogleSub — sub из id-токена Google. Unique sparse в mongo.
	GoogleSub string `json:"-" bson:"google_sub,omitempty"`
	// AppleSub — sub из id-токена Apple. Unique sparse в mongo.
	AppleSub string `json:"-" bson:"apple_sub,omitempty"`
	// Email — best-effort, НЕ идентификатор: Apple отдаёт relay-адрес и только
	// при первом входе, почта меняется. Аккаунты по email не склеиваются.
	Email string `json:"-" bson:"email,omitempty"`
	// LoginEmail — адрес входа по паролю, отдельное поле от Email именно потому,
	// что Email идентификатором не является и unique-индекс на нём сломал бы
	// вход тому, чей адрес совпал. Хранится нормализованным
	// (repository.NormalizeLoginEmail), unique sparse в mongo.
	LoginEmail string `json:"-" bson:"login_email,omitempty"`
	// PasswordHash — bcrypt пароля. Наружу не отдаётся никогда.
	PasswordHash string `json:"-" bson:"password_hash,omitempty"`
	// DeletedAt — tombstone: аккаунт удалён. Документ остаётся (иначе upsert-методы
	// репозитория воскресили бы его, а выданный JWT продолжал бы работать),
	// но PII вычищена, а поля личности освобождены под повторную регистрацию.
	DeletedAt *time.Time `json:"-" bson:"deleted_at,omitempty"`
	// NotificationsSeenAt — до какого момента человек просмотрел раздел
	// уведомлений. Одна отметка на пользователя, а не флаг на каждое событие:
	// лента выводится на лету, хранить строку на каждый расход каждому
	// получателю было бы write amplification без пользы
	NotificationsSeenAt *time.Time `json:"-" bson:"notifications_seen_at,omitempty"`
	// RoomsSeenAt — отметка прочитанного ПО КОМНАТЕ (ключ — hex id комнаты):
	// счётчик на карточке группы гасится открытием ЭТОЙ группы, а не заходом в
	// раздел «Уведомления» — иначе счётчики умирали бы раньше, чем человек
	// успевал ими воспользоваться (в раздел его ведёт как раз бейдж).
	//
	// Карта на документе пользователя, а не массив в room_states комнаты: там
	// лежат списки id, форма «кто → когда» туда не ложится, а на пользователе
	// отметки исчезают вместе с аккаунтом сами — отдельную коллекцию пришлось
	// бы дописывать в чистку руками (см. room_invite в rest/delete_account.go).
	//
	// Отсутствующий ключ — НЕ «непрочитано всё»: читатель откатывается на
	// общий NotificationsSeenAt (см. rest.roomSeenAt). Без этого в день выкатки
	// загорелись бы разом все карточки у всех
	RoomsSeenAt map[string]time.Time `json:"-" bson:"rooms_seen_at,omitempty"`
	// AppleRefreshToken — refresh token Apple, полученный обменом authorization
	// code при входе. Нужен, чтобы при удалении аккаунта отозвать токены через
	// POST https://appleid.apple.com/auth/revoke (Apple Guideline 5.1.1(v)).
	AppleRefreshToken string `json:"-" bson:"apple_refresh_token,omitempty"`
	// PurchaseBindingToken — случайный UUID, которым покупка в сторе
	// привязывается к аккаунту: клиент передаёт его в стор (appAccountToken у
	// Apple, obfuscatedAccountId у Google), стор возвращает его в чеке, сервер
	// сверяет с владельцем.
	//
	// Без такой привязки действует правило «чей чек — того, кто первый прислал»:
	// утёкший или расшаренный чек забирает тот, кто успел раньше, а настоящий
	// покупатель остаётся без Plus. Значение стабильно и не меняется: оно
	// вшивается в уже совершённые покупки, и ротация оторвала бы их от аккаунта.
	//
	// Наружу отдаётся только через GET /me — клиенту он нужен ДО покупки.
	PurchaseBindingToken string `json:"-" bson:"purchase_binding_token,omitempty"`
	// DevAuth — аккаунт заведён через POST /auth/dev (режим разработки).
	// Единственный смысл поля — отличить такой аккаунт от ИСТОРИЧЕСКОГО
	// telegram-пользователя: у обоих маленький _id и ни одного поля личности,
	// то есть по содержимому документа они неразличимы, а бэкфилл telegram_id
	// (repository.BackfillTelegramID) обязан трогать только вторых.
	DevAuth bool `json:"-" bson:"dev_auth,omitempty"`
}

// HasTelegram — привязан ли к аккаунту telegram. Только для таких пользователей
// имеет смысл отправка в Telegram API и ссылки вида tg://user?id=.
func (u *User) HasTelegram() bool {
	return u != nil && u.TelegramID != nil && *u.TelegramID != 0
}

// IsDeleted — помечен ли аккаунт удалённым (tombstone).
func (u *User) IsDeleted() bool {
	return u != nil && u.DeletedAt != nil
}

// Snapshot возвращает копию пользователя, пригодную для записи во встроенный
// снимок комнаты: поля личности, PII и персональные настройки обнулены.
//
// Зачем: JoinToRoom (repository.go) делает $push {users: u} целым api.User, и
// операции точно так же кладут пользователя в Donor/Recipients. Без санитайза
// telegram_id, google_sub, apple_sub, email и push-токены осели бы в документах
// room навсегда — их оттуда никто не чистит и не обновляет. Снимок нужен только
// ради id и отображаемого имени, всё остальное читается из канонического
// документа user.
func (u User) Snapshot() User {
	u.TelegramID = nil
	u.GoogleSub = ""
	u.AppleSub = ""
	u.Email = ""
	u.LoginEmail = ""
	u.PasswordHash = ""
	u.AppleRefreshToken = ""
	u.DeletedAt = nil
	u.PushTokens = nil
	u.Notify = nil
	u.Aliases = nil
	u.BankDetails = ""
	u.NotificationsSeenAt = nil
	u.RoomsSeenAt = nil
	u.DevAuth = false
	// Отсечка отзыва токенов — состояние аккаунта, а не личность: в снимках
	// внутри комнат ей делать нечего, а тащить её туда значило бы размножить
	// её по документам и однажды прочитать устаревшую копию
	u.TokensValidFrom = nil
	// Токен привязки покупок — секрет: кто его знает, тот может привязать свою
	// покупку к чужому аккаунту. Снимок участника лежит в документе комнаты и
	// виден всем её участникам, поэтому здесь его быть не должно
	u.PurchaseBindingToken = ""
	return u
}

// PushToken — FCM-токен одного устройства пользователя.
type PushToken struct {
	Token    string `json:"token" bson:"token"`
	Platform string `json:"platform" bson:"platform,omitempty"` // "android" | "ios"
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
	// Invites — приглашения в группы
	Invites ChannelPrefs `json:"invites" bson:"invites,omitempty"`
}

// NotifyCategory категория уведомления для проверки настроек
type NotifyCategory string

const (
	NotifyOperations NotifyCategory = "operations"
	NotifyDebts      NotifyCategory = "debts"
	// NotifyInvites — приглашения в комнаты. Собственные настройки категории
	// добавляются позже (NotifySettings.Invites); до этого действуют общий
	// дефолт «включено» и мастер-выключатель NotificationOn
	NotifyInvites NotifyCategory = "invites"
)

// AllowsTelegram слать ли пользователю telegram-уведомление категории.
// Приоритет: глобальный выключатель NotificationOn (мастер-тумблер) → явная
// настройка категории → легаси-дефолт (по умолчанию включено). Мастер работает
// как настоящий kill-switch: выключен — не шлём ничего (любой канал, любая
// категория, включая долги), даже если per-category telegram явно включён.
func (u *User) AllowsTelegram(category NotifyCategory) bool {
	if u == nil {
		return false
	}
	// Глобальный выключатель имеет приоритет над всем остальным.
	if u.NotificationOn != nil && !*u.NotificationOn {
		return false
	}
	if u.Notify != nil {
		prefs := u.Notify.Operations
		switch category {
		case NotifyDebts:
			prefs = u.Notify.Debts
		case NotifyInvites:
			prefs = u.Notify.Invites
		}
		if prefs.Telegram != nil {
			return *prefs.Telegram
		}
	}
	// Мастер включён (или не задан → включён): по умолчанию telegram-канал
	// активен для обеих категорий.
	return true
}

// WantsPush хочет ли пользователь push категории. Симметрично AllowsTelegram:
// глобальный выключатель NotificationOn → явная настройка категории → дефолт
// «включено». Push включён по умолчанию — устройство всё равно получит его
// только после регистрации FCM-токена (и выданного системного разрешения),
// так что для тех, кто не ставил приложение, это ничего не меняет.
func (u *User) WantsPush(category NotifyCategory) bool {
	if u == nil {
		return false
	}
	// Глобальный выключатель имеет приоритет над всем остальным.
	if u.NotificationOn != nil && !*u.NotificationOn {
		return false
	}
	if u.Notify != nil {
		prefs := u.Notify.Operations
		switch category {
		case NotifyDebts:
			prefs = u.Notify.Debts
		case NotifyInvites:
			prefs = u.Notify.Invites
		}
		if prefs.Push != nil {
			return *prefs.Push
		}
	}
	return true
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
