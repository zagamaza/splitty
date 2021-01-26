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
	wire.Build(initMongoConnection, initTelegramApi, initTelegramConfig, initBotConfig,
		service.NewUserService, wire.Bind(new(bot.UserService), new(*service.UserService)),
		service.NewRoomService, wire.Bind(new(bot.RoomService), new(*service.RoomService)),
		service.NewChatStateService, wire.Bind(new(bot.ChatStateService), new(*service.ChatStateService)),
		service.NewButtonService, wire.Bind(new(bot.ButtonService), new(*service.ButtonService)),
		wire.Bind(new(events.ChatStateService), new(*service.ChatStateService)),
		wire.Bind(new(events.ButtonService), new(*service.ButtonService)),
		ProvideBotList, bot.NewHelper, bot.NewStartScreen, bot.NewRoomCreating, bot.NewRoomSetName, bot.NewJoinRoom, bot.NewAllRoom, bot.NewOperation,
		repository.NewUserRepository, wire.Bind(new(repository.UserRepository), new(*repository.MongoUserRepository)),
		repository.NewRoomRepository, wire.Bind(new(repository.RoomRepository), new(*repository.MongoRoomRepository)),
		repository.NewChatStateRepository, wire.Bind(new(repository.ChatStateRepository), new(*repository.MongoChatStateRepository)),
		repository.NewButtonRepository, wire.Bind(new(repository.ButtonRepository), new(*repository.MongoButtonRepository)),
	)
	return nil, nil, nil
}

func ProvideBotList(start *bot.Helper, startScreen *bot.StartScreen, rc *bot.RoomCreating, rsn *bot.RoomSetName, jr *bot.JoinRoom, ar *bot.AllRoom, o *bot.Operation) []bot.Interface {
	return []bot.Interface{start, startScreen, rc, rsn, jr, ar, o}
}
