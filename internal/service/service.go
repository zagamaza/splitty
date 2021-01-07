package service

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"math/rand"
	"splitty/internal/repository"
)

type Service interface {
	GetDiamonds(ctx context.Context, mineName string) (int, error)
	GetAllMines(ctx context.Context) ([]repository.Mine, error)
	AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error)
	EmptyMine(ctx context.Context, name string) (diamondCount int, err error)
}

func New(r repository.MineRepository) *SimpleMineService {
	return &SimpleMineService{r: r}
}

type SimpleMineService struct {
	r repository.MineRepository
}

func (s *SimpleMineService) GetDiamonds(ctx context.Context, mineName string) (int, error) {
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

func (s *SimpleMineService) GetAllMines(ctx context.Context) ([]repository.Mine, error) {
	r, err := s.r.GetAllMines(ctx)
	if err == mongo.ErrNoDocuments {
		return nil, echo.ErrNotFound
	}
	return r, err
}

func (s *SimpleMineService) AddDiamondMine(ctx context.Context, m *repository.Mine) (*repository.Mine, error) {
	return s.r.AddDiamondMine(ctx, m)
}

func (s *SimpleMineService) EmptyMine(ctx context.Context, name string) (diamondCount int, err error) {
	r, err := s.r.EmptyMine(ctx, name)
	if err == mongo.ErrNoDocuments {
		return 0, echo.ErrNotFound
	}
	return r, err
}
