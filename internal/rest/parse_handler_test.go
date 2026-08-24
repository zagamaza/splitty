package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/service"
)

// fakeParser подставной Parser: возвращает заданный результат или ошибку.
type fakeParser struct {
	result ai.ParseResult
	err    error
	called bool
	gotIn  ai.ParseInput
}

func (f *fakeParser) Parse(_ context.Context, in ai.ParseInput) (ai.ParseResult, error) {
	f.called = true
	f.gotIn = in
	return f.result, f.err
}

// unitCounter отдаёт 1 на каждый инкремент — с ratePerMin=0 даёт мгновенный 429.
// Get возвращает 0: суточное окно пустое, поэтому отказ приходит именно от
// минутного лимита, а не от исчерпанной квоты.
type unitCounter struct{}

func (unitCounter) Incr(_ context.Context, _ string, _ time.Duration) (int64, error) {
	return 1, nil
}

func (unitCounter) Get(_ context.Context, _ string) (int64, error) { return 0, nil }

// exhaustedCounter имитирует израсходованную суточную норму: окно суток уже
// заполнено, минутное пустое.
type exhaustedCounter struct{ quota int64 }

func (c exhaustedCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if strings.Contains(key, ":day:") {
		return c.quota + 1, nil
	}
	return 1, nil
}

func (c exhaustedCounter) Get(_ context.Context, key string) (int64, error) {
	if strings.Contains(key, ":day:") {
		return c.quota, nil
	}
	return 0, nil
}

// собираем сервер с AI. Лимитер настраиваем на большие значения, чтобы не мешал.
func newAIServer(t *testing.T, ur *fakeUserRepo, rr *fakeRoomRepo, parser ai.Parser, limiter *service.RateLimiter) *Server {
	t.Helper()
	s := newTestServer(Config{}, ur, rr)
	s.SetAI(parser, limiter, 15<<20)
	return s
}

func multipartBody(t *testing.T, fields map[string]string, files map[string]filePart) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for field, fp := range files {
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{`form-data; name="` + field + `"; filename="` + fp.name + `"`}
		h["Content-Type"] = []string{fp.mime}
		pw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		pw.Write(fp.data)
	}
	mw.Close()
	return mw.FormDataContentType(), &buf
}

type filePart struct {
	name string
	mime string
	data []byte
}

func doParse(t *testing.T, s *Server, roomId, token, contentType string, body *bytes.Buffer) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+roomId+"/operations/parse", body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func okDraft() ai.ParseResult {
	return ai.ParseResult{Draft: ai.Draft{
		Description: "Ужин", Sum: 300,
		Items: []ai.DraftItem{{Name: "Пицца", Price: 300, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}}},
	}}
}

func TestParse_Unauthorized(t *testing.T) {
	room := newTestRoom()
	s := newAIServer(t, newFakeUserRepo(testUser1), newFakeRoomRepo(room), &fakeParser{result: okDraft()}, nil)
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), "", ct, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestParse_ForbiddenNonMember(t *testing.T) {
	room := newTestRoom() // участники 1 и 2
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room), &fakeParser{result: okDraft()}, nil)
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser3.ID), ct, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestParse_Disabled503(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room)) // без SetAI
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestParse_HappyPathText(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, nil)
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if !fp.called || fp.gotIn.Text == "" {
		t.Fatalf("parser не вызван с текстом: %+v", fp.gotIn)
	}
	var res ai.ParseResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Draft.Sum != 300 {
		t.Fatalf("неожиданный черновик: %+v", res.Draft)
	}
	// участники переданы модели
	if len(fp.gotIn.Participants) != 2 {
		t.Fatalf("ожидалось 2 участника, got %d", len(fp.gotIn.Participants))
	}
}

func TestParse_AIErrorEchoesDraft(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{err: context.DeadlineExceeded}
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, nil)
	// шлём текущий черновик — при ошибке он не должен потеряться
	draftJSON := `{"description":"мой черновик","sum":500,"items":[]}`
	ct, body := multipartBody(t, map[string]string{"text": "правка", "draft": draftJSON}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	var res ai.ParseResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Draft.Description != "мой черновик" || res.Draft.Sum != 500 {
		t.Fatalf("черновик потерян при ошибке AI: %+v", res.Draft)
	}
}

func TestParse_UnsupportedAudioMime(t *testing.T) {
	room := newTestRoom()
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), &fakeParser{result: okDraft()}, nil)
	ct, body := multipartBody(t, nil, map[string]filePart{
		"audio": {name: "voice.m4a", mime: "audio/mp4", data: []byte("xxxx")}, // mp4 не в allowlist
	})
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415, body %s", rec.Code, rec.Body.String())
	}
}

func TestParse_AudioAacInlined(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, nil)
	ct, body := multipartBody(t, nil, map[string]filePart{
		"audio": {name: "voice.aac", mime: "audio/aac", data: []byte("аудио")},
	})
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if len(fp.gotIn.Audio) == 0 || fp.gotIn.AudioMime != "audio/aac" {
		t.Fatalf("аудио не передано парсеру: %+v", fp.gotIn)
	}
}

func TestParse_AudioAndImageTogether(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, nil)
	ct, body := multipartBody(t, nil, map[string]filePart{
		"audio": {name: "v.aac", mime: "audio/aac", data: []byte("голос")},
		"image": {name: "r.jpg", mime: "image/jpeg", data: []byte("фото")},
	})
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if len(fp.gotIn.Audio) == 0 || len(fp.gotIn.Image) == 0 {
		t.Fatalf("оба медиа должны дойти до парсера: audio=%d image=%d", len(fp.gotIn.Audio), len(fp.gotIn.Image))
	}
}

func TestParse_HugeTextRejected(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, nil)
	huge := make([]byte, maxTextBytes+1)
	for i := range huge {
		huge[i] = 'a'
	}
	ct, body := multipartBody(t, map[string]string{"text": string(huge)}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (текст слишком длинный)", rec.Code)
	}
	if fp.called {
		t.Fatal("parser не должен вызываться на слишком длинном тексте")
	}
}

func TestParse_NoInput400(t *testing.T) {
	room := newTestRoom()
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), &fakeParser{result: okDraft()}, nil)
	ct, body := multipartBody(t, map[string]string{}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestParse_RateLimited429(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	limiter := service.NewRateLimiter(unitCounter{}, 0) // ratePerMin=0 → сразу отказ
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, limiter)
	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if fp.called {
		t.Fatal("parser не должен вызываться при превышении лимита")
	}
}

// TestParseDailyQuotaExceededOpensPaywall — исчерпанная суточная норма отдаёт
// ОТДЕЛЬНЫЙ код, по которому клиент открывает экран оплаты.
//
// Пока причина отказа была одна на оба лимита, упёршийся в суточную норму
// получал то же «слишком часто», что и человек, тыкнувший микрофон дважды
// подряд, — и заплатить ему никто не предлагал.
func TestParseDailyQuotaExceededOpensPaywall(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	limiter := service.NewRateLimiter(exhaustedCounter{quota: 5}, 100)
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, limiter)
	s.SetEntitlements(service.NewEntitlements(&stubSubs{}, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 5,
	}))

	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if fp.called {
		t.Fatal("parser не должен вызываться при исчерпанной квоте")
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("тело не разбирается моделью клиента: %v (%s)", err, rec.Body.String())
	}
	if env.Error.Code != errCodeAiQuotaExceeded {
		t.Errorf("code = %q, want %q — иначе клиент покажет тост вместо paywall", env.Error.Code, errCodeAiQuotaExceeded)
	}
	if env.Error.Message == "" {
		t.Error("пустой message: сборки 1.6 показывают именно его, у них нет строки для нового кода")
	}
}

// TestParseMinuteThrottleKeepsOldCode — минутный троттл по-прежнему
// rate_limited: на него paywall показывать нельзя.
func TestParseMinuteThrottleKeepsOldCode(t *testing.T) {
	room := newTestRoom()
	fp := &fakeParser{result: okDraft()}
	limiter := service.NewRateLimiter(unitCounter{}, 0)
	s := newAIServer(t, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room), fp, limiter)
	s.SetEntitlements(service.NewEntitlements(&stubSubs{}, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 5,
	}))

	ct, body := multipartBody(t, map[string]string{"text": "пицца 300"}, nil)
	rec := doParse(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), ct, body)

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if env.Error.Code != errCodeRateLimited {
		t.Errorf("code = %q, want %q", env.Error.Code, errCodeRateLimited)
	}
}
