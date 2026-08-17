package rest

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// pngBytes — настоящая картинка, а не произвольные байты: сервер определяет тип
// по сигнатуре, и подделка заголовком тут не проходит.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

// avatarBody собирает multipart-тело с полем image.
func avatarBody(t *testing.T, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="image"; filename="a.png"`}
	h["Content-Type"] = []string{contentType}
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("multipart write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return &body, w.FormDataContentType()
}

// bigJpeg — шумная картинка side×side: шум не сжимается, поэтому тело
// гарантированно перевалит за мегабайт.
func bigJpeg(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	seed := 1
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			seed = seed*1103515245 + 12345
			v := uint8(seed >> 16)
			img.Set(x, y, color.RGBA{R: v, G: v << 1, B: v >> 1, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg: %v", err)
	}
	if buf.Len() < 1<<20 {
		t.Fatalf("картинка вышла %d байт — меньше мегабайта, тест ничего не проверит", buf.Len())
	}
	return buf.Bytes()
}

func doAvatarUpload(t *testing.T, s *Server, roomId, token, contentType string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := avatarBody(t, contentType, data)
	req := httptest.NewRequest("PUT", "/api/v1/rooms/"+roomId+"/avatar", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func avatarServer(t *testing.T, room *api.Room) (*Server, *fakeFileStore) {
	t.Helper()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	store := newFakeFileStore()
	s.SetFiles(store)
	return s, store
}

func TestSetRoomAvatar(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)

	data := pngBytes(t)
	rec := doAvatarUpload(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "image/png", data)
	assertStatus(t, rec, 200)

	var resp roomAvatarDto
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if resp.AvatarFileId == "" {
		t.Fatal("ответ без id — клиенту нечего показать до перезагрузки списка")
	}
	if room.AvatarFileId == nil || *room.AvatarFileId != resp.AvatarFileId {
		t.Fatalf("комната не ссылается на файл: %v", room.AvatarFileId)
	}

	stored, _ := store.Get(t.Context(), resp.AvatarFileId)
	if stored == nil {
		t.Fatal("файл не сохранён")
	}
	if !bytes.Equal(stored.Data, data) {
		t.Error("сохранены не те байты")
	}
	if stored.Kind != api.StoredFileRoomAvatar || stored.OwnerId != testUser1.ID || stored.RoomId != room.ID {
		t.Errorf("метаданные файла: %+v", stored)
	}

	// Загруженная ава видна в списке групп — иначе клиент узнал бы о ней только
	// из ответа на загрузку.
	list := doRequest(t, s, "GET", "/api/v1/rooms", mustToken(t, s, testUser1.ID), "")
	assertStatus(t, list, 200)
	var rooms []roomSummaryDto
	if err := json.Unmarshal(list.Body.Bytes(), &rooms); err != nil {
		t.Fatalf("список комнат: %v", err)
	}
	if len(rooms) != 1 || rooms[0].AvatarFileId != resp.AvatarFileId {
		t.Errorf("в списке групп нет ссылки на аву: %+v", rooms)
	}
}

// Замена авы не копит мусор: прежние байты уходят из базы.
func TestReplaceRoomAvatarDropsPrevious(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)
	token := mustToken(t, s, testUser1.ID)

	first := doAvatarUpload(t, s, room.ID.Hex(), token, "image/png", pngBytes(t))
	assertStatus(t, first, 200)
	var firstResp roomAvatarDto
	_ = json.Unmarshal(first.Body.Bytes(), &firstResp)

	second := doAvatarUpload(t, s, room.ID.Hex(), token, "image/png", pngBytes(t))
	assertStatus(t, second, 200)

	if store.count() != 1 {
		t.Errorf("в хранилище %d файлов, ожидался один — старая ава не удалена", store.count())
	}
	if got, _ := store.Get(t.Context(), firstResp.AvatarFileId); got != nil {
		t.Error("прежняя ава осталась в базе")
	}
}

func TestDeleteRoomAvatar(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)
	token := mustToken(t, s, testUser1.ID)

	assertStatus(t, doAvatarUpload(t, s, room.ID.Hex(), token, "image/png", pngBytes(t)), 200)

	rec := doRequest(t, s, "DELETE", "/api/v1/rooms/"+room.ID.Hex()+"/avatar", token, "")
	assertStatus(t, rec, 204)

	if room.AvatarFileId != nil {
		t.Errorf("ссылка осталась: %v", *room.AvatarFileId)
	}
	if store.count() != 0 {
		t.Errorf("байты остались в базе: %d файлов", store.count())
	}

	// Повтор ничего не ломает: снятие идемпотентно.
	assertStatus(t, doRequest(t, s, "DELETE", "/api/v1/rooms/"+room.ID.Hex()+"/avatar", token, ""), 204)
}

// Не участник комнаты не может ни поставить аву, ни снять её.
func TestRoomAvatarForbiddenForOutsider(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)
	token := mustToken(t, s, testUser3.ID)

	assertErrorCode(t, doAvatarUpload(t, s, room.ID.Hex(), token, "image/png", pngBytes(t)), 403, "forbidden")
	if store.count() != 0 {
		t.Error("файл чужака всё-таки сохранился")
	}
	assertErrorCode(t, doRequest(t, s, "DELETE", "/api/v1/rooms/"+room.ID.Hex()+"/avatar", token, ""), 403, "forbidden")
}

// Заголовок Content-Type пишет клиент, поэтому тип проверяется по байтам:
// «image/png» поверх текста не проходит.
func TestSetRoomAvatarRejectsNonImage(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)

	rec := doAvatarUpload(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "image/png", []byte("<html>это не картинка</html>"))
	assertErrorCode(t, rec, 415, "unsupported_media")
	if store.count() != 0 {
		t.Error("не-картинка сохранилась")
	}
	if room.AvatarFileId != nil {
		t.Error("комната сослалась на не-картинку")
	}
}

// Ава крупнее 1 МБ обязана проходить: документированный лимит — 5 МБ. Общий
// потолок тела (1 МБ) стоит middleware, и повторный MaxBytesReader в хендлере
// его НЕ снимает — маршрут должен быть из него исключён.
func TestSetRoomAvatarAllowsLargeImage(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)

	rec := doAvatarUpload(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "image/jpeg", bigJpeg(t, 1600))
	assertStatus(t, rec, 200)
	if store.count() != 1 {
		t.Error("файл не сохранён")
	}
}

// Картинка-бомба: тело крошечное, а развернётся в гигабайты пикселей и положит
// приложение остальных участников. Размера тела для защиты мало — нужен разбор
// заголовка картинки.
func TestSetRoomAvatarRejectsHugeDimensions(t *testing.T) {
	room := newTestRoom()
	s, store := avatarServer(t, room)

	// Одноцветный PNG 12000×12000 сжимается в считаные килобайты.
	img := image.NewRGBA(image.Rect(0, 0, 12000, 12000))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	if buf.Len() > maxAvatarFileBytes {
		t.Fatalf("бомба вышла на %d байт — её отсечёт лимит размера, тест ничего не проверит", buf.Len())
	}

	rec := doAvatarUpload(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "image/png", buf.Bytes())
	assertErrorCode(t, rec, 413, "too_large")
	if store.count() != 0 {
		t.Error("бомба сохранилась")
	}
}

// Без хранилища эндпоинт честно отвечает 503, а не падает.
func TestSetRoomAvatarWithoutStore(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))

	rec := doAvatarUpload(t, s, room.ID.Hex(), mustToken(t, s, testUser1.ID), "image/png", pngBytes(t))
	assertErrorCode(t, rec, 503, "unavailable")
}
