package bot

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog/log"
)

const login string = "/login"

// loginCodeAlphabet без неоднозначных символов (0/O, 1/I/L)
const loginCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
const loginCodeLen = 8

// loginCodeTTL время жизни одноразового кода входа
const loginCodeTTL = 5 * time.Minute

type LoginCodeService interface {
	SaveLoginCode(ctx context.Context, c *api.LoginCode) error
}

// LoginScreen по команде /login в личном чате выдаёт одноразовый код
// для входа в мобильное приложение (обменивается на JWT через POST /api/v1/auth/code)
type LoginScreen struct {
	css ChatStateService
	lcs LoginCodeService
	cfg *Config
}

// NewLoginScreen makes a bot for screen login code
func NewLoginScreen(css ChatStateService, lcs LoginCodeService, cfg *Config) *LoginScreen {
	return &LoginScreen{
		css: css,
		lcs: lcs,
		cfg: cfg,
	}
}

// HasReact реагирует на /login только в личном чате. «/start login» —
// диплинк из приложения (t.me/bot?start=login): код выдаётся сразу по
// кнопке «Открыть бота», без ручного ввода команды.
func (s LoginScreen) HasReact(u *api.Update) bool {
	return isPrivate(u) && u.Message != nil &&
		(u.Message.Text == login || u.Message.Text == login+"@"+s.cfg.BotName ||
			u.Message.Text == start+" login")
}

// OnMessage returns one entry
func (s *LoginScreen) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	defer s.css.CleanChatState(ctx, u.ChatState)

	code, err := generateLoginCode()
	if err != nil {
		log.Error().Err(err).Msg("generate login code failed")
		return
	}

	lc := &api.LoginCode{
		Code:      code,
		UserId:    u.User.ID,
		ExpiresAt: time.Now().Add(loginCodeTTL),
		Used:      false,
	}
	if err := s.lcs.SaveLoginCode(ctx, lc); err != nil {
		log.Error().Err(err).Msg("save login code failed")
		return
	}

	tbMsg := tgbotapi.NewMessage(getChatID(u), I18n(u.User, "scrn_login_code", code))
	tbMsg.ParseMode = tgbotapi.ModeHTML

	return api.TelegramMessage{
		Chattable: []tgbotapi.Chattable{tbMsg},
		Send:      true,
	}
}

// generateLoginCode криптослучайный код из loginCodeAlphabet;
// rand.Int вместо остатка от деления — без modulo bias
func generateLoginCode() (string, error) {
	max := big.NewInt(int64(len(loginCodeAlphabet)))
	code := make([]byte, loginCodeLen)
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		code[i] = loginCodeAlphabet[n.Int64()]
	}
	return string(code), nil
}
