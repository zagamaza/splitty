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
	"github.com/google/wire"
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
	mongoChatStateRepository := repository.NewChatStateRepository(database)
	chatStateService := service.NewChatStateService(mongoChatStateRepository)
	mongoButtonRepository := repository.NewButtonRepository(database)
	buttonService := service.NewButtonService(mongoButtonRepository)
	mongoRoomRepository := repository.NewRoomRepository(database)
	operationService := service.NewOperationService(mongoRoomRepository)
	operation := bot.NewOperation(chatStateService, buttonService, operationService, botConfig)
	startScreen := bot.NewStartScreen(chatStateService, buttonService, botConfig)
	roomCreating := bot.NewRoomCreating(chatStateService, buttonService, botConfig)
	roomService := service.NewRoomService(mongoRoomRepository)
	roomSetName := bot.NewRoomSetName(chatStateService, buttonService, roomService, botConfig)
	joinRoom := bot.NewJoinRoom(chatStateService, buttonService, roomService, botConfig)
	statisticService := service.NewStatisticService(mongoRoomRepository, operationService)
	allRoomInline := bot.NewAllRoomInline(chatStateService, buttonService, roomService, statisticService, botConfig)
	wantDonorOperation := bot.NewWantDonorOperation(chatStateService, buttonService, operationService, roomService, botConfig)
	addDonorOperation := bot.NewAddDonorOperation(chatStateService, buttonService, operationService, roomService, botConfig)
	editDonorOperation := bot.NewEditDonorOperation(buttonService, operationService, roomService, botConfig)
	deleteDonorOperation := bot.NewDeleteDonorOperation(chatStateService, buttonService, operationService, botConfig)
	viewRoom := bot.NewViewRoom(buttonService, roomService, chatStateService, botConfig)
	viewAllOperations := bot.NewViewAllOperations(chatStateService, buttonService, operationService, botConfig)
	allRoom := bot.NewAllRoom(chatStateService, buttonService, roomService, botConfig)
	chooseRecepientOperation := bot.NewChooseRecepientOperation(chatStateService, buttonService, operationService, roomService, botConfig)
	wantReturnDebt := bot.NewWantReturnDebt(chatStateService, buttonService, operationService, roomService, botConfig)
	mongoUserRepository := repository.NewUserRepository(database)
	userService := service.NewUserService(mongoUserRepository)
	addRecepientOperation := bot.NewAddRecepientOperation(chatStateService, buttonService, operationService, userService, roomService, botConfig)
	viewUserDebts := bot.NewViewUserDebts(chatStateService, buttonService, operationService, botConfig)
	viewAllDebts := bot.NewViewAllDebts(chatStateService, buttonService, operationService, botConfig)
	roomSetting := bot.NewRoomSetting(buttonService, roomService, chatStateService, botConfig)
	archiveRoom := bot.NewArchiveRoom(buttonService, roomService, chatStateService, botConfig, roomSetting)
	archivedRooms := bot.NewArchivedRooms(chatStateService, buttonService, roomService, botConfig)
	statistic := bot.NewStatistic(buttonService, roomService, chatStateService, statisticService, botConfig)
	viewAllDebtOperations := bot.NewViewAllDebtOperations(chatStateService, buttonService, operationService, botConfig)
	viewMyOperations := bot.NewViewMyOperations(chatStateService, buttonService, operationService, botConfig)
	debt := bot.NewDebt(chatStateService, buttonService, operationService, botConfig)
	userSetting := bot.NewUserSetting(buttonService, userService, chatStateService, botConfig)
	chooseLanguage := bot.NewChooseLanguage(buttonService, roomService, chatStateService, botConfig)
	operationAdded := bot.NewOperationAdded(chatStateService, buttonService, roomService, operationService, userService, botConfig)
	chooseNotification := bot.NewChooseNotification(buttonService, chatStateService, botConfig)
	selectedNotification := bot.NewSelectedNotification(buttonService, userService, chatStateService, botConfig)
	debtReturned := bot.NewDebtReturned()
	wantAddFileToOperation := bot.NewWantAddFileToOperation(chatStateService, buttonService, roomService, operationService, botConfig)
	addFileToOperation := bot.NewAddFileToOperation(chatStateService, buttonService, roomService, operationService, botConfig)
	viewFileOperation := bot.NewViewFileOperation(chatStateService, buttonService, roomService, operationService, botConfig)
	viewDonorOperation := bot.NewViewDonorOperation(buttonService, operationService, roomService, botConfig)
	selectedLeaveRoom := bot.NewSelectedLeaveRoom(buttonService, userService, roomService, chatStateService, botConfig)
	viewOperationsWithMe := bot.NewViewOperationsWithMe(chatStateService, buttonService, operationService, botConfig)
	chooseCountInPage := bot.NewChooseCountInPage(buttonService, chatStateService, userService, botConfig)
	v := ProvideBotList(operation, startScreen, roomCreating, roomSetName, joinRoom, allRoomInline, wantDonorOperation, addDonorOperation, editDonorOperation, deleteDonorOperation, viewRoom, viewAllOperations, allRoom, chooseRecepientOperation, wantReturnDebt, addRecepientOperation, viewUserDebts, viewAllDebts, roomSetting, archiveRoom, archivedRooms, statistic, viewAllDebtOperations, viewMyOperations, debt, userSetting, chooseLanguage, operationAdded, chooseNotification, selectedNotification, debtReturned, wantAddFileToOperation, addFileToOperation, viewFileOperation, viewDonorOperation, selectedLeaveRoom, viewOperationsWithMe, chooseCountInPage)
	telegramListener, err := initTelegramConfig(botAPI, v, buttonService, userService, chatStateService)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return telegramListener, func() {
		cleanup()
	}, nil
}

// wire.go:

var bots = wire.NewSet(bot.NewStartScreen, bot.NewRoomCreating, bot.NewRoomSetName, bot.NewJoinRoom, bot.NewAllRoomInline, bot.NewWantDonorOperation, bot.NewAddDonorOperation, bot.NewEditDonorOperation, bot.NewDeleteDonorOperation, bot.NewViewRoom, bot.NewViewAllOperations, bot.NewAllRoom, bot.NewChooseRecepientOperation, bot.NewWantReturnDebt, bot.NewAddRecepientOperation, bot.NewViewUserDebts, bot.NewViewAllDebts, bot.NewRoomSetting, bot.NewArchiveRoom, bot.NewArchivedRooms, bot.NewStatistic, bot.NewViewAllDebtOperations, bot.NewOperation, bot.NewViewMyOperations, bot.NewDebt, bot.NewUserSetting, bot.NewChooseLanguage, bot.NewOperationAdded, bot.NewChooseNotification, bot.NewSelectedNotification, bot.NewDebtReturned, bot.NewWantAddFileToOperation, bot.NewAddFileToOperation, bot.NewViewFileOperation, bot.NewViewDonorOperation, bot.NewSelectedLeaveRoom, bot.NewViewOperationsWithMe, bot.NewChooseCountInPage)

func ProvideBotList(
	b1 *bot.Operation,
	b2 *bot.StartScreen,
	b3 *bot.RoomCreating,
	b4 *bot.RoomSetName,
	b5 *bot.JoinRoom,
	b6 *bot.AllRoomInline,
	b8 *bot.WantDonorOperation,
	b9 *bot.AddDonorOperation,
	b10 *bot.EditDonorOperation,
	b11 *bot.DeleteDonorOperation,
	b12 *bot.ViewRoom,
	b13 *bot.ViewAllOperations,
	b14 *bot.AllRoom,
	b15 *bot.ChooseRecepientOperation,
	b16 *bot.WantReturnDebt,
	b17 *bot.AddRecepientOperation,
	b18 *bot.ViewUserDebts,
	b19 *bot.ViewAllDebts,
	b20 *bot.RoomSetting,
	b21 *bot.ArchiveRoom,
	b22 *bot.ArchivedRooms,
	b23 *bot.Statistic,
	b24 *bot.ViewAllDebtOperations,
	b25 *bot.ViewMyOperations,
	b26 *bot.Debt,
	b27 *bot.UserSetting,
	b28 *bot.ChooseLanguage,
	b29 *bot.OperationAdded,
	b30 *bot.ChooseNotification,
	b31 *bot.SelectedNotification,
	b32 *bot.DebtReturned,
	b33 *bot.WantAddFileToOperation,
	b34 *bot.AddFileToOperation,
	b35 *bot.ViewFileOperation,
	b36 *bot.ViewDonorOperation,
	b37 *bot.SelectedLeaveRoom,
	b38 *bot.ViewOperationsWithMe,
	b39 *bot.ChooseCountInPage,
) []bot.Interface {
	return []bot.Interface{b1, b2, b3, b4, b5, b6, b8, b9, b10, b11, b12, b13, b14, b15, b16, b17, b18, b19, b20,
		b21, b22, b23, b24, b25, b26, b27, b28, b29, b30, b31, b32, b33, b34, b35, b36, b37, b38, b39}
}
