package bot

import (
	"context"
	"html"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

const report string = "/report"

type BugReportService interface {
	SaveBugReport(ctx context.Context, r *api.BugReport) error
}

// ReportScreen по команде /report <текст> сохраняет баг-репорт в базу
// и уведомляет суперюзеров личным сообщением. /report без текста —
// подсказка, как отправить репорт.
type ReportScreen struct {
	css ChatStateService
	brs BugReportService
	us  UserService
	cfg *Config
}

// NewReportScreen makes a bot for the /report command
func NewReportScreen(css ChatStateService, brs BugReportService, us UserService, cfg *Config) *ReportScreen {
	return &ReportScreen{
		css: css,
		brs: brs,
		us:  us,
		cfg: cfg,
	}
}

// HasReact реагирует на /report и /report@бот с текстом и без
func (s ReportScreen) HasReact(u *api.Update) bool {
	if u.Message == nil {
		return false
	}
	text := u.Message.Text
	return text == report || text == report+"@"+s.cfg.BotName ||
		strings.HasPrefix(text, report+" ") || strings.HasPrefix(text, report+"@"+s.cfg.BotName+" ")
}

// OnMessage returns one entry
func (s *ReportScreen) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer s.css.CleanChatState(ctx, u.ChatState)

	text := strings.TrimSpace(strings.TrimPrefix(u.Message.Text, report+"@"+s.cfg.BotName))
	text = strings.TrimSpace(strings.TrimPrefix(text, report))
	if text == "" {
		tbMsg := tgbotapi.NewMessage(getChatID(u), I18n(u.User, "scrn_report_hint"))
		tbMsg.ParseMode = tgbotapi.ModeHTML
		return api.TelegramMessage{Chattable: []tgbotapi.Chattable{tbMsg}, Send: true}
	}

	br := &api.BugReport{
		UserId:      u.User.ID,
		Username:    u.User.Username,
		DisplayName: u.User.DisplayName,
		Text:        text,
		CreateAt:    time.Now(),
	}
	if err := s.brs.SaveBugReport(ctx, br); err != nil {
		log.Error().Err(err).Msg("save bug report failed")
		return
	}

	chattable := []tgbotapi.Chattable{}
	// Уведомление суперюзерам в личку (включая автора-суперюзера — так видно,
	// что механика работает); кто не писал боту — молча пропускается.
	for _, username := range s.cfg.SuperUsers {
		su, err := s.us.FindByUsername(ctx, username)
		if err != nil || su == nil {
			log.Warn().Err(err).Msgf("superuser %s not found, report notification skipped", username)
			continue
		}
		// su — канонический документ (FindByUsername), но telegram у него может быть
		// не привязан (вход через Google/Apple) — тогда слать в telegram некуда
		chatId, ok := telegramChatID(su)
		if !ok {
			log.Warn().Msgf("superuser %s has no telegram, report notification skipped", username)
			continue
		}
		// ParseMode=HTML: текст репорта — сырой ввод пользователя, экранируем,
		// иначе это и HTML-инъекция в ЛС суперюзера, и 400 от Telegram на "a < b"
		notify := tgbotapi.NewMessage(chatId,
			I18n(su, "scrn_report_notify", userLink(u.User), html.EscapeString(text)))
		notify.ParseMode = tgbotapi.ModeHTML
		chattable = append(chattable, notify)
	}

	thanks := tgbotapi.NewMessage(getChatID(u), I18n(u.User, "scrn_report_thanks"))
	thanks.ParseMode = tgbotapi.ModeHTML
	chattable = append(chattable, thanks)

	return api.TelegramMessage{Chattable: chattable, Send: true}
}
