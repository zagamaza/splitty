package rest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/almaznur91/splitty/internal/analytics"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
)

const (
	// maxEventsPerRequest — потолок пачки. Отдельный лимит тела не нужен: общий
	// в 1 МБ ставит maxBodyMiddleware, а /api/v1/events не в bigBodyPaths.
	maxEventsPerRequest = 50
	// eventsPerUserPerMin — сколько СОБЫТИЙ (не запросов) принимаем от человека
	// в минуту. Лимит здесь про стоимость и кривого клиента, а не про перебор:
	// событий много по определению.
	eventsPerUserPerMin = 300
	// eventsPerDevicePerMin — то же для анонимного маршрута. Ниже: до входа
	// событий у клиента ровно четыре вида, и поток там на порядок реже, а
	// маршрут открыт всему интернету.
	eventsPerDevicePerMin = 60
	// eventAgeLimit / eventFutureLimit — окно доверия к часам телефона. Они
	// врут и переводятся руками, а один аппарат с датой 2035 года испортил бы
	// все графики разом.
	eventAgeLimit    = 7 * 24 * time.Hour
	eventFutureLimit = time.Hour
)

type eventDto struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	At         time.Time         `json:"at"`
	Session    string            `json:"session"`
	Platform   string            `json:"platform"`
	AppVersion string            `json:"appVersion"`
	Locale     string            `json:"locale"`
	Params     map[string]string `json:"params"`
}

type eventsRequest struct {
	Events []eventDto `json:"events"`
}

// anonymousEventsRequest — пачка событий, присланная ДО входа.
//
// Device — установка приложения, а не человек: экран входа человека ещё не
// знает. Формат тот же, что у id и session.
type anonymousEventsRequest struct {
	Device string     `json:"device"`
	Events []eventDto `json:"events"`
}

// eventsResponse — три числа, а не «ок».
//
// Без них потеря событий и штатный дедуп выглядят одинаково; на этих числах
// держится обещание измерять фактический поток, а не проектировать свёртки по
// косвенным метрикам.
type eventsResponse struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Rejected   int `json:"rejected"`
}

// handlePostEvents принимает пачку продуктовых событий.
//
// user_id берётся ИЗ ТОКЕНА и из тела не читается никогда: иначе любой
// авторизованный клиент писал бы события от чужого имени.
func (s *Server) handlePostEvents(w http.ResponseWriter, r *http.Request) {
	userId := userIdFromCtx(r.Context())

	var req eventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "не разобрал тело запроса")
		return
	}
	if len(req.Events) == 0 {
		writeJSON(w, http.StatusOK, eventsResponse{})
		return
	}
	if len(req.Events) > maxEventsPerRequest {
		writeError(w, http.StatusBadRequest, "validation", "слишком много событий в одном запросе")
		return
	}

	// Выключенный приём отвечает «принято» и ничего не пишет. Отдавать отказ
	// нельзя: клиент решил бы, что не доставил, и копил бы очередь, ретрая её
	// вечно — то есть выключатель сам стал бы источником нагрузки.
	if !s.analyticsEnabled {
		writeJSON(w, http.StatusOK, eventsResponse{Accepted: len(req.Events)})
		return
	}
	if s.productEvents == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "приём событий не настроен")
		return
	}

	if !s.authThrottle.allowCost("events:"+strconv.Itoa(userId), eventsPerUserPerMin, len(req.Events)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много событий, попробуйте позже")
		return
	}

	now := time.Now().UTC()
	events := make([]repository.ProductEvent, 0, len(req.Events))
	rejected := 0
	for _, dto := range req.Events {
		if err := validateEvent(dto); err != nil {
			rejected++
			continue
		}
		events = append(events, repository.ProductEvent{
			ID:         dto.ID,
			UserID:     userId,
			Name:       dto.Name,
			At:         eventTime(dto.At, now),
			Session:    dto.Session,
			Platform:   dto.Platform,
			AppVersion: dto.AppVersion,
			Locale:     dto.Locale,
			Params:     dto.Params,
		})
	}

	result, err := s.productEvents.Insert(r.Context(), events)
	if err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot store product events")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось записать события")
		return
	}

	writeJSON(w, http.StatusOK, eventsResponse{
		Accepted:   result.Accepted,
		Duplicates: result.Duplicates,
		Rejected:   rejected,
	})
}

// validateEvent — одно событие пачки. Кривое отбрасывается поштучно, а не
// роняет пачку: у клиента может разъехаться одно поле, и терять из-за него
// остальные девятнадцать событий незачем.
func validateEvent(dto eventDto) error {
	if dto.ID == "" {
		return errEventField("id")
	}
	if err := validateClientId("id", dto.ID); err != nil {
		return errEventField("id")
	}
	if dto.Session == "" {
		return errEventField("session")
	}
	if err := validateClientId("session", dto.Session); err != nil {
		return errEventField("session")
	}
	if dto.Platform != "ios" && dto.Platform != "android" {
		return errEventField("platform")
	}
	return analytics.Validate(dto.Name, dto.Params)
}

type eventFieldError string

func (e eventFieldError) Error() string { return "поле " + string(e) }

func errEventField(field string) error { return eventFieldError(field) }

// eventTime зажимает время события в окно доверия к часам телефона.
func eventTime(at, now time.Time) time.Time {
	if at.IsZero() || at.Before(now.Add(-eventAgeLimit)) || at.After(now.Add(eventFutureLimit)) {
		return now
	}
	return at.UTC()
}

// handlePostAnonymousEvents принимает события ДО входа.
//
// Отдельный маршрут, а не послабление в основном: разница не в авторизации, а
// в том, что можно прислать. Здесь открыты четыре имени из analytics.Anonymous
// и ничего больше — маршрут доступен всему интернету, и каждое лишнее имя это
// то, чем коллекцию засорят бесплатно.
//
// Событие ложится с UserID 0 и device_id. Связывать его с аккаунтом сервер не
// будет даже потом: воронка считается по ступеням, а не по людям, и склейка
// превратила бы обезличенный поток в профиль.
func (s *Server) handlePostAnonymousEvents(w http.ResponseWriter, r *http.Request) {
	var req anonymousEventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "не разобрал тело запроса")
		return
	}
	// Пустая строка проходит validateClientId (в ней нечего проверять), а
	// событие без устройства обезличено настолько, что считать по нему нечего.
	if req.Device == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле device обязательно")
		return
	}
	if err := validateClientId("device", req.Device); err != nil {
		err.write(w)
		return
	}
	if len(req.Events) == 0 {
		writeJSON(w, http.StatusOK, eventsResponse{})
		return
	}
	if len(req.Events) > maxEventsPerRequest {
		writeError(w, http.StatusBadRequest, "validation", "слишком много событий в одном запросе")
		return
	}

	// Как и на основном маршруте: выключенный приём отвечает «принято».
	if !s.analyticsEnabled {
		writeJSON(w, http.StatusOK, eventsResponse{Accepted: len(req.Events)})
		return
	}
	if s.productEvents == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "приём событий не настроен")
		return
	}

	if !s.authThrottle.allowCost("events:anon:"+req.Device, eventsPerDevicePerMin, len(req.Events)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много событий, попробуйте позже")
		return
	}

	now := time.Now().UTC()
	events := make([]repository.ProductEvent, 0, len(req.Events))
	rejected := 0
	for _, dto := range req.Events {
		if !analytics.IsAnonymous(dto.Name) {
			rejected++
			continue
		}
		if err := validateEvent(dto); err != nil {
			rejected++
			continue
		}
		events = append(events, repository.ProductEvent{
			ID:         dto.ID,
			DeviceID:   req.Device,
			Name:       dto.Name,
			At:         eventTime(dto.At, now),
			Session:    dto.Session,
			Platform:   dto.Platform,
			AppVersion: dto.AppVersion,
			Locale:     dto.Locale,
			Params:     dto.Params,
		})
	}

	result, err := s.productEvents.Insert(r.Context(), events)
	if err != nil {
		log.Error().Err(err).Msg("cannot store anonymous product events")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось записать события")
		return
	}

	writeJSON(w, http.StatusOK, eventsResponse{
		Accepted:   result.Accepted,
		Duplicates: result.Duplicates,
		Rejected:   rejected,
	})
}
