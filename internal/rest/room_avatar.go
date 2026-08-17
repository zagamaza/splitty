package rest

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// maxAvatarFileBytes — потолок на аву комнаты. Клиент сжимает картинку перед
// отправкой; лимит нужен на случай, когда не сжал.
const maxAvatarFileBytes = 5 << 20

// maxAvatarSide — потолок на сторону картинки в пикселях. Клиенты ужимают аву
// до 1024, запас взят на случай чужого клиента; всё, что больше, — либо ошибка,
// либо попытка положить остальных участников декодированием.
const maxAvatarSide = 4096

// allowedAvatarUploadTypes — что принимаем как аву. Уже, чем список инлайновых
// типов отдачи: ни gif, ни видео, ни webp. Webp нет намеренно — в stdlib нет
// его декодера, а без разбора заголовка нельзя проверить размеры картинки
// (см. maxAvatarSide). Оба клиента шлют JPEG, тащить зависимость не за чем.
var allowedAvatarUploadTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
}

// handleSetRoomAvatar PUT /api/v1/rooms/{roomId}/avatar — загрузка фото группы
// (multipart, поле «image»). Право — любой участник комнаты, как у смены валюты.
//
// Старый файл удаляется сразу после того, как комната начала ссылаться на
// новый: обратный порядок оставил бы комнату со ссылкой в никуда, если запись
// не удастся.
func (s *Server) handleSetRoomAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomId := r.PathValue("roomId")
	userId := userIdFromCtx(ctx)

	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "хранилище файлов недоступно")
		return
	}

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarFileBytes+1<<20)
	if err := r.ParseMultipartForm(maxAvatarFileBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "файл слишком большой")
		return
	}

	data, mime, hErr := readFilePart(r, "image", maxAvatarFileBytes, allowedAvatarUploadTypes)
	if hErr != nil {
		hErr.write(w)
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "validation", "нужно передать image")
		return
	}
	// Заголовку Content-Type верить нельзя: его пишет клиент. Тип определяем по
	// самим байтам, иначе под видом jpeg приедет что угодно, а отдавать мы это
	// будем со своего origin.
	sniffed := http.DetectContentType(data)
	if !allowedAvatarUploadTypes[sniffed] {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media", "нужна картинка JPEG или PNG")
		return
	}
	mime = sniffed

	// Размера тела мало: одноцветный PNG на 30000×30000 сжимается в считаные
	// килобайты, а на клиенте разворачивается в гигабайты пикселей и роняет
	// приложение остальных участников. Разбираем только заголовок картинки —
	// сами пиксели декодировать не нужно.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media", "не удалось прочитать картинку")
		return
	}
	if cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "картинка слишком большая")
		return
	}

	fileId, err := s.files.Save(ctx, &api.StoredFile{
		RoomId:  room.ID,
		OwnerId: userId,
		Kind:    api.StoredFileRoomAvatar,
		Mime:    mime,
		Data:    data,
	})
	if err != nil {
		log.Error().Err(err).Msgf("cannot save avatar for room %s", roomId)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить фото")
		return
	}

	// Прежнюю ссылку отдаёт сама запись — не читаем её из снимка комнаты:
	// две одновременные загрузки увидели бы один и тот же снимок и удалили один
	// и тот же файл, а проигравший гонку остался бы в базе навсегда.
	previous, err := s.roomRepo.SetAvatarFileId(ctx, roomId, fileId)
	if err != nil {
		// Ссылку поставить не удалось — убираем только что загруженные байты,
		// иначе они останутся в базе никем не адресуемые.
		if delErr := s.files.Delete(ctx, fileId); delErr != nil {
			log.Error().Err(delErr).Msg("cannot drop orphan avatar")
		}
		log.Error().Err(err).Msgf("cannot set avatar for room %s", roomId)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить фото")
		return
	}
	s.dropPreviousAvatar(ctx, previous)

	writeJSON(w, http.StatusOK, roomAvatarDto{AvatarFileId: fileId})
}

// handleDeleteRoomAvatar DELETE /api/v1/rooms/{roomId}/avatar — снять фото
// группы. Идемпотентно: у комнаты без авы ответ тот же.
func (s *Server) handleDeleteRoomAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomId := r.PathValue("roomId")

	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "хранилище файлов недоступно")
		return
	}

	if _, hErr := s.roomForMember(ctx, roomId, userIdFromCtx(ctx)); hErr != nil {
		hErr.write(w)
		return
	}

	previous, err := s.roomRepo.SetAvatarFileId(ctx, roomId, "")
	if err != nil {
		log.Error().Err(err).Msgf("cannot clear avatar for room %s", roomId)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось убрать фото")
		return
	}
	s.dropPreviousAvatar(ctx, previous)

	w.WriteHeader(http.StatusNoContent)
}

// dropPreviousAvatar удаляет прежнюю картинку комнаты. Ошибка не валит запрос:
// ссылки на файл уже нет, и худшее последствие — лишние байты в базе.
func (s *Server) dropPreviousAvatar(ctx context.Context, previous string) {
	if previous == "" {
		return
	}
	if err := s.files.Delete(ctx, previous); err != nil {
		log.Error().Err(err).Msg("cannot delete previous avatar")
	}
}
