package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

func ToUserEntity(ctx context.Context, u *tbapi.User) *api.User {
	q := &api.User{
		ID:          u.ID,
		Username:    u.UserName,
		DisplayName: u.FirstName + " " + u.LastName,
	}
	return q
}
