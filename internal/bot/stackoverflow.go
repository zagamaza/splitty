package bot

import (
	"encoding/json"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// StackOverflow bot, returns from "https://api.stackexchange.com/2.2/questions?order=desc&sort=activity&site=stackoverflow"
// reacts on "so!" prefix, i.e. "so! golang"
type StackOverflow struct{}

// StackOverflow for json response
type soResponse struct {
	Items []struct {
		Title string   `json:"title"`
		Link  string   `json:"link"`
		Tags  []string `json:"tags"`
	} `json:"items"`
}

// NewStackOverflow makes a bot for SO
func NewStackOverflow() *StackOverflow {
	log.Printf("[INFO] StackOverflow bot with https://api.stackexchange.com/2.2/questions")
	return &StackOverflow{}
}

// Help returns help message
func (s StackOverflow) Help() string {
	return genHelpMsg(s.ReactOn(), "1 случайный вопрос со StackOverflow")
}

// OnMessage returns one entry
func (s StackOverflow) OnMessage(update api.Update) (response api.Response) {
	if update.Message == nil {
		return api.Response{}
	}

	if !contains(s.ReactOn(), update.Message.Text) {
		return api.Response{}
	}

	reqURL := "https://api.stackexchange.com/2.2/questions?order=desc&sort=activity&site=stackoverflow"
	client := http.Client{Timeout: 5 * time.Second}

	req, err := makeHTTPRequest(reqURL)
	if err != nil {
		log.Printf("[WARN] failed to prep request %service, error=%v", reqURL, err)
		return api.Response{}
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[WARN] failed to send request %service, error=%v", reqURL, err)
		return api.Response{}
	}
	defer resp.Body.Close()

	soRecs := soResponse{}

	if err := json.NewDecoder(resp.Body).Decode(&soRecs); err != nil {
		log.Printf("[WARN] failed to parse response, error %v", err)
		return api.Response{}
	}
	if len(soRecs.Items) == 0 {
		return api.Response{}
	}

	r := soRecs.Items[rand.Intn(len(soRecs.Items))]
	return api.Response{
		Text: fmt.Sprintf("[%service](%service) %service", r.Title, r.Link, strings.Join(r.Tags, ",")),
		Send: true,
	}
}

// ReactOn keys
func (s StackOverflow) ReactOn() []string {
	return []string{"so!"}
}
