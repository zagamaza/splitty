package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/repository"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"math/rand"
)

type Service interface {
	UpsertUser(ctx context.Context, user *tbapi.User) error
	GetDiamonds(ctx context.Context, mineName string) (int, error)
	GetAllMines(ctx context.Context) ([]repository.Mine, error)
	AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error)
	EmptyMine(ctx context.Context, name string) (diamondCount int, err error)
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

func (s *UserService) UpsertUser(ctx context.Context, user *tbapi.User) error {
	u, err := ToUserEntity(ctx, user)
	if err != nil {
		log.Err(err)
	}
	return s.r.UpsertUser(ctx, u)
}

func (s *UserService) GetDiamonds(ctx context.Context, mineName string) (int, error) {
	m, err := s.r.FindByName(ctx, mineName)
	if err == mongo.ErrNoDocuments {
		return 0, echo.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	log.Debug().Interface("result", m).Msg("found mine ")
	if m.DiamondCount <= 0 {
		return s.EmptyMine(ctx, m.Name)
	}
	dc := rand.Intn(m.DiamondCount)
	nc := m.DiamondCount - dc
	if err := s.r.UpdateMine(ctx, m.Name, nc); err != nil {
		return 0, err
	}
	return dc, nil
}

func (s *UserService) GetAllMines(ctx context.Context) ([]repository.Mine, error) {
	r, err := s.r.GetAllMines(ctx)
	if err == mongo.ErrNoDocuments {
		return nil, echo.ErrNotFound
	}
	return r, err
}

func (s *UserService) AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error) {
	return s.r.AddDiamondMine(ctx, m)
}

func (s *UserService) EmptyMine(ctx context.Context, name string) (diamondCount int, err error) {
	r, err := s.r.EmptyMine(ctx, name)
	if err == mongo.ErrNoDocuments {
		return 0, echo.ErrNotFound
	}
	return r, err
}
