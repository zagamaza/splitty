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
		wire.Bind(new(events.ChatStateService), new(*service.ChatStateService)),
		wire.Bind(new(events.ButtonService), new(*service.ButtonService)),
		ProvideBotList, bot.NewHelper, bot.NewStartScreen, bot.NewRoomCreating, bot.NewRoomSetName, bot.NewJoinRoom,
		bot.NewAllRoomInline, bot.NewOperation, bot.NewWantDonorOperation, bot.NewAddDonorOperation, bot.NewDonorOperation,
		bot.NewDeleteDonorOperation, bot.NewViewRoom, bot.NewViewAllOperations, bot.NewAllRoom, bot.NewChooseRecepientOperation,
		bot.NewWantRecepientOperation, bot.NewAddRecepientOperation, bot.NewViewUserDebts, bot.NewViewAllDebts,
		repository.NewUserRepository, wire.Bind(new(repository.UserRepository), new(*repository.MongoUserRepository)),
		repository.NewRoomRepository, wire.Bind(new(repository.RoomRepository), new(*repository.MongoRoomRepository)),
		repository.NewChatStateRepository, wire.Bind(new(repository.ChatStateRepository), new(*repository.MongoChatStateRepository)),
		repository.NewButtonRepository, wire.Bind(new(repository.ButtonRepository), new(*repository.MongoButtonRepository)),
	)
	return nil, nil, nil
}

func ProvideBotList(helper *bot.Helper, startScreen *bot.StartScreen, rc *bot.RoomCreating, rsn *bot.RoomSetName,
	jr *bot.JoinRoom, ari *bot.AllRoomInline, o *bot.Operation, do *bot.WantDonorOperation, ado *bot.AddDonorOperation,
	cdo *bot.DonorOperation, ddo *bot.DeleteDonorOperation, vr *bot.ViewRoom, vaop *bot.ViewAllOperations,
	ar *bot.AllRoom, cro *bot.ChooseRecepientOperation, wro *bot.WantRecepientOperation, aro *bot.AddRecepientOperation,
	vud *bot.ViewUserDebts, vad *bot.ViewAllDebts) []bot.Interface {

	return []bot.Interface{helper, startScreen, rc, rsn, jr, ari, o, do, ado, cdo, ddo, vr, vaop, ar, cro, wro, aro, vud, vad}
}
