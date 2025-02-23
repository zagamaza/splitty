package dailyexpenses

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"net/http"
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

	//тут настраиваются пользователи
	userIds := i.config.Users

	var users []api.User
	for _, uId := range userIds {
		user, err := i.UserService.FindById(context.Background(), uId)
		if err != nil {
			log.Error().Err(err).Msg("Ошибка при получении пользователя")
			return
		}
		users = append(users, *user)
	}

	roomIds := make(map[primitive.ObjectID]primitive.ObjectID)
	for _, user := range users {
		rooms, err := i.RoomService.FindRoomsByUserId(context.Background(), user.ID)
		if err != nil {
			log.Error().Err(err).Msg("Ошибка при получении комнат")
			return
		}
		for _, room := range *rooms {
			id := room.ID
			roomIds[id] = id
		}
	}

	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			var operations []api.Operation
			for _, rId := range roomIds {
				ops, err := i.OperationService.GetAllSpendOperations(context.Background(), rId.String())
				if err != nil {
					log.Error().Err(err).Msg("Ошибка при получении операций")
					return
				}
				operations = append(operations, *ops...)
			}

			// Сериализация структуры в JSON
			jsonData, err := json.Marshal(operations)
			if err != nil {
				log.Error().Err(err).Msg("Ошибка при сериализации данных")
				return
			}
			sendPostRequest(i.config.Url, jsonData)
		}
	}()
}

type Config struct {
	Url   string
	Users []int
}
