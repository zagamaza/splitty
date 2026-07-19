package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeTelegram мок telegram bot api для аватаров: getUserProfilePhotos,
// getFile и скачивание файла. Считает обращения — для проверки кеша.
func fakeTelegram(t *testing.T, hasPhoto bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/bottest-token/getUserProfilePhotos", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !hasPhoto {
			fmt.Fprint(w, `{"ok":true,"result":{"total_count":0,"photos":[]}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"total_count":1,"photos":[[
			{"file_id":"small","width":160,"height":160},
			{"file_id":"medium","width":320,"height":320},
			{"file_id":"big","width":800,"height":800}
		]]}}`)
	})
	mux.HandleFunc("/bottest-token/getFile", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("file_id") != "medium" {
			t.Errorf("ожидали файл medium (крупнейший ≤640), got %s", r.URL.Query().Get("file_id"))
		}
		fmt.Fprint(w, `{"ok":true,"result":{"file_path":"photos/medium.jpg"}}`)
	})
	mux.HandleFunc("/file/bottest-token/photos/medium.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	})
	return httptest.NewServer(mux)
}

func TestGetUserAvatarProxiesAndCaches(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, true, &calls)
	defer tg.Close()

	userRepo := newFakeUserRepo(testUser1, testUser2)
	s := newTestServer(Config{TgToken: "test-token"}, userRepo, newFakeRoomRepo(sharedRoom()))
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/2/avatar", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type = %q", ct)
	}

	// Повторный запрос — из кеша, без похода в telegram.
	rec = doRequest(t, s, "GET", "/api/v1/users/2/avatar", token, "")
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("cached: status %d body %q", rec.Code, rec.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("telegram calls = %d, ожидали 1 (кеш)", calls.Load())
	}
}

func TestGetUserAvatarNoPhotoCaches404(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, false, &calls)
	defer tg.Close()

	userRepo := newFakeUserRepo(testUser1, testUser2)
	s := newTestServer(Config{TgToken: "test-token"}, userRepo, newFakeRoomRepo(sharedRoom()))
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	for i := 0; i < 2; i++ {
		rec := doRequest(t, s, "GET", "/api/v1/users/2/avatar", token, "")
		assertErrorCode(t, rec, http.StatusNotFound, "not_found")
	}
	if calls.Load() != 1 {
		t.Fatalf("telegram calls = %d, ожидали 1 (негативный кеш)", calls.Load())
	}
}

func TestGetUserAvatarRequiresAuth(t *testing.T) {
	s := newTestServer(Config{TgToken: "test-token"}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	rec := doRequest(t, s, "GET", "/api/v1/users/2/avatar", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ожидали 401", rec.Code)
	}
}

// TestGetUserAvatarForbidsStranger — токен бота даёт доступ к фото любого
// собеседника бота, поэтому чужие аватары без общей комнаты отдавать нельзя.
func TestGetUserAvatarForbidsStranger(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, true, &calls)
	defer tg.Close()

	// testUser3 не состоит в комнате с testUser1
	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(sharedRoom()))
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/3/avatar", token, "")
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
	if calls.Load() != 0 {
		t.Fatalf("telegram calls = %d, ожидали 0 (запрос не должен дойти до telegram)", calls.Load())
	}
}

// TestGetUserAvatarAllowsSelf — своё фото доступно и без общей комнаты.
func TestGetUserAvatarAllowsSelf(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, true, &calls)
	defer tg.Close()

	s := newTestServer(Config{TgToken: "test-token"}, newFakeUserRepo(testUser1), newFakeRoomRepo())
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/1/avatar", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
}
