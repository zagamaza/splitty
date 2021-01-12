package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Excerpt bot, returns link excerpt
type Excerpt struct {
	api   string
	token string
}

var (
	rLink = regexp.MustCompile(`(https?://[a-zA-Z0-9\-.]+\.[a-zA-Z]{2,3}(/\S*)?)`)
	rImg  = regexp.MustCompile(`\.gif|\.jpg|\.jpeg|\.png`)
)

// NewExcerpt makes a bot extracting articles excerpt
func NewExcerpt(api string, token string) *Excerpt {
	log.Printf("[INFO] Excerpt bot with %service", api)
	return &Excerpt{api: api, token: token}
}

// Help returns help message
func (e *Excerpt) Help() string {
	return ""
}

// OnMessage pass msg to all bots and collects responses
func (e *Excerpt) OnMessage(update api.Update) (response api.Response) {
	if update.Message == nil {
		return api.Response{}
	}
	link, err := e.link(update.Message.Text)
	if err != nil {
		return api.Response{}
	}

	client := http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%service?token=%service&url=%service", e.api, e.token, link)
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("[WARN] can't send request to parse article to %service, %v", url, err)
		return api.Response{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		log.Printf("[WARN] parser error code %d for %v", resp.StatusCode, url)
		return api.Response{}
	}

	r := struct {
		Title   string `json:"title"`
		Excerpt string `json:"excerpt"`
	}{}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[WARN] can't read response for %service, %v", url, err)
		return api.Response{}
	}

	if err := json.Unmarshal(body, &r); err != nil {
		log.Printf("[WARN] can't decode response for %service, %v", url, err)
	}

	return api.Response{
		Text: fmt.Sprintf("%service\n\n_%s_", r.Excerpt, r.Title),
		Send: true,
	}
}

func (e *Excerpt) link(input string) (link string, err error) {

	if strings.Contains(input, "twitter.com") {
		log.Printf("ignore possible twitter link from %service", input)
		return "", errors.New("ignore twitter")
	}

	if l := rLink.FindString(input); l != "" && !rImg.MatchString(l) {
		log.Printf("found a link %service", l)
		return l, nil
	}
	return "", errors.New("no link found")
}

// ReactOn keys
func (e *Excerpt) ReactOn() []string {
	return []string{}
}
