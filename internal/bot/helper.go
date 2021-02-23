package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

type UserService interface {
	UpsertUser(ctx context.Context, u api.User) error
	FindById(ctx context.Context, id int) (*api.User, error)
}

type RoomService interface {
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	CreateRoom(ctx context.Context, u *api.Room) (*api.Room, error)
	FindById(ctx context.Context, id string) (*api.Room, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindArchivedRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, userId int, name string) (*[]api.Room, error)
	ArchiveRoom(ctx context.Context, userId int, roomId string) error
	UnArchiveRoom(ctx context.Context, userId int, roomId string) error
}

type Config struct {
	BotName    string
	SuperUsers []string
}

// Helper for update user
type Helper struct {
	us  UserService
	rs  RoomService
	cgf *Config
}

func NewHelper(s UserService, rs RoomService, cfg *Config) *Helper {
	return &Helper{
		us:  s,
		rs:  rs,
		cgf: cfg,
	}
}

// ReactOn keys
func (s Helper) HasReact(u *api.Update) bool {
	return u.CallbackQuery != nil || u.Message != nil || u.InlineQuery != nil
}

// OnMessage returns one entry
func (s Helper) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {

	if err := s.us.UpsertUser(ctx, *getFrom(u)); err != nil {
		log.Error().Err(err).Msgf("failed to respond on update, %v", err)
	}

	return api.TelegramMessage{}
}
