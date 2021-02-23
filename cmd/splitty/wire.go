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
		service.NewOperationService, wire.Bind(new(bot.OperationService), new(*service.OperationService)),
		service.NewStatisticService, wire.Bind(new(bot.StatisticService), new(*service.StatisticService)),
		wire.Bind(new(events.ChatStateService), new(*service.ChatStateService)),
		wire.Bind(new(events.ButtonService), new(*service.ButtonService)),
		ProvideBotList, bots,
		repository.NewUserRepository, wire.Bind(new(repository.UserRepository), new(*repository.MongoUserRepository)),
		repository.NewRoomRepository, wire.Bind(new(repository.RoomRepository), new(*repository.MongoRoomRepository)),
		repository.NewChatStateRepository, wire.Bind(new(repository.ChatStateRepository), new(*repository.MongoChatStateRepository)),
		repository.NewButtonRepository, wire.Bind(new(repository.ButtonRepository), new(*repository.MongoButtonRepository)),
	)
	return nil, nil, nil
}

var bots = wire.NewSet(
	bot.NewHelper,
	bot.NewStartScreen,
	bot.NewRoomCreating,
	bot.NewRoomSetName,
	bot.NewJoinRoom,
	bot.NewAllRoomInline,
	bot.NewOperation,
	bot.NewWantDonorOperation,
	bot.NewAddDonorOperation,
	bot.NewDonorOperation,
	bot.NewDeleteDonorOperation,
	bot.NewViewRoom,
	bot.NewViewAllOperations,
	bot.NewAllRoom,
	bot.NewChooseRecepientOperation,
	bot.NewWantRecepientOperation,
	bot.NewAddRecepientOperation,
	bot.NewViewUserDebts,
	bot.NewViewAllDebts,
	bot.NewViewSetting,
	bot.NewArchiveRoom,
	bot.NewArchivedRooms,
	bot.NewStatistic,
	bot.NewViewAllDebtOperations,
)

func ProvideBotList(
	b1 *bot.Helper,
	b2 *bot.StartScreen,
	b3 *bot.RoomCreating,
	b4 *bot.RoomSetName,
	b5 *bot.JoinRoom,
	b6 *bot.AllRoomInline,
	b7 *bot.Operation,
	b8 *bot.WantDonorOperation,
	b9 *bot.AddDonorOperation,
	b10 *bot.DonorOperation,
	b11 *bot.DeleteDonorOperation,
	b12 *bot.ViewRoom,
	b13 *bot.ViewAllOperations,
	b14 *bot.AllRoom,
	b15 *bot.ChooseRecepientOperation,
	b16 *bot.WantRecepientOperation,
	b17 *bot.AddRecepientOperation,
	b18 *bot.ViewUserDebts,
	b19 *bot.ViewAllDebts,
	b20 *bot.ViewSetting,
	b21 *bot.ArchiveRoom,
	b22 *bot.ArchivedRooms,
	b23 *bot.Statistic,
	b24 *bot.ViewAllDebtOperations,

) []bot.Interface {
	return []bot.Interface{b1, b2, b3, b4, b5, b6, b7, b8, b9, b10, b11, b12, b13, b14, b15, b16, b17, b18, b19, b20,
		b21, b22, b23, b24}
}
