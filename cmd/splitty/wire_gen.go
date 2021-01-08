// Code generated by Wire. DO NOT EDIT.

//go:generate wire
//+build !wireinject

package main

import (
	"context"
	"github.com/almaznur91/splitty/internal/events"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"
)

// Injectors from wire.go:

func initApp(ctx context.Context, cfg *config) (*events.TelegramListener, func(), error) {
	database, cleanup, err := initMongoConnection(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	mongoUserRepository := repository.NewUserRepository(database)
	userService := service.NewUserService(mongoUserRepository)
	mongoRoomRepository := repository.NewRoomRepository(database)
	roomService := service.NewRoomService(mongoRoomRepository)
	telegramListener, err := initTelegramConfig(ctx, cfg, userService, roomService)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return telegramListener, func() {
		cleanup()
	}, nil
}
