package ai

import (
	"encoding/json"
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

// Цена из ответа модели читается ТОЧНО.
//
// Модель в тусе с копейками возвращает «20.8»; обычный разбор на целом поле
// падает, а через float64 получается 2079.9999… и теряется копейка.
func TestDraftItemPriceDecoding(t *testing.T) {
	cases := []struct {
		raw       string
		wantPrice int
		wantMinor *int64
	}{
		{`{"name":"Кофе","price":20.8,"kind":"item","shares":[]}`, 21, ptrInt64(2080)},
		{`{"name":"Кофе","price":20.80,"kind":"item","shares":[]}`, 21, ptrInt64(2080)},
		{`{"name":"Кофе","price":12,"kind":"item","shares":[]}`, 12, nil},
		{`{"name":"Кофе","price":0,"kind":"item","shares":[]}`, 0, nil},
		// Строкой модель тоже иногда отвечает — читаем и это.
		{`{"name":"Кофе","price":"7.5","kind":"item","shares":[]}`, 8, ptrInt64(750)},
	}
	for _, tc := range cases {
		var item DraftItem
		if err := json.Unmarshal([]byte(tc.raw), &item); err != nil {
			t.Fatalf("разбор %s: %v", tc.raw, err)
		}
		if item.Price != tc.wantPrice {
			t.Errorf("%s: целое = %d, want %d", tc.raw, item.Price, tc.wantPrice)
		}
		switch {
		case tc.wantMinor == nil && item.PriceMinor != nil:
			t.Errorf("%s: точное поле у целой цены = %d, want nil", tc.raw, *item.PriceMinor)
		case tc.wantMinor != nil && (item.PriceMinor == nil || *item.PriceMinor != *tc.wantMinor):
			t.Errorf("%s: точное поле = %v, want %d", tc.raw, item.PriceMinor, *tc.wantMinor)
		}
	}

	// Не число — ошибка разбора, а не молчаливый ноль.
	var item DraftItem
	if err := json.Unmarshal([]byte(`{"name":"Кофе","price":"дорого","kind":"item","shares":[]}`), &item); err == nil {
		t.Error("нечисловая цена принята")
	}
}

func ptrInt64(v int64) *int64 { return &v }

// Дробная фикс-доля из ответа модели читается ТОЧНО.
//
// Схема в тусе с копейками разрешает дробное amount, а поле целое: обычный
// разбор падал на «10.5», и весь /parse отвечал 502 — терялось всё, что
// человек надиктовал.
func TestItemShareAmountDecoding(t *testing.T) {
	var item DraftItem
	raw := `{"name":"Вино","price":20.8,"kind":"item","shares":[` +
		`{"userId":1,"weight":1,"amount":10.5},{"userId":2,"weight":1,"amount":3}]}`
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("разбор упал: %v", err)
	}
	if len(item.Shares) != 2 {
		t.Fatalf("долей %d, want 2", len(item.Shares))
	}
	if item.Shares[0].AmountMinor == nil || *item.Shares[0].AmountMinor != 1050 {
		t.Errorf("точная доля = %v, want 1050", item.Shares[0].AmountMinor)
	}
	if item.Shares[0].Amount == nil || *item.Shares[0].Amount != 11 {
		t.Errorf("округлённая проекция = %v, want 11", item.Shares[0].Amount)
	}
	// Целая доля точного поля не получает: оно не несёт ничего сверх целого.
	if item.Shares[1].AmountMinor != nil {
		t.Errorf("у целой доли точное поле = %v, want nil", item.Shares[1].AmountMinor)
	}
	if item.Shares[1].Amount == nil || *item.Shares[1].Amount != 3 {
		t.Errorf("целая доля = %v, want 3", item.Shares[1].Amount)
	}

	// Не число — ошибка разбора, а не молчаливый ноль.
	var bad DraftItem
	if err := json.Unmarshal([]byte(`{"name":"Вино","price":1,"kind":"item","shares":[{"userId":1,"amount":"много"}]}`), &bad); err == nil {
		t.Error("нечисловая доля принята")
	}
}
