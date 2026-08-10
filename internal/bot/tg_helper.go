package bot

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/gookit/i18n"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type UserService interface {
	FindById(ctx context.Context, id int) (*api.User, error)
	// FindByIds — батч-чтение канонических документов. Списочные экраны
	// (участники комнаты, история долгов) упоминают десятки пользователей,
	// поштучный FindById дал бы N чтений на одну отрисовку (см. canonicalUsers.warm)
	FindByIds(ctx context.Context, ids []int) ([]api.User, error)
	FindByUsername(ctx context.Context, username string) (*api.User, error)
	SetUserLang(ctx context.Context, userId int, lang string) error
	SetCountInPage(ctx context.Context, userId int, count int) error
	SetNotificationUser(ctx context.Context, userId int, notification bool) error
	SetUserBankDetails(ctx context.Context, userId int, bankDerails string) error
}

type RoomService interface {
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	LeaveRoom(ctx context.Context, userId int, roomId string) (bool, error)
	CreateRoom(ctx context.Context, u *api.Room) (*api.Room, error)
	FindById(ctx context.Context, id string) (*api.Room, error)
	UpdateCurrency(ctx context.Context, roomId string, currency string) error
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
	article := tgbotapi.NewInlineQueryResultArticleHTML(primitive.NewObjectID().Hex(), title, text)
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
		ParseMode: tgbotapi.ModeHTML,
	}
	tbMsg.InlineMessageID = inlId
	tbMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	return tbMsg
}

func NewEditMessage(chatId int64, msgId int, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.EditMessageTextConfig {
	tbMsg := tgbotapi.EditMessageTextConfig{
		Text:      text,
		ParseMode: tgbotapi.ModeHTML,
	}
	tbMsg.ChatID = chatId
	tbMsg.MessageID = msgId
	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	tbMsg.ReplyMarkup = &markup
	return tbMsg
}

func NewMessage(chatId int64, text string, keyboard [][]tgbotapi.InlineKeyboardButton) tgbotapi.MessageConfig {
	tbMsg := tgbotapi.NewMessage(chatId, text)
	tbMsg.ParseMode = tgbotapi.ModeHTML
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
		// ИСКЛЮЧЕНИЕ из правила «в Telegram уходит только user.TelegramID»:
		// From пришёл в самом апдейте от Telegram, это telegram id по определению,
		// а не номер Splitty. Резолвить его через репозиторий не нужно и нечем
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

func hasChatStateAction(update *api.Update, action api.Action) bool {
	return (update.ChatState != nil && update.ChatState.Action == action)
}

func hasButtonAction(update *api.Update, action api.Action) bool {
	return update.Button != nil && update.Button.Action == action

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

// telegramChatID возвращает chat id для отправки в Telegram. false — у пользователя
// нет привязки к Telegram (вход через Google/Apple), telegram-канал пропускается,
// push при этом работает независимо.
//
// ВАЖНО: u обязан быть КАНОНИЧЕСКИМ документом пользователя. Во встроенных снимках
// комнат (room.users[], op.donor, op.recipientsWithSum[].user) telegram_id нет
// никогда — api.User.Snapshot() его обнуляет, а старые снимки писались ещё до
// появления поля. Для резолва есть canonicalUsers.
func telegramChatID(u *api.User) (int64, bool) {
	if !u.HasTelegram() {
		return 0, false
	}
	return int64(*u.TelegramID), true
}

// canonicalUsers резолвит канонические документы пользователей: и chat id для
// отправки, и упоминания в текстах должны браться из коллекции user, а не из
// встроенного снимка комнаты. Без этого telegram-уведомления перестали бы
// уходить вообще, а упоминания потеряли бы ссылку tg://user у всех живых
// telegram-пользователей.
//
// Кеш на время одной отрисовки: одного и того же участника (донор + получатель +
// редактор) уведомление упоминает по несколько раз, читать его повторно незачем.
type canonicalUsers struct {
	ctx   context.Context
	uf    UserFinder
	cache map[int]*api.User
	// warmed — id, по которым батч уже отвечал. Канонического документа у них
	// в базе нет (иначе он лежал бы в cache), и поштучно перечитывать их
	// незачем: иначе списочный экран всё равно выродился бы в N запросов
	warmed map[int]struct{}
}

// canonical собирает резолвер на время обработки одного апдейта/уведомления.
// uf может быть nil — тогда резолвер прозрачно отдаёт то, что ему передали
func canonical(ctx context.Context, uf UserFinder) *canonicalUsers {
	return &canonicalUsers{
		ctx:    ctx,
		uf:     uf,
		cache:  make(map[int]*api.User),
		warmed: make(map[int]struct{}),
	}
}

// warm прогревает кеш одним запросом. Нужен списочным экранам (участники
// комнаты, история долгов): без него отрисовка списка из N участников
// превратилась бы в N чтений пользователя. Best-effort — ошибка батча просто
// оставляет кеш пустым, get() дочитает поштучно
func (c *canonicalUsers) warm(ids []int) {
	if c == nil || c.uf == nil || len(ids) == 0 {
		return
	}
	var missing []int
	for _, id := range ids {
		if _, cached := c.cache[id]; cached {
			continue
		}
		if _, done := c.warmed[id]; done {
			continue
		}
		if slices.Contains(missing, id) {
			continue
		}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return
	}
	users, err := c.uf.FindByIds(c.ctx, missing)
	if err != nil {
		log.Warn().Err(err).Msg("can't batch read canonical users, falling back to embedded snapshots")
		return
	}
	for i := range users {
		u := users[i]
		c.cache[u.ID] = &u
	}
	for _, id := range missing {
		c.warmed[id] = struct{}{}
	}
}

// get отдаёт канонический документ. Не нашли или ошибка — возвращаем исходный
// снимок: имя в тексте важнее ссылки, а отправку всё равно отсечёт telegramChatID
func (c *canonicalUsers) get(u *api.User) *api.User {
	if u == nil {
		return nil
	}
	if c == nil || c.uf == nil {
		return u
	}
	if cached, ok := c.cache[u.ID]; ok {
		return cached
	}
	if _, ok := c.warmed[u.ID]; ok {
		// батч по этому id уже отработал и канонического документа не нашёл —
		// поштучное чтение вернуло бы то же самое
		return u
	}
	res := u
	if found, err := c.uf.FindById(c.ctx, u.ID); err != nil {
		log.Warn().Err(err).Int("user", u.ID).Msg("can't read canonical user, using embedded snapshot")
	} else if found != nil {
		res = found
	}
	c.cache[u.ID] = res
	return res
}

// link — упоминание пользователя по каноническому документу
func (c *canonicalUsers) link(u *api.User) string {
	return userLink(c.get(u))
}

// chatID — chat id пользователя по каноническому документу
func (c *canonicalUsers) chatID(u *api.User) (int64, bool) {
	return telegramChatID(c.get(u))
}

// userLink собирает упоминание пользователя. Кликабельная ссылка tg://user
// возможна только у пользователя с привязанным Telegram; у вошедших через
// Google/Apple остаётся экранированное имя. DisplayName задаёт сам пользователь
// и он может содержать "<" — без экранирования это и инъекция в общее сообщение,
// и 400 от Telegram, роняющий экран целиком.
//
// user обязан быть КАНОНИЧЕСКИМ: во встроенных снимках telegram_id нет никогда,
// поэтому по снимку ссылка не соберётся (см. canonicalUsers.link).
func userLink(user *api.User) string {
	if user == nil {
		return ""
	}
	name := html.EscapeString(user.DisplayName)
	chatId, ok := telegramChatID(user)
	if !ok {
		return name
	}
	return fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", chatId, name)
}

func moneySpace(sum int, currency string) string {
	s := strconv.Itoa(sum)
	re := regexp.MustCompile("(\\d+)(\\d{3})")
	for n := ""; n != s; {
		n = s
		s = re.ReplaceAllString(s, "$1 $2")
	}
	return s + " " + GetCurrencySymbol(currency)
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
	return slices.Contains(room.RoomStates.Archived, user.ID)
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
