package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// maxGeminiResponseBytes — потолок чтения ответа модели. Черновик по
// responseSchema — это единицы килобайт; 4 МБ с огромным запасом.
const maxGeminiResponseBytes = 4 << 20

// httpDoer позволяет подставить фейковый транспорт в тестах.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// GeminiClient реализует Parser поверх Gemini generateContent REST API.
// Медиа передаётся как inline_data (base64) внутри JSON-тела, ответ
// возвращается по responseSchema как JSON-черновик.
type GeminiClient struct {
	apiKey    string
	model     string
	fallbacks []string
	backoff   time.Duration
	baseURL   string
	http      httpDoer
}

// statusError — ответ Gemini не 200. Код нужен, чтобы отличить перегрузку
// («high demand», 503/429) от отказа по сути запроса.
type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("ai: Gemini вернул %d: %s", e.code, e.body)
}

// transient — сбой на стороне Google, который лечится повтором или другой
// моделью. Отменённый клиентом запрос сюда не входит: ждать его уже некому.
func transient(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests || se.code >= 500
	}
	// Минутный таймаут уже съел всё терпение человека: повтор с тем же
	// потолком только удвоит ожидание.
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	return true
}

// NewGemini создаёт клиент. Пустой apiKey допускается конструктором, но Parse
// вернёт ошибку — вызывающий код обязан отключать фичу при пустом ключе.
// fallbacks — модели, к которым уходит запрос, когда основная перегружена.
func NewGemini(apiKey, model string, fallbacks ...string) *GeminiClient {
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}
	var fb []string
	for _, m := range fallbacks {
		if m = strings.TrimSpace(m); m != "" && m != model {
			fb = append(fb, m)
		}
	}
	return &GeminiClient{
		apiKey:    apiKey,
		model:     model,
		fallbacks: fb,
		backoff:   time.Second,
		baseURL:   defaultBaseURL,
		http:      &http.Client{Timeout: 60 * time.Second},
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

	res, err := c.callWithFallback(ctx, body)
	if err != nil {
		return ParseResult{}, err
	}
	parsed, err := parseCandidate(res)
	if err != nil {
		// ретрая нет: при temperature=0 и фиксированной responseSchema повтор
		// того же запроса детерминированно вернёт тот же невалидный ответ
		return ParseResult{}, fmt.Errorf("ai: модель вернула невалидный ответ: %w", err)
	}
	return parsed, nil
}

func (c *GeminiClient) buildRequest(in ParseInput) ([]byte, error) {
	if !in.HasMedia() {
		return nil, fmt.Errorf("ai: нет ввода (ни фото, ни голоса, ни текста)")
	}
	// один запрос — несколько частей: фото чека + аудио + текст вместе
	parts := []geminiPart{{Text: buildPrompt(in)}}
	if len(in.Image) > 0 {
		parts = append(parts, geminiPart{InlineData: &geminiInline{
			MimeType: in.ImageMime,
			Data:     base64.StdEncoding.EncodeToString(in.Image),
		}})
	}
	if len(in.Audio) > 0 {
		parts = append(parts, geminiPart{InlineData: &geminiInline{
			MimeType: in.AudioMime,
			Data:     base64.StdEncoding.EncodeToString(in.Audio),
		}})
	}
	if t := strings.TrimSpace(in.Text); t != "" {
		parts = append(parts, geminiPart{Text: "Ввод пользователя: " + t})
	}

	req := geminiRequest{
		Contents: []geminiContent{{Parts: parts}},
		GenerationConfig: map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   draftSchema(in.Fractional),
			"temperature":      0,
		},
	}
	return json.Marshal(req)
}

// callWithFallback: основная модель — попытка и один повтор после паузы,
// затем по одной попытке на каждую запасную. Перегрузка у Google длится
// минутами, а человек ждёт у микрофона, поэтому повторов немного.
func (c *GeminiClient) callWithFallback(ctx context.Context, body []byte) (geminiResponse, error) {
	attempts := []string{c.model, c.model}
	attempts = append(attempts, c.fallbacks...)
	var err error
	for i, model := range attempts {
		if i == 1 {
			select {
			case <-ctx.Done():
				return geminiResponse{}, err
			case <-time.After(c.backoff):
			}
		}
		var res geminiResponse
		res, err = c.call(ctx, model, body)
		if err == nil {
			if i > 0 {
				log.Warn().Str("model", model).Int("attempt", i+1).Msg("ai: Gemini ответил после повтора")
			}
			return res, nil
		}
		if !transient(ctx, err) {
			return geminiResponse{}, err
		}
		log.Warn().Err(err).Str("model", model).Int("attempt", i+1).Msg("ai: Gemini недоступен, пробую дальше")
	}
	return geminiResponse{}, err
}

func (c *GeminiClient) call(ctx context.Context, model string, body []byte) (geminiResponse, error) {
	// ключ передаём заголовком x-goog-api-key, а не query-параметром, чтобы он
	// не утёк в логи вместе с URL при сетевых/HTTP-ошибках
	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, model)
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

	// Ответ читаем с потолком: без него скомпрометированный или просто
	// сбойнувший upstream кладёт произвольный объём в кучу, а на каждый /parse
	// уже приходится до ~15 МБ медиа в памяти.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGeminiResponseBytes))
	if err != nil {
		return geminiResponse{}, fmt.Errorf("ai: чтение ответа Gemini: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return geminiResponse{}, &statusError{code: resp.StatusCode, body: truncate(string(raw), 300)}
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

// truncate режет строку для лога по рунам, а не по байтам: тела ошибок Gemini —
// JSON с кириллицей, и срез по байту разваливал бы руну пополам, оставляя в
// логе битый UTF-8 ровно тогда, когда его читают для разбора сбоя
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
