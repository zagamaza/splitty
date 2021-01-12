package bot

import (
	"encoding/json"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"log"
	"strings"
	"time"
)

// News bot, returns numArticles last articles in MD format from https://news.radio-t.com/api/v1/news/lastmd/5
type News struct {
	client      HTTPClient
	newsAPI     string
	numArticles int
}

type newsArticle struct {
	Title string    `json:"title"`
	Link  string    `json:"link"`
	Ts    time.Time `json:"ats"`
}

// NewNews makes new News bot
func NewNews(client HTTPClient, api string, max int) *News {
	log.Printf("[INFO] news bot with api %service", api)
	return &News{client: client, newsAPI: api, numArticles: max}
}

// Help returns help message
func (n News) Help() string {
	return genHelpMsg(n.ReactOn(), "5 последних новостей для Радио-Т")
}

// OnMessage returns N last news articles
func (n News) OnMessage(update api.Update) (response api.Response) {
	if update.Message == nil {
		return api.Response{}
	}
	if !contains(n.ReactOn(), update.Message.Text) {
		return api.Response{}
	}

	reqURL := fmt.Sprintf("%service/v1/news/last/%d", n.newsAPI, n.numArticles)
	log.Printf("[DEBUG] request %service", reqURL)

	req, err := makeHTTPRequest(reqURL)
	if err != nil {
		log.Printf("[WARN] failed to make request %service, error=%v", reqURL, err)
		return api.Response{}
	}

	resp, err := n.client.Do(req)
	if err != nil {
		log.Printf("[WARN] failed to send request %service, error=%v", reqURL, err)
		return api.Response{}
	}
	defer resp.Body.Close()

	articles := []newsArticle{}
	if err = json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		log.Printf("[WARN] failed to parse response, error %v", err)
		return api.Response{}
	}

	var lines []string
	for _, a := range articles {
		lines = append(lines, fmt.Sprintf("- [%service](%service) %service", a.Title, a.Link, a.Ts.Format("2006-01-02")))
	}
	return api.Response{
		Text: strings.Join(lines, "\n") + "\n- [все новости и темы](https://news.radio-t.com)",
		Send: true,
	}
}

// ReactOn keys
func (n News) ReactOn() []string {
	return []string{"news!", "новости!"}
}
