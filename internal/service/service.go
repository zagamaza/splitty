package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"math/rand"
)

type Service interface {
	UpsertUser(ctx context.Context, user tbapi.User) error
	GetDiamonds(ctx context.Context, mineName string) (int, error)
	GetAllMines(ctx context.Context) ([]repository.Mine, error)
	AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error)
	EmptyMine(ctx context.Context, name string) (diamondCount int, err error)
	CreateRoom(ctx context.Context, user api.User) (*repository.Room, error)
	JoinToRoom(ctx context.Context, u *api.User, roomId string) error
	FindById(ctx context.Context, id string) (*repository.Room, error)
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

func (us *UserService) GetDiamonds(ctx context.Context, mineName string) (int, error) {
	m, err := us.r.FindByName(ctx, mineName)
	if err == mongo.ErrNoDocuments {
		return 0, echo.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	log.Debug().Interface("result", m).Msg("found mine ")
	if m.DiamondCount <= 0 {
		return us.EmptyMine(ctx, m.Name)
	}
	dc := rand.Intn(m.DiamondCount)
	nc := m.DiamondCount - dc
	if err := us.r.UpdateMine(ctx, m.Name, nc); err != nil {
		return 0, err
	}
	return dc, nil
}

func (us *UserService) GetAllMines(ctx context.Context) ([]repository.Mine, error) {
	r, err := us.r.GetAllMines(ctx)
	if err == mongo.ErrNoDocuments {
		return nil, echo.ErrNotFound
	}
	return r, err
}

func (us *UserService) AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error) {
	return us.r.AddDiamondMine(ctx, m)
}

func (us *UserService) EmptyMine(ctx context.Context, name string) (diamondCount int, err error) {
	r, err := us.r.EmptyMine(ctx, name)
	if err == mongo.ErrNoDocuments {
		return 0, echo.ErrNotFound
	}
	return r, err
}
