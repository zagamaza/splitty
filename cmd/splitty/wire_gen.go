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
	allRoomInline := bot.NewAllRoomInline(chatStateService, buttonService, roomService, botConfig)
	operation := bot.NewOperation(chatStateService, buttonService, roomService, botConfig)
	operationService := service.NewOperationService(mongoRoomRepository)
	wantDonorOperation := bot.NewWantDonorOperation(chatStateService, buttonService, operationService, roomService, botConfig)
	addDonorOperation := bot.NewAddDonorOperation(chatStateService, buttonService, operationService, roomService, botConfig)
	donorOperation := bot.NewDonorOperation(buttonService, operationService, roomService, botConfig)
	deleteDonorOperation := bot.NewDeleteDonorOperation(chatStateService, buttonService, operationService, botConfig)
	viewRoom := bot.NewViewRoom(buttonService, roomService, chatStateService, botConfig)
	viewAllOperations := bot.NewViewAllOperations(chatStateService, buttonService, operationService, botConfig)
	allRoom := bot.NewAllRoom(chatStateService, buttonService, roomService, botConfig)
	v := ProvideBotList(helper, startScreen, roomCreating, roomSetName, joinRoom, allRoomInline, operation, wantDonorOperation, addDonorOperation, donorOperation, deleteDonorOperation, viewRoom, viewAllOperations, allRoom)
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

func ProvideBotList(helper *bot.Helper, startScreen *bot.StartScreen, rc *bot.RoomCreating, rsn *bot.RoomSetName,
	jr *bot.JoinRoom, ari *bot.AllRoomInline, o *bot.Operation, do *bot.WantDonorOperation, ado *bot.AddDonorOperation,
	cdo *bot.DonorOperation, ddo *bot.DeleteDonorOperation, vr *bot.ViewRoom, vaop *bot.ViewAllOperations,
	ar *bot.AllRoom) []bot.Interface {
	return []bot.Interface{helper, startScreen, rc, rsn, jr, ari, o, do, ado, cdo, ddo, vr, vaop, ar}
}
