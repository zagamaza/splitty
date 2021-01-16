package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type Service interface {
	UpsertUser(ctx context.Context, user tbapi.User) error
	CreateRoom(ctx context.Context, user api.User) (*repository.Room, error)
	JoinToRoom(ctx context.Context, u *api.User, roomId string) error
	FindById(ctx context.Context, id string) (*repository.Room, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]repository.Room, error)
}

func NewUserService(r repository.UserRepository) *UserService {
	return &UserService{r: r}
}
func NewRoomService(r repository.RoomRepository) *RoomService {
	return &RoomService{r: r}
}

type UserService struct {
	r repository.UserRepository
}

type RoomService struct {
	r repository.RoomRepository
}

func (rs *RoomService) FindRoomsByUserId(ctx context.Context, uId int) (*[]repository.Room, error) {
	return rs.r.FindRoomsByUserId(ctx, uId)
}

func (rs *RoomService) JoinToRoom(ctx context.Context, u api.User, roomId string) error {
	return rs.r.JoinToRoom(ctx, u, roomId)
}

func (rs *RoomService) FindById(ctx context.Context, id string) (*repository.Room, error) {
	return rs.r.FindById(ctx, id)
}

func (rs *RoomService) CreateRoom(ctx context.Context, u api.User) (*repository.Room, error) {
	r := &repository.Room{
		Users: &[]api.User{u},
		Name:  "Тестовая",
	}
	rId, err := rs.r.SaveRoom(ctx, r)
	r.ID = rId
	return r, err
}

func (us *UserService) UpsertUser(ctx context.Context, u api.User) error {
	return us.r.UpsertUser(ctx, u)
}
