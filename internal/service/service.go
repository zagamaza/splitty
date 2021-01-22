package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
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

func (rs *RoomService) CreateRoom(ctx context.Context, r *api.Room) (*api.Room, error) {
	rId, err := rs.RoomRepository.SaveRoom(ctx, r)
	r.ID = rId
	return r, err
}
