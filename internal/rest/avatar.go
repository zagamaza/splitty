package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

// avatarCacheTTL как долго держим аватар (и факт его отсутствия) в памяти:
// фото профиля меняются редко, а каждый промах — два запроса к telegram
const avatarCacheTTL = 24 * time.Hour

// maxAvatarBytes — потолок на скачиваемое фото профиля (реальные превью
// telegram — десятки килобайт).
const maxAvatarBytes = 2 << 20

// allowedAvatarTypes — типы, которые безопасно отдавать со своего origin.
var allowedAvatarTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// allowedInlineTypes — вложения операций, которые можно показывать инлайн;
// всё прочее уходит как octet-stream с Content-Disposition: attachment.
var allowedInlineTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"image/heic":      true,
	"video/mp4":       true,
	"audio/mpeg":      true,
	"audio/ogg":       true,
	"application/pdf": true,
}

// avatarMaxSide предпочтительная сторона фото: среди размеров telegram
// берём самый крупный, не превышающий этот порог (хватает для списков)
const avatarMaxSide = 640

type avatarCacheEntry struct {
	data        []byte
	contentType string
	found       bool
	fetchedAt   time.Time
}

// maxAvatarCacheBytes — бюджет памяти под кеш аватаров. Одной только TTL мало:
// в пределах суток кеш держит по maxAvatarBytes на каждого запрошенного
// пользователя без ограничения их числа, а /users/{id}/avatar ничем не
// throttled — комнат у активного пользователя много, и кеш рос бы линейно
const maxAvatarCacheBytes = 64 << 20

// avatarCache потокобезопасный in-memory кеш аватаров по telegram user id;
// негативные ответы («у пользователя нет фото») тоже кешируются
type avatarCache struct {
	mu      sync.Mutex
	entries map[int]avatarCacheEntry
	// bytes — суммарный размер тел в entries, поддерживается put'ом
	bytes int
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

// put кладёт запись, вычищает протухшие и держит кеш в пределах бюджета
// maxAvatarCacheBytes, вытесняя самые старые. Без вытеснения get считал
// протухшее промахом, но из map ничего не удалял: кеш рос на каждого нового
// пользователя и держал jpeg-и до конца жизни процесса.
func (c *avatarCache) put(userId int, e avatarCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int]avatarCacheEntry{}
	}
	for id, old := range c.entries {
		if e.fetchedAt.Sub(old.fetchedAt) > avatarCacheTTL {
			c.evictLocked(id)
		}
	}
	c.evictLocked(userId) // перезапись: старое тело из счётчика убираем
	c.entries[userId] = e
	c.bytes += len(e.data)

	// бюджет исчерпан — выбрасываем самые старые записи, пока не уложимся.
	// Только что положенную не трогаем: она и есть самая свежая
	for c.bytes > maxAvatarCacheBytes && len(c.entries) > 1 {
		oldestId, oldestAt := 0, time.Time{}
		for id, old := range c.entries {
			if oldestAt.IsZero() || old.fetchedAt.Before(oldestAt) {
				oldestId, oldestAt = id, old.fetchedAt
			}
		}
		c.evictLocked(oldestId)
	}
}

// evictLocked удаляет запись, поддерживая счётчик байт. Вызывается под c.mu
func (c *avatarCache) evictLocked(userId int) {
	if e, ok := c.entries[userId]; ok {
		c.bytes -= len(e.data)
		delete(c.entries, userId)
	}
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
//
// {userId} — НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY, а не telegram id: в telegram уходит
// user.TelegramID канонического документа. Пользователю без telegram отдаём
// 404 — клиенты рисуют инициалы.
func (s *Server) handleGetUserAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
		return
	}
	// Токен бота даёт доступ к фото ЛЮБОГО собеседника бота — отдаём только
	// себя и тех, с кем у вызывающего есть общая комната (как в handleAddAlias).
	callerId := userIdFromCtx(ctx)
	if callerId != userId && !s.shareRoom(ctx, callerId, userId) {
		writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
		return
	}
	if s.cfg.TgToken == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telegram-бот не сконфигурирован")
		return
	}

	// ключ кеша — номер Splitty: привязка/отвязка telegram не должна путать
	// закешированные картинки разных пользователей между собой
	if e, ok := s.avatars.get(userId, s.now()); ok {
		writeAvatar(w, e)
		return
	}

	// telegram id берём из КАНОНИЧЕСКОГО документа: _id давно не равен
	// telegram id, а у google/apple-пользователей его нет вовсе
	user, err := s.userRepo.FindById(ctx, userId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
			return
		}
		log.Error().Err(err).Msg("cannot find avatar owner")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить фото профиля")
		return
	}
	if !user.HasTelegram() {
		writeError(w, http.StatusNotFound, "not_found", "у пользователя нет фото профиля")
		return
	}

	photosURL := fmt.Sprintf("%s/bot%s/getUserProfilePhotos?user_id=%d&limit=1",
		s.tgApiURL, s.cfg.TgToken, *user.TelegramID)
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

	// file_id — значение из ответа telegram, но в query оно всё равно уходит
	// экранированным (как в handleGetOperationFile): подставлять сырым нельзя
	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", s.tgApiURL, s.cfg.TgToken, url.QueryEscape(fileId))
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

	// потолок на тело: telegram отдаёт превью профиля, но полагаться на это
	// нельзя — без лимита ответ произвольного размера уходил целиком в память
	data, err := io.ReadAll(io.LimitReader(download.Body, maxAvatarBytes))
	if err != nil {
		log.Error().Err(err).Msg("telegram avatar read failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать фото профиля")
		return
	}

	// Content-Type приходит извне и отдаётся нашим origin: любой не-image тип
	// (например text/html) означал бы хранимую XSS на домене API
	contentType := download.Header.Get("Content-Type")
	if !allowedAvatarTypes[contentType] {
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(e.data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(e.data); err != nil {
		log.Error().Err(err).Msg("cannot write avatar")
	}
}
