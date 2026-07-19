package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeDoer подставной транспорт: отдаёт заранее заданные ответы по очереди.
type fakeDoer struct {
	responses  []fakeResp
	calls      int
	lastBody   string
	lastURL    string
	lastAPIKey string
}

type fakeResp struct {
	status int
	body   string
	err    error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		f.lastBody = string(raw)
	}
	f.lastURL = req.URL.String()
	f.lastAPIKey = req.Header.Get("x-goog-api-key")
	i := f.calls
	f.calls++
	if i >= len(f.responses) {
		return nil, fmt.Errorf("неожиданный вызов #%d", i)
	}
	r := f.responses[i]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

func candidate(jsonDraft string) string {
	// экранируем как строку внутри JSON-ответа Gemini
	escaped := strings.ReplaceAll(jsonDraft, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", "")
	return fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":"%s"}]}}]}`, escaped)
}

func newTestClient(f *fakeDoer) *GeminiClient {
	return &GeminiClient{apiKey: "test-key", model: "gemini-2.0-flash", baseURL: "https://example.test/v1beta", http: f}
}

func TestGemini_Success(t *testing.T) {
	draft := `{"draft":{"description":"Ужин","sum":300,"items":[{"name":"Пицца","price":300,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}]},"questions":[]}`
	f := &fakeDoer{responses: []fakeResp{{status: 200, body: candidate(draft)}}}
	c := newTestClient(f)

	res, err := c.Parse(context.Background(), ParseInput{Text: "пицца 300", Currency: "RUB"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if res.Draft.Sum != 300 || len(res.Draft.Items) != 1 || res.Draft.Items[0].Name != "Пицца" {
		t.Fatalf("неожиданный черновик: %+v", res.Draft)
	}
	if f.calls != 1 {
		t.Fatalf("ожидался 1 вызов, было %d", f.calls)
	}
}

func TestGemini_APIKeyInHeaderNotURL(t *testing.T) {
	draft := `{"draft":{"description":"x","sum":0,"items":[]}}`
	f := &fakeDoer{responses: []fakeResp{{status: 200, body: candidate(draft)}}}
	c := newTestClient(f)

	if _, err := c.Parse(context.Background(), ParseInput{Text: "тест"}); err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if f.lastAPIKey != "test-key" {
		t.Fatalf("ключ не передан заголовком x-goog-api-key: %q", f.lastAPIKey)
	}
	if strings.Contains(f.lastURL, "test-key") || strings.Contains(f.lastURL, "key=") {
		t.Fatalf("ключ утёк в URL: %s", f.lastURL)
	}
}

// TestGemini_InvalidJSONNotRetried — при temperature=0 и фиксированной
// responseSchema повтор того же запроса вернёт тот же невалидный ответ,
// поэтому ретрая нет: ошибка сразу, платный вызов ровно один.
func TestGemini_InvalidJSONNotRetried(t *testing.T) {
	f := &fakeDoer{responses: []fakeResp{
		{status: 200, body: candidate("мусор 1")},
		{status: 200, body: candidate("мусор 2")},
	}}
	c := newTestClient(f)
	if _, err := c.Parse(context.Background(), ParseInput{Text: "тест"}); err == nil {
		t.Fatal("ожидалась ошибка на невалидном ответе")
	}
	if f.calls != 1 {
		t.Fatalf("ожидался 1 вызов (без ретрая), было %d", f.calls)
	}
}

func TestGemini_HTTPErrorNotRetried(t *testing.T) {
	f := &fakeDoer{responses: []fakeResp{{status: 500, body: `{"error":"boom"}`}}}
	c := newTestClient(f)
	if _, err := c.Parse(context.Background(), ParseInput{Text: "тест"}); err == nil {
		t.Fatal("ожидалась ошибка на HTTP 500")
	}
	if f.calls != 1 {
		t.Fatalf("HTTP-ошибка не должна ретраиться; вызовов %d", f.calls)
	}
}

func TestGemini_EmptyKey(t *testing.T) {
	c := &GeminiClient{apiKey: "", model: "m", baseURL: "x", http: &fakeDoer{}}
	if _, err := c.Parse(context.Background(), ParseInput{Text: "т"}); err == nil {
		t.Fatal("ожидалась ошибка при пустом ключе")
	}
}

func TestGemini_AudioInlineBase64(t *testing.T) {
	draft := `{"draft":{"description":"x","sum":0,"items":[]}}`
	f := &fakeDoer{responses: []fakeResp{{status: 200, body: candidate(draft)}}}
	c := newTestClient(f)

	_, err := c.Parse(context.Background(), ParseInput{Audio: []byte("аудиобайты"), AudioMime: "audio/aac"})
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	// в теле должен быть inline_data с base64 (не multipart)
	if !strings.Contains(f.lastBody, "inline_data") || !strings.Contains(f.lastBody, "audio/aac") {
		t.Fatalf("тело не содержит inline_data/mime: %s", truncate(f.lastBody, 200))
	}
	if !strings.Contains(f.lastBody, "responseSchema") {
		t.Fatalf("тело не содержит responseSchema")
	}
}
