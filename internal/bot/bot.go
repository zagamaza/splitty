package bot

import (
	"context"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-pkgz/syncs"
	"github.com/pkg/errors"
)

//go:generate mockery -name HTTPClient -case snake
//go:generate mockery -inpkg -name Interface -case snake
//go:generate mockery -name SuperUser -case snake

// genHelpMsg construct help message from bot'service ReactOn
func genHelpMsg(com []string, msg string) string {
	return strings.Join(com, ", ") + " _– " + msg + "_\n"
}

// Interface is a bot reactive spec. response will be sent if "send" result is true
type Interface interface {
	OnMessage(msg Message) (response Response)
	ReactOn() []string
	Help() string
}

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

// HTTPClient wrap http.Client to allow mocking
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SuperUser defines interface checking ig user name in su list
type SuperUser interface {
	IsSuper(userName string) bool
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

// MultiBot combines many bots to one virtual
type MultiBot []Interface

// Help returns help message
func (b MultiBot) Help() string {
	sb := strings.Builder{}
	for _, child := range b {
		help := child.Help()
		if help != "" {
			// WriteString always returns nil err
			if !strings.HasSuffix(help, "\n") {
				help += "\n"
			}
			_, _ = sb.WriteString(help)
		}
	}
	return sb.String()
}

// OnMessage pass msg to all bots and collects reposnses (combining all of them)
//noinspection GoShadowedVar
func (b MultiBot) OnMessage(msg Message) (response Response) {
	if contains([]string{"help", "/help", "help!"}, msg.Text) {
		return Response{
			Text: b.Help(),
			Send: true,
		}
	}

	resps := make(chan string)
	btn := make(chan []tgbotapi.InlineKeyboardButton)
	var pin, unpin int32
	var banInterval time.Duration
	var mutex = &sync.Mutex{}

	var buttons []tgbotapi.InlineKeyboardButton
	wg := syncs.NewSizedGroup(4)
	for _, bot := range b {
		bot := bot
		wg.Go(func(ctx context.Context) {
			if resp := bot.OnMessage(msg); resp.Send {
				resps <- resp.Text

				if len(resp.Button) != 0 {
					buttons = resp.Button
				}

				if resp.Pin {
					atomic.AddInt32(&pin, 1)
				}
				if resp.Unpin {
					atomic.AddInt32(&unpin, 1)
				}
				if resp.BanInterval > 0 {
					mutex.Lock()
					if resp.BanInterval > banInterval {
						banInterval = resp.BanInterval
					}
					mutex.Unlock()
				}
			}
		})
	}

	go func() {
		wg.Wait()
		close(resps)
		close(btn)
	}()

	var lines []string
	for r := range resps {
		log.Printf("[DEBUG] collect %q", r)
		lines = append(lines, r)
	}

	sort.Slice(lines, func(i, j int) bool {
		return lines[i] < lines[j]
	})

	log.Printf("[DEBUG] answers %d, send %v", len(lines), len(lines) > 0)
	return Response{
		Text:        strings.Join(lines, "\n"),
		Button:      buttons,
		Send:        len(lines) > 0,
		Pin:         atomic.LoadInt32(&pin) > 0,
		Unpin:       atomic.LoadInt32(&unpin) > 0,
		BanInterval: banInterval,
	}
}

// ReactOn returns combined list of all keywords
func (b MultiBot) ReactOn() (res []string) {
	for _, bot := range b {
		res = append(res, bot.ReactOn()...)
	}
	return res
}

func contains(s []string, e string) bool {
	e = strings.TrimSpace(e)
	for _, a := range s {
		if strings.EqualFold(a, e) {
			return true
		}
	}
	return false
}

func makeHTTPRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to make request %service", url)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}
