package dailyexpenses

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/safe"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"net/http"
	"strings"
	"time"
)

func NewIntegrationService(roomService *service.RoomService, userService *service.UserService,
	operationService *service.OperationService, config *Config,
) *IntegrationService {
	return &IntegrationService{
		RoomService:      *roomService,
		UserService:      *userService,
		OperationService: *operationService,
		config:           *config,
	}
}

type IntegrationService struct {
	service.RoomService
	service.UserService
	service.OperationService
	config Config
}

// SendPostRequest выполняет отправку POST запроса с указанными данными.
func sendPostRequest(url string, jsonData []byte) {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error().Err(err).Msg("Ошибка при отправке запроса")
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msg("Ошибка при закрытии тела запроса")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Error().Msgf("Ошибка при отправке запроса: %s", resp.Status)
	}
}

// StartPostScheduler запускает горутину, которая каждую минуту отправляет POST запрос.
func (i *IntegrationService) StartPostScheduler() {
	// Пустой адрес — выгрузки нет. Раньше он был вшит в значение по умолчанию,
	// и любая поднятая сборка начинала отправлять расходы живых людей на
	// сторонний хост, никого не спросив
	if strings.TrimSpace(i.config.Url) == "" || len(i.config.Users) == 0 {
		log.Info().Msg("выгрузка расходов выключена: не задан DAILY_EXPENSES_URL или DAILY_EXPENSES_USERS")
		return
	}

	//тут настраиваются пользователи
	userIds := i.config.Users


	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer safe.Recover("планировщик выгрузки расходов")
		for range ticker.C {
			// Комнаты перечитываются КАЖДЫЙ цикл, а не один раз на старте: в
			// конверт уходят имя и валюта, а они меняются. Снимок со старта
			// врал бы получателю до перезапуска, и новые тусы не появлялись бы
			// в выгрузке вовсе.
			rooms, err := i.exportRooms(context.Background(), userIds)
			if err != nil {
				log.Error().Err(err).Msg("Ошибка при получении комнат")
				continue
			}

			envelope := exportEnvelope{Version: exportVersion, GeneratedAt: time.Now().UTC()}
			failed := false
			for _, room := range rooms {
				ops, err := i.OperationService.GetAllOperations(context.Background(), room.ID.Hex())
				if err != nil {
					log.Error().Err(err).Msg("Ошибка при получении операций")
					failed = true
					break
				}
				envelope.Expenses = append(envelope.Expenses, exportExpenses(room, *ops)...)
			}
			// Пропускаем ЦИКЛ, а не выходим из планировщика: прежний return
			// убивал выгрузку навсегда из-за одной временной ошибки чтения.
			if failed {
				continue
			}

			jsonData, err := json.Marshal(envelope)
			if err != nil {
				log.Error().Err(err).Msg("Ошибка при сериализации данных")
				continue
			}
			sendPostRequest(i.config.Url, jsonData)
		}
	}()
}

type Config struct {
	Url   string
	Users []int
}

// exportRooms собирает комнаты выгружаемых пользователей в свежем виде.
// Дубли схлопываются: одна туса может быть у нескольких из них.
func (i *IntegrationService) exportRooms(ctx context.Context, userIds []int) ([]api.Room, error) {
	seen := make(map[primitive.ObjectID]bool)
	out := make([]api.Room, 0)
	for _, userId := range userIds {
		rooms, err := i.RoomService.FindRoomsByUserId(ctx, userId)
		if err != nil {
			return nil, err
		}
		for _, room := range *rooms {
			if seen[room.ID] {
				continue
			}
			seen[room.ID] = true
			out = append(out, room)
		}
	}
	return out, nil
}
