package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// покусочные лимиты частей multipart (общий предел тела — s.aiMaxBody)
const (
	maxAudioBytes = 3 << 20  // 3 МБ
	maxImageBytes = 8 << 20  // 8 МБ
	maxDraftBytes = 64 << 10 // 64 КБ
)

var (
	allowedAudioMime = map[string]bool{
		"audio/aac": true, "audio/mpeg": true, "audio/mp3": true,
		"audio/wav": true, "audio/x-wav": true, "audio/ogg": true, "audio/flac": true,
	}
	allowedImageMime = map[string]bool{
		"image/jpeg": true, "image/png": true, "image/webp": true, "image/heic": true,
	}
)

// handleParseOperation POST /api/v1/rooms/{roomId}/operations/parse
// Распознаёт расход из аудио/фото/текста в черновик. Ничего не создаёт и не
// хранит: чистая функция «черновик + ввод → черновик». Порядок защит: auth
// (middleware) → членство в комнате → rate-limit → лимит тела → multipart.
func (s *Server) handleParseOperation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	if s.aiParser == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_disabled", "распознавание недоступно")
		return
	}

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	// rate-limit ДО чтения больших частей и вызова Gemini
	if s.rateLimiter != nil {
		ok, reason, err := s.rateLimiter.AllowParse(ctx, userId)
		if err != nil {
			log.Error().Err(err).Msg("rate limit check failed")
			writeError(w, http.StatusInternalServerError, "internal", "ошибка проверки лимита")
			return
		}
		if !ok {
			writeError(w, http.StatusTooManyRequests, "rate_limited", reason)
			return
		}
	}

	// общий предел тела (взамен глобального 1 МБ, снятого в middleware для /parse)
	r.Body = http.MaxBytesReader(w, r.Body, s.aiMaxBody)
	in, hErr := parseMultipartInput(r)
	if hErr != nil {
		hErr.write(w)
		return
	}

	in.Participants = s.buildParticipants(ctx, room)
	in.Currency = room.Currency

	res, err := s.aiParser.Parse(ctx, in)
	if err != nil {
		log.Error().Err(err).Msg("ai parse failed")
		// не теряем то, что пользователь уже надиктовал: эхо входного черновика
		echo := ai.Draft{}
		if in.Draft != nil {
			echo = *in.Draft
		}
		writeJSON(w, http.StatusBadGateway, ai.ParseResult{
			Draft:     echo,
			Questions: []string{"не удалось распознать, попробуйте ещё раз"},
		})
		return
	}

	res.Draft = sanitizeDraft(res.Draft, roomMembers(room))
	writeJSON(w, http.StatusOK, res)
}

// parseMultipartInput разбирает multipart-тело в ai.ParseInput с покусочными
// лимитами и allowlist Content-Type. Приоритет медиа: audio → image → text.
func parseMultipartInput(r *http.Request) (ai.ParseInput, *httpError) {
	if err := r.ParseMultipartForm(maxAudioBytes + maxImageBytes); err != nil {
		return ai.ParseInput{}, &httpError{http.StatusRequestEntityTooLarge, "too_large", "тело запроса слишком большое"}
	}

	var in ai.ParseInput

	// текущий черновик (для правки)
	if raw := r.FormValue("draft"); raw != "" {
		if len(raw) > maxDraftBytes {
			return ai.ParseInput{}, &httpError{http.StatusRequestEntityTooLarge, "too_large", "черновик слишком большой"}
		}
		var d ai.Draft
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return ai.ParseInput{}, &httpError{http.StatusBadRequest, "validation", "невалидный черновик"}
		}
		in.Draft = &d
	}

	if data, mime, hErr := readFilePart(r, "audio", maxAudioBytes, allowedAudioMime); hErr != nil {
		return ai.ParseInput{}, hErr
	} else if data != nil {
		in.Media, in.Data, in.Mime = ai.MediaAudio, data, mime
		return in, nil
	}

	if data, mime, hErr := readFilePart(r, "image", maxImageBytes, allowedImageMime); hErr != nil {
		return ai.ParseInput{}, hErr
	} else if data != nil {
		in.Media, in.Data, in.Mime = ai.MediaImage, data, mime
		return in, nil
	}

	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		in.Media, in.Text = ai.MediaText, text
		return in, nil
	}

	return ai.ParseInput{}, &httpError{http.StatusBadRequest, "validation", "нужно передать audio, image или text"}
}

// readFilePart читает файловую часть с лимитом размера и проверкой mime.
// Возвращает (nil, "", nil), если части нет.
func readFilePart(r *http.Request, field string, limit int64, allowed map[string]bool) ([]byte, string, *httpError) {
	file, hdr, err := r.FormFile(field)
	if err != nil {
		return nil, "", nil // части нет — не ошибка
	}
	defer file.Close()

	if hdr.Size > limit {
		return nil, "", &httpError{http.StatusRequestEntityTooLarge, "too_large", field + ": файл слишком большой"}
	}
	mime := hdr.Header.Get("Content-Type")
	if !allowed[mime] {
		return nil, "", &httpError{http.StatusUnsupportedMediaType, "unsupported_media", field + ": неподдерживаемый формат " + mime}
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "", &httpError{http.StatusBadRequest, "validation", field + ": не удалось прочитать"}
	}
	if int64(len(data)) > limit {
		return nil, "", &httpError{http.StatusRequestEntityTooLarge, "too_large", field + ": файл слишком большой"}
	}
	return data, mime, nil
}

// buildParticipants собирает участников для промпта из КАНОНИЧЕСКИХ профилей
// (коллекция user), а не из embedded-снимков комнаты — только там актуальные
// алиасы. При сбое чтения откатывается к именам из комнаты (без алиасов).
func (s *Server) buildParticipants(ctx context.Context, room *api.Room) []ai.Participant {
	members := roomMembers(room)
	ids := make([]int, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}

	canonical, err := s.userRepo.FindByIds(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("cannot load canonical users for parse; falling back to room members")
		canonical = members
	}
	byId := make(map[int]api.User, len(canonical))
	for _, u := range canonical {
		byId[u.ID] = u
	}

	out := make([]ai.Participant, 0, len(members))
	for _, m := range members {
		u := m
		if cu, ok := byId[m.ID]; ok {
			u = cu
		}
		out = append(out, ai.Participant{
			UserId:      u.ID,
			DisplayName: u.DisplayName,
			Username:    u.Username,
			Aliases:     u.Aliases,
		})
	}
	return out
}
