package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// httpDoer позволяет подставить фейковый транспорт в тестах.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// GeminiClient реализует Parser поверх Gemini generateContent REST API.
// Медиа передаётся как inline_data (base64) внутри JSON-тела, ответ
// возвращается по responseSchema как JSON-черновик.
type GeminiClient struct {
	apiKey  string
	model   string
	baseURL string
	http    httpDoer
}

// NewGemini создаёт клиент. Пустой apiKey допускается конструктором, но Parse
// вернёт ошибку — вызывающий код обязан отключать фичу при пустом ключе.
func NewGemini(apiKey, model string) *GeminiClient {
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// --- форма запроса/ответа generateContent ---

type geminiPart struct {
	Text       string        `json:"text,omitempty"`
	InlineData *geminiInline `json:"inline_data,omitempty"`
}

type geminiInline struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig map[string]any  `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

// Parse распознаёт вход в черновик. Один повтор при невалидном JSON ответа.
func (c *GeminiClient) Parse(ctx context.Context, in ParseInput) (ParseResult, error) {
	if c.apiKey == "" {
		return ParseResult{}, fmt.Errorf("ai: пустой GEMINI_API_KEY")
	}

	body, err := c.buildRequest(in)
	if err != nil {
		return ParseResult{}, err
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		res, err := c.call(ctx, body)
		if err != nil {
			return ParseResult{}, err // сетевые/HTTP ошибки не ретраим здесь
		}
		parsed, err := parseCandidate(res)
		if err == nil {
			return parsed, nil
		}
		lastErr = err // невалидный JSON — повторяем один раз
	}
	return ParseResult{}, fmt.Errorf("ai: модель вернула невалидный ответ: %w", lastErr)
}

func (c *GeminiClient) buildRequest(in ParseInput) ([]byte, error) {
	parts := []geminiPart{{Text: buildPrompt(in)}}
	switch in.Media {
	case MediaAudio, MediaImage:
		if len(in.Data) == 0 || in.Mime == "" {
			return nil, fmt.Errorf("ai: пустое медиа или mime")
		}
		parts = append(parts, geminiPart{InlineData: &geminiInline{
			MimeType: in.Mime,
			Data:     base64.StdEncoding.EncodeToString(in.Data),
		}})
	case MediaText:
		if strings.TrimSpace(in.Text) == "" {
			return nil, fmt.Errorf("ai: пустой текст")
		}
		parts = append(parts, geminiPart{Text: "Ввод пользователя: " + in.Text})
	}

	req := geminiRequest{
		Contents: []geminiContent{{Parts: parts}},
		GenerationConfig: map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   draftSchema(),
			"temperature":      0,
		},
	}
	return json.Marshal(req)
}

func (c *GeminiClient) call(ctx context.Context, body []byte) (geminiResponse, error) {
	// ключ передаём заголовком x-goog-api-key, а не query-параметром, чтобы он
	// не утёк в логи вместе с URL при сетевых/HTTP-ошибках
	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return geminiResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return geminiResponse{}, fmt.Errorf("ai: запрос к Gemini: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return geminiResponse{}, fmt.Errorf("ai: Gemini вернул %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var gr geminiResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return geminiResponse{}, fmt.Errorf("ai: разбор ответа Gemini: %w", err)
	}
	return gr, nil
}

// parseCandidate достаёт текст первого кандидата и разбирает его как ParseResult.
func parseCandidate(gr geminiResponse) (ParseResult, error) {
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return ParseResult{}, fmt.Errorf("пустой ответ модели")
	}
	text := gr.Candidates[0].Content.Parts[0].Text
	if strings.TrimSpace(text) == "" {
		return ParseResult{}, fmt.Errorf("пустой текст кандидата")
	}
	var out ParseResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return ParseResult{}, err
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
