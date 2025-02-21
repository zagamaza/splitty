package dailyexpenses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"net/http"
	"time"
)

type IntegrationService struct {
	service.RoomService
	service.UserService
	service.OperationService
}

// SendPostRequest выполняет отправку POST запроса с указанными данными.
func sendPostRequest(url string, jsonData []byte) {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Ошибка при отправке запроса:", err)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println("Ошибка при закрытии тела ответа:", err)
		}
	}(resp.Body)

	fmt.Println("Запрос отправлен, статус:", resp.Status)
}

// StartPostScheduler запускает горутину, которая каждую минуту отправляет POST запрос.
func (i *IntegrationService) StartPostScheduler() {
	url := "http://pet.zagirnur.dev:19090/from-splitty"

	//тут настраиваются пользователи
	userIds := []int{
		147181773,
		369575379,
		172261383,
		304898122,
		360624984,
		373160631,
	}

	var users []api.User
	for _, uId := range userIds {
		user, err := i.UserService.FindById(context.Background(), uId)
		if err != nil {
			fmt.Println("Ошибка при получении пользователя:", err)
			return
		}
		users = append(users, *user)
	}

	var roomIds map[primitive.ObjectID]primitive.ObjectID
	for _, user := range users {
		rooms, err := i.RoomService.FindRoomsByUserId(context.Background(), user.ID)
		if err != nil {
			fmt.Println("Ошибка при получении комнат:", err)
			return
		}
		for rIdx := range *rooms {
			id := (*rooms)[rIdx].ID
			roomIds[id] = id
		}
	}

	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			var operations []api.Operation
			for _, rId := range roomIds {
				ops, err := i.OperationService.GetAllOperations(context.Background(), rId.String())
				if err != nil {
					fmt.Println("Ошибка при получении операций:", err)
					return
				}
				operations = append(operations, *ops...)
			}

			// Сериализация структуры в JSON
			jsonData, err := json.Marshal(operations)
			if err != nil {
				fmt.Println("Ошибка при сериализации данных:", err)
				return
			}
			sendPostRequest(url, jsonData)
		}
	}()
}
