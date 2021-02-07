package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
)

func NewUserService(r repository.UserRepository) *UserService {
	return &UserService{r}
}

func NewRoomService(r repository.RoomRepository) *RoomService {
	return &RoomService{r}
}

func NewChatStateService(r repository.ChatStateRepository) *ChatStateService {
	return &ChatStateService{r}
}

func NewButtonService(r repository.ButtonRepository) *ButtonService {
	return &ButtonService{r}
}

func NewOperationService(r repository.RoomRepository) *OperationService {
	return &OperationService{r}
}

type UserService struct {
	repository.UserRepository
}

type RoomService struct {
	repository.RoomRepository
}

type ChatStateService struct {
	repository.ChatStateRepository
}

type ButtonService struct {
	repository.ButtonRepository
}

type OperationService struct {
	repository.RoomRepository
}

func (rs *RoomService) CreateRoom(ctx context.Context, r *api.Room) (*api.Room, error) {
	rId, err := rs.RoomRepository.SaveRoom(ctx, r)
	r.ID = rId
	return r, err
}

func (css *ChatStateService) CleanChatState(ctx context.Context, state *api.ChatState) {
	if state == nil {
		return
	} else if err := (*css).DeleteByUserId(ctx, state.UserId); err != nil {
		log.Error().Err(err).Msg("")
	}
}

func (s *OperationService) GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error) {
	id, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id:", roomId)
		return nil, err
	}
	return id.Operations, nil
}
