package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeFileStore in-memory хранилище картинок для тестов.
type fakeFileStore struct {
	mu    sync.Mutex
	files map[string]*api.StoredFile
}

func newFakeFileStore() *fakeFileStore {
	return &fakeFileStore{files: map[string]*api.StoredFile{}}
}

func (f *fakeFileStore) Save(_ context.Context, file *api.StoredFile) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := primitive.NewObjectID().Hex()
	stored := *file
	stored.Size = len(file.Data)
	f.files[id] = &stored
	return id, nil
}

func (f *fakeFileStore) Get(_ context.Context, id string) (*api.StoredFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[id]
	if !ok {
		return nil, nil
	}
	copied := *file
	return &copied, nil
}

func (f *fakeFileStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, id)
	return nil
}

func (f *fakeFileStore) DeleteByRoom(_ context.Context, roomId string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, file := range f.files {
		if file.RoomId.Hex() == roomId {
			delete(f.files, id)
		}
	}
	return nil
}

func (f *fakeFileStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.files)
}

// seedAvatar кладёт картинку комнаты в хранилище и возвращает её id.
func seedAvatar(t *testing.T, store *fakeFileStore, room *api.Room, data []byte) string {
	t.Helper()
	id, err := store.Save(context.Background(), &api.StoredFile{
		RoomId:  room.ID,
		OwnerId: testUser1.ID,
		Kind:    api.StoredFileRoomAvatar,
		Mime:    "image/jpeg",
		Data:    data,
	})
	if err != nil {
		t.Fatalf("не удалось засеять файл: %v", err)
	}
	return id
}

// Картинка из своего хранилища отдаётся без похода в телеграм: TgToken пуст,
// и если бы обработчик пошёл прежним путём, ответ был бы 503.
func TestGetStoredFileServedFromMongo(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	store := newFakeFileStore()
	s.SetFiles(store)

	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}
	id := seedAvatar(t, store, room, data)

	rec := doRequest(t, s, "GET", "/api/v1/files/"+id, mustToken(t, s, testUser1.ID), "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, тело: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); string(got) != string(data) {
		t.Errorf("отдали не те байты: %v", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	// nosniff обязателен: файл кладёт участник комнаты, а отдаём мы его со
	// своего origin.
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("нет X-Content-Type-Options: nosniff")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "private") || !strings.Contains(cc, "max-age=") {
		t.Errorf("Cache-Control = %q — ава неизменяема и обязана кешироваться", cc)
	}
}

// Чужой комнате — 403, даже если id угадали.
func TestGetStoredFileForbiddenForOutsider(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	store := newFakeFileStore()
	s.SetFiles(store)

	id := seedAvatar(t, store, room, []byte{1, 2, 3})

	rec := doRequest(t, s, "GET", "/api/v1/files/"+id, mustToken(t, s, testUser3.ID), "")
	assertErrorCode(t, rec, 403, "forbidden")
}

// Неизвестный id уходит на телеграмный путь: без токена бота это 503, и
// именно этим отличается «нет в mongo» от «нет доступа».
func TestGetUnknownFileFallsBackToTelegram(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))
	s.SetFiles(newFakeFileStore())

	rec := doRequest(t, s, "GET", "/api/v1/files/AgACAgIAAxkBAAI", mustToken(t, s, testUser1.ID), "")
	assertErrorCode(t, rec, 503, "unavailable")
}

// Хранилище не подключено — сервер работает как раньше.
func TestGetFileWithoutStoreKeepsTelegramPath(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))

	rec := doRequest(t, s, "GET", "/api/v1/files/whatever", mustToken(t, s, testUser1.ID), "")
	assertErrorCode(t, rec, 503, "unavailable")
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d, тело: %s", rec.Code, want, rec.Body.String())
	}
}
