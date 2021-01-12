//+build wireinject

package main

import (
	"context"
	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/events"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"

	"github.com/google/wire"
)

func initApp(ctx context.Context, cfg *config) (tg *events.TelegramListener, closer func(), err error) {
	wire.Build(initMongoConnection, initTelegramConfig,
		service.NewRoomService, wire.Bind(new(bot.RoomService), new(*service.RoomService)),
		service.NewUserService, wire.Bind(new(bot.UserService), new(*service.UserService)),
		repository.NewUserRepository, wire.Bind(new(repository.UserRepository), new(*repository.MongoUserRepository)),
		repository.NewRoomRepository, wire.Bind(new(repository.RoomRepository), new(*repository.MongoRoomRepository)),
	)
	return nil, nil, nil
}
