package bot

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"log"
)

type UserService interface {
	UpsertUser(ctx context.Context, u api.User) error
}

type RoomService interface {
	JoinToRoom(ctx context.Context, u api.User, roomId string) error
	CreateRoom(ctx context.Context, u *api.Room) (*api.Room, error)
	FindById(ctx context.Context, id string) (*api.Room, error)
	FindRoomsByUserId(ctx context.Context, id int) (*[]api.Room, error)
	FindRoomsByLikeName(ctx context.Context, name string) (*[]api.Room, error)
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

// OnMessage returns one entry
func (s Helper) OnMessage(ctx context.Context, u *api.Update) (response api.TelegramMessage) {
	if !s.HasReact(u) {
		return api.TelegramMessage{}
	}
	if err := s.us.UpsertUser(ctx, getFrom(u)); err != nil {
		log.Printf("[WARN] failed to respond on update, %v", err)
	}

	return api.TelegramMessage{}
}

// ReactOn keys
func (s Helper) HasReact(u *api.Update) bool {
	return true
}
