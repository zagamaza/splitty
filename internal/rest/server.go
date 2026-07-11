// Package rest реализует REST API поверх сервисов splitty (контракт: docs/API.md).
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog/log"
)

// Notifier отправляет участникам комнаты telegram-уведомления о REST-мутациях —
// те же, что шлют экраны бота (реализация — bot.Notifier). Интерфейс объявлен
// в пакете rest узким, чтобы не тащить пакет bot и telegram-зависимости в тесты.
// Методы вызываются в фоне после успешной записи и не должны возвращать ошибок —
// сбой уведомления не ломает запрос (реализация только логирует)
type Notifier interface {
	NotifyOperationCreated(ctx context.Context, room api.Room, op api.Operation, author api.User)
	NotifyOperationUpdated(ctx context.Context, room api.Room, oldOp api.Operation, newOp api.Operation, author api.User)
	NotifyOperationDeleted(ctx context.Context, room api.Room, op api.Operation, author api.User)
	NotifyRepaymentCreated(ctx context.Context, room api.Room, op api.Operation, author api.User)
}

// Config конфигурация REST-сервера
type Config struct {
	Listen    string // адрес http-сервера, например "localhost:7171"
	JwtSecret string // секрет подписи JWT (HS256)
	DevAuth   bool   // включает POST /auth/dev
	TgToken   string // токен бота: нужен для проверки Telegram Login и проксирования файлов
}

// Server REST API сервер со всеми зависимостями.
// Статистика и долги считаются в хендлерах по нормализованной комнате
// (см. normalizedRoom) расчётом develop — отдельный StatisticService не нужен:
// он перечитывает комнату из базы и не видит нормализацию легаси-операций
type Server struct {
	cfg           Config
	userRepo      repository.UserRepository
	roomRepo      repository.RoomRepository
	loginCodeRepo repository.LoginCodeRepository
	roomSrv       *service.RoomService
	operationSrv  *service.OperationService
	// notifier опционален (см. SetNotifier): nil — уведомления выключены (no-op)
	notifier Notifier

	httpServer *http.Server
	httpClient *http.Client
	tgApiURL   string           // базовый url telegram api, переопределяется в тестах
	now        func() time.Time // источник времени для статистики, переопределяется в тестах
}

// NewServer собирает сервер со всеми зависимостями
func NewServer(cfg Config, ur repository.UserRepository, rr repository.RoomRepository,
	lr repository.LoginCodeRepository, rs *service.RoomService, os *service.OperationService) *Server {
	// клиент для telegram: без общего Timeout, иначе он режет скачивание больших
	// файлов (таймер тикает и во время чтения тела). Ограничиваем только фазу
	// соединения/заголовков, а тело привязано к контексту входящего запроса
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second

	s := &Server{
		cfg:           cfg,
		userRepo:      ur,
		roomRepo:      rr,
		loginCodeRepo: lr,
		roomSrv:       rs,
		operationSrv:  os,
		httpClient:    &http.Client{Transport: transport},
		tgApiURL:      "https://api.telegram.org",
		now:           time.Now,
	}
	// httpServer создаётся здесь, а не в Run: Shutdown может быть вызван из
	// closer/сигнальной горутины параллельно с Run — инициализация поля до
	// запуска горутин исключает data race на s.httpServer
	s.httpServer = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// защита от slowloris: ReadTimeout ограничивает чтение всего запроса,
		// IdleTimeout закрывает вечные keep-alive соединения.
		// WriteTimeout намеренно не задан — через сервер стримятся большие файлы
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return s
}

// SetNotifier включает telegram-уведомления о мутациях (когда бот сконфигурирован).
// Вызывать до Run. Nil-безопасно: без notifier'а сервер работает как раньше
func (s *Server) SetNotifier(n Notifier) {
	s.notifier = n
}

// notifyTimeout ограничивает фоновую отправку уведомлений одной мутации
const notifyTimeout = 30 * time.Second

// notifyAsync выполняет notify в отдельной горутине, чтобы поход в telegram не
// задерживал ответ клиенту. Контекст отвязан от запроса (context.WithoutCancel —
// значения auth сохраняются, но завершение запроса не отменяет отправку)
// и ограничен notifyTimeout. При nil notifier (бот выключен) — no-op
func (s *Server) notifyAsync(ctx context.Context, notify func(ctx context.Context, n Notifier)) {
	if s.notifier == nil {
		return
	}
	nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	go func() {
		defer cancel()
		notify(nctx, s.notifier)
	}()
}

// Handler возвращает корневой http.Handler со всеми маршрутами и middleware
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /api/v1/auth/telegram", s.handleAuthTelegram)
	mux.HandleFunc("POST /api/v1/auth/code", s.handleAuthCode)
	mux.HandleFunc("POST /api/v1/auth/dev", s.handleAuthDev)

	mux.Handle("GET /api/v1/me", s.auth(s.handleGetMe))
	mux.Handle("PATCH /api/v1/me", s.auth(s.handlePatchMe))

	mux.Handle("GET /api/v1/rooms", s.auth(s.handleListRooms))
	mux.Handle("POST /api/v1/rooms", s.auth(s.handleCreateRoom))
	mux.Handle("GET /api/v1/rooms/{roomId}", s.auth(s.handleGetRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/join", s.auth(s.handleJoinRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/archive", s.auth(s.handleArchiveRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/unarchive", s.auth(s.handleUnarchiveRoom))
	mux.Handle("PUT /api/v1/rooms/{roomId}/currency", s.auth(s.handleUpdateCurrency))
	mux.Handle("GET /api/v1/currencies", s.auth(s.handleCurrencies))

	mux.Handle("GET /api/v1/rooms/{roomId}/operations", s.auth(s.handleListOperations))
	mux.Handle("POST /api/v1/rooms/{roomId}/operations", s.auth(s.handleCreateOperation))
	mux.Handle("PUT /api/v1/rooms/{roomId}/operations/{operationId}", s.auth(s.handleUpdateOperation))
	mux.Handle("DELETE /api/v1/rooms/{roomId}/operations/{operationId}", s.auth(s.handleDeleteOperation))

	mux.Handle("GET /api/v1/rooms/{roomId}/debts", s.auth(s.handleListDebts))
	mux.Handle("POST /api/v1/rooms/{roomId}/repayments", s.auth(s.handleCreateRepayment))
	mux.Handle("GET /api/v1/rooms/{roomId}/statistics", s.auth(s.handleStatistics))

	mux.Handle("GET /api/v1/friends", s.auth(s.handleFriends))
	mux.Handle("GET /api/v1/activity", s.auth(s.handleActivity))

	mux.Handle("GET /api/v1/files/{fileId}", s.auth(s.handleGetFile))

	// все незарегистрированные пути — 404 в едином json-формате
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
	})

	return recoverMiddleware(logMiddleware(maxBodyMiddleware(mux)))
}

// maxRequestBodyBytes лимит тела запроса — 1 МБ: защита от OOM на гигантском json
// (в т.ч. на неаутентифицированном /auth/telegram). Превышение decodeJSON отдаёт как 413
const maxRequestBodyBytes = 1 << 20

// maxBodyMiddleware ограничивает размер тела всех запросов через http.MaxBytesReader
func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// Run запускает http-сервер, блокирующий вызов. Останавливается через Shutdown или отмену ctx
func (s *Server) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	log.Info().Msgf("rest api listening on %s", s.cfg.Listen)
	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown корректно останавливает http-сервер
func (s *Server) Shutdown() {
	if s.httpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("rest api shutdown failed")
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// recoverMiddleware превращает панику в 500 с json-телом
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Msgf("panic! %s %s: %v, stack: %s", r.Method, r.URL.Path, rec, string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logMiddleware пишет request-лог через zerolog
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Dur("duration", time.Since(start)).
			Msg("rest request")
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Error().Err(err).Msg("cannot write response")
	}
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}
