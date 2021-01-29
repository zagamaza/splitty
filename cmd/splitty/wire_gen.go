// Code generated by Wire. DO NOT EDIT.

//go:generate wire
//+build !wireinject

package main

import (
	"context"
	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/events"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"
)

// Injectors from wire.go:

func initApp(ctx context.Context, cfg *config) (*events.TelegramListener, func(), error) {
	botConfig := initBotConfig(cfg)
	botAPI, err := initTelegramApi(cfg, botConfig)
	if err != nil {
		return nil, nil, err
	}
	database, cleanup, err := initMongoConnection(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	mongoUserRepository := repository.NewUserRepository(database)
	userService := service.NewUserService(mongoUserRepository)
	mongoRoomRepository := repository.NewRoomRepository(database)
	roomService := service.NewRoomService(mongoRoomRepository)
	helper := bot.NewHelper(userService, roomService, botConfig)
	mongoChatStateRepository := repository.NewChatStateRepository(database)
	chatStateService := service.NewChatStateService(mongoChatStateRepository)
	mongoButtonRepository := repository.NewButtonRepository(database)
	buttonService := service.NewButtonService(mongoButtonRepository)
	startScreen := bot.NewStartScreen(chatStateService, buttonService, botConfig)
	roomCreating := bot.NewRoomCreating(chatStateService, buttonService, botConfig)
	roomSetName := bot.NewRoomSetName(chatStateService, buttonService, roomService, botConfig)
	joinRoom := bot.NewJoinRoom(chatStateService, buttonService, roomService, botConfig)
	allRoom := bot.NewAllRoom(chatStateService, buttonService, roomService, botConfig)
	operation := bot.NewOperation(chatStateService, buttonService, roomService, botConfig)
	v := ProvideBotList(helper, startScreen, roomCreating, roomSetName, joinRoom, allRoom, operation)
	telegramListener, err := initTelegramConfig(botAPI, v, buttonService, chatStateService)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return telegramListener, func() {
		cleanup()
	}, nil
}

// wire.go:

func ProvideBotList(helper *bot.Helper, startScreen *bot.StartScreen, rc *bot.RoomCreating, rsn *bot.RoomSetName, jr *bot.JoinRoom, ar *bot.AllRoom, o *bot.Operation) []bot.Interface {
	return []bot.Interface{helper, startScreen, rc, rsn, jr, ar, o}
}
