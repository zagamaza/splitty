package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/repository"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

func ToUserEntity(ctx context.Context, u *tbapi.User) *repository.User {
	q := &repository.User{
		ID:          u.ID,
		Username:    u.UserName,
		DisplayName: u.FirstName + " " + u.LastName,
	}
	return q
}
