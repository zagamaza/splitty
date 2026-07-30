package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// withTelegram копия пользователя с привязанным telegram id. Номер Splitty
// (_id) и telegram id намеренно разные — иначе тест не отличит одно от другого
func withTelegram(u api.User, tgID int) api.User {
	u.TelegramID = &tgID
	return u
}

// tgIDOf telegram id, который фикстуры дают пользователю с номером Splitty id
func tgIDOf(id int) int { return 900_000_000 + id }

// fakeTelegram мок telegram bot api для аватаров: getUserProfilePhotos,
// getFile и скачивание файла. Считает обращения — для проверки кеша.
// wantUserID (если не 0) — telegram id, который обязан прийти в user_id:
// в telegram уходит TelegramID, а не номер Splitty.
func fakeTelegram(t *testing.T, hasPhoto bool, calls *atomic.Int32, wantUserID int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/bottest-token/getUserProfilePhotos", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.URL.Query().Get("user_id"); wantUserID != 0 && got != strconv.Itoa(wantUserID) {
			t.Errorf("user_id = %s, ожидали telegram id %d", got, wantUserID)
		}
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
	tg := fakeTelegram(t, true, &calls, tgIDOf(testUser2.ID))
	defer tg.Close()

	userRepo := newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID)),
		withTelegram(testUser2, tgIDOf(testUser2.ID)))
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
	tg := fakeTelegram(t, false, &calls, tgIDOf(testUser2.ID))
	defer tg.Close()

	userRepo := newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID)),
		withTelegram(testUser2, tgIDOf(testUser2.ID)))
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
	tg := fakeTelegram(t, true, &calls, 0)
	defer tg.Close()

	// testUser3 не состоит в комнате с testUser1
	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID)),
			withTelegram(testUser2, tgIDOf(testUser2.ID)),
			withTelegram(testUser3, tgIDOf(testUser3.ID))), newFakeRoomRepo(sharedRoom()))
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
	tg := fakeTelegram(t, true, &calls, tgIDOf(testUser1.ID))
	defer tg.Close()

	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID))), newFakeRoomRepo())
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/1/avatar", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestGetUserAvatarUsesTelegramID номер Splitty и telegram id разошлись:
// в telegram обязан уйти telegram_id канонического документа, а ключом кеша
// остаётся номер Splitty (иначе привязка/отвязка перемешала бы картинки)
func TestGetUserAvatarUsesTelegramID(t *testing.T) {
	var calls atomic.Int32
	// google-первый пользователь: _id ≥ 10^12, telegram привязан позже
	splittyID := 1_000_000_000_007
	tgID := 987_654_321
	tg := fakeTelegram(t, true, &calls, tgID)
	defer tg.Close()

	me := api.User{ID: splittyID, Username: "gsu", DisplayName: "Гугл"}
	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(withTelegram(me, tgID)), newFakeRoomRepo())
	s.tgApiURL = tg.URL
	token := mustToken(t, s, splittyID)

	rec := doRequest(t, s, "GET", fmt.Sprintf("/api/v1/users/%d/avatar", splittyID), token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	// ключ кеша — номер Splitty: повторный запрос по нему не идёт в telegram
	rec = doRequest(t, s, "GET", fmt.Sprintf("/api/v1/users/%d/avatar", splittyID), token, "")
	if rec.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("cached: status %d, telegram calls %d (ожидали 1)", rec.Code, calls.Load())
	}
	if _, ok := s.avatars.get(splittyID, s.now()); !ok {
		t.Fatalf("кеш должен быть по номеру Splitty %d", splittyID)
	}
	if _, ok := s.avatars.get(tgID, s.now()); ok {
		t.Fatalf("кеш не должен быть по telegram id %d", tgID)
	}
}

// TestGetUserAvatarNoTelegram404 у пользователя без telegram фото взять
// неоткуда: отдаём 404 (клиенты рисуют инициалы), а не 500/503
func TestGetUserAvatarNoTelegram404(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, true, &calls, 0)
	defer tg.Close()

	// testUser2 — google-пользователь без telegram
	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID)), testUser2),
		newFakeRoomRepo(sharedRoom()))
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/2/avatar", token, "")
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
	if calls.Load() != 0 {
		t.Fatalf("telegram calls = %d, ожидали 0 (без telegram запрос не отправляется)", calls.Load())
	}
}

// TestGetUserAvatarUnknownUser404 пользователя нет в коллекции user —
// например, снимок в комнате пережил удаление аккаунта
func TestGetUserAvatarUnknownUser404(t *testing.T) {
	var calls atomic.Int32
	tg := fakeTelegram(t, true, &calls, 0)
	defer tg.Close()

	room := &api.Room{ID: sharedRoom().ID, Name: "Комната",
		Members: &[]api.User{testUser1, {ID: 999, DisplayName: "Призрак"}}}
	s := newTestServer(Config{TgToken: "test-token"},
		newFakeUserRepo(withTelegram(testUser1, tgIDOf(testUser1.ID))), newFakeRoomRepo(room))
	s.tgApiURL = tg.URL
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, "GET", "/api/v1/users/999/avatar", token, "")
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
	if calls.Load() != 0 {
		t.Fatalf("telegram calls = %d, ожидали 0", calls.Load())
	}
}
