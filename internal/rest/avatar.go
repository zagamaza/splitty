package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// avatarCacheTTL как долго держим аватар (и факт его отсутствия) в памяти:
// фото профиля меняются редко, а каждый промах — два запроса к telegram
const avatarCacheTTL = 24 * time.Hour

// avatarMaxSide предпочтительная сторона фото: среди размеров telegram
// берём самый крупный, не превышающий этот порог (хватает для списков)
const avatarMaxSide = 640

type avatarCacheEntry struct {
	data        []byte
	contentType string
	found       bool
	fetchedAt   time.Time
}

// avatarCache потокобезопасный in-memory кеш аватаров по telegram user id;
// негативные ответы («у пользователя нет фото») тоже кешируются
type avatarCache struct {
	mu      sync.Mutex
	entries map[int]avatarCacheEntry
}

func (c *avatarCache) get(userId int, now time.Time) (avatarCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[userId]
	if !ok || now.Sub(e.fetchedAt) > avatarCacheTTL {
		return avatarCacheEntry{}, false
	}
	return e, true
}

func (c *avatarCache) put(userId int, e avatarCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int]avatarCacheEntry{}
	}
	c.entries[userId] = e
}

type tgUserProfilePhotosResponse struct {
	Ok     bool `json:"ok"`
	Result struct {
		TotalCount int `json:"total_count"`
		Photos     [][]struct {
			FileId string `json:"file_id"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"photos"`
	} `json:"result"`
}

// handleGetUserAvatar GET /api/v1/users/{userId}/avatar — фото профиля
// Telegram (через getUserProfilePhotos + getFile), 404 если фото нет или
// скрыто настройками приватности. Ответ кешируется в памяти на сутки.
func (s *Server) handleGetUserAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
		return
	}
	if s.cfg.TgToken == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telegram-бот не сконфигурирован")
		return
	}

	if e, ok := s.avatars.get(userId, s.now()); ok {
		writeAvatar(w, e)
		return
	}

	photosURL := fmt.Sprintf("%s/bot%s/getUserProfilePhotos?user_id=%d&limit=1",
		s.tgApiURL, s.cfg.TgToken, userId)
	resp, err := s.tgGet(ctx, photosURL)
	if err != nil {
		log.Error().Err(err).Msg("telegram getUserProfilePhotos failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить фото профиля")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var photos tgUserProfilePhotosResponse
	if err := json.NewDecoder(resp.Body).Decode(&photos); err != nil || !photos.Ok {
		log.Error().Err(err).Msgf("telegram getUserProfilePhotos bad response, status %d", resp.StatusCode)
		writeError(w, http.StatusNotFound, "not_found", "фото профиля недоступно")
		return
	}
	if len(photos.Result.Photos) == 0 || len(photos.Result.Photos[0]) == 0 {
		// нет фото — кешируем и 404, чтобы не долбить telegram на каждый список
		s.avatars.put(userId, avatarCacheEntry{found: false, fetchedAt: s.now()})
		writeError(w, http.StatusNotFound, "not_found", "у пользователя нет фото профиля")
		return
	}

	// размеры отсортированы по возрастанию: берём самый крупный ≤ avatarMaxSide
	sizes := photos.Result.Photos[0]
	fileId := sizes[0].FileId
	for _, size := range sizes {
		if size.Width <= avatarMaxSide {
			fileId = size.FileId
		}
	}

	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", s.tgApiURL, s.cfg.TgToken, fileId)
	fileMeta, err := s.tgGet(ctx, getFileURL)
	if err != nil {
		log.Error().Err(err).Msg("telegram getFile (avatar) failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить фото профиля")
		return
	}
	defer func() { _ = fileMeta.Body.Close() }()

	var fileResp tgFileResponse
	if err := json.NewDecoder(fileMeta.Body).Decode(&fileResp); err != nil || !fileResp.Ok || fileResp.Result.FilePath == "" {
		log.Error().Err(err).Msgf("telegram getFile (avatar) bad response, status %d", fileMeta.StatusCode)
		writeError(w, http.StatusNotFound, "not_found", "фото профиля недоступно")
		return
	}

	downloadURL := fmt.Sprintf("%s/file/bot%s/%s", s.tgApiURL, s.cfg.TgToken, fileResp.Result.FilePath)
	download, err := s.tgGet(ctx, downloadURL)
	if err != nil {
		log.Error().Err(err).Msg("telegram avatar download failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать фото профиля")
		return
	}
	defer func() { _ = download.Body.Close() }()
	if download.StatusCode != http.StatusOK {
		log.Error().Msgf("telegram avatar download status %d", download.StatusCode)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать фото профиля")
		return
	}

	data, err := io.ReadAll(download.Body)
	if err != nil {
		log.Error().Err(err).Msg("telegram avatar read failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать фото профиля")
		return
	}

	contentType := download.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "image/jpeg" // фото профиля telegram — всегда jpeg
	}
	entry := avatarCacheEntry{data: data, contentType: contentType, found: true, fetchedAt: s.now()}
	s.avatars.put(userId, entry)
	writeAvatar(w, entry)
}

func writeAvatar(w http.ResponseWriter, e avatarCacheEntry) {
	if !e.found {
		writeError(w, http.StatusNotFound, "not_found", "у пользователя нет фото профиля")
		return
	}
	w.Header().Set("Content-Type", e.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(e.data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(e.data); err != nil {
		log.Error().Err(err).Msg("cannot write avatar")
	}
}
