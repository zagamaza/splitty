package events

import (
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"log"
	"sync"

	"github.com/almaznur91/splitty/internal/bot"
)

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI   tbAPI
	Bots    bot.Interface
	Service service.UserService

	msgs struct {
		once sync.Once
		ch   chan api.Response
	}
}

type tbAPI interface {
	GetUpdatesChan(config tbapi.UpdateConfig) (tbapi.UpdatesChannel, error)
	Send(c tbapi.Chattable) (tbapi.Message, error)
	PinChatMessage(config tbapi.PinChatMessageConfig) (tbapi.APIResponse, error)
	UnpinChatMessage(config tbapi.UnpinChatMessageConfig) (tbapi.APIResponse, error)
	GetChat(config tbapi.ChatConfig) (tbapi.Chat, error)
	RestrictChatMember(config tbapi.RestrictChatMemberConfig) (tbapi.APIResponse, error)
}

// Do process all events, blocked call
func (l *TelegramListener) Do(ctx context.Context) (err error) {

	u := tbapi.NewUpdate(0)
	u.Timeout = 60

	var updates tbapi.UpdatesChannel
	if updates, err = l.TbAPI.GetUpdatesChan(u); err != nil {
		return errors.Wrap(err, "can't get updates channel")
	}

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case update, ok := <-updates:

			if !ok {
				return errors.Errorf("telegram update chan closed")
			}

			if update.Message == nil {
				log.Print("[DEBUG] empty message body")
				continue
			}

			msgJSON, err := json.Marshal(update.Message)
			if err != nil {
				log.Printf("[ERROR] failed to marshal update.Message to json: %v", err)
				continue
			}
			log.Printf("[DEBUG] %service", string(msgJSON))

			upd := l.transformUpdate(update)

			if err := l.Service.UpsertUser(ctx, upd.Message.From); err != nil {
				log.Printf("[WARN] failed to respond on update, %v", err)
			}

			log.Printf("[DEBUG] incoming msg: %+v", upd.Message)

			resp := l.Bots.OnMessage(ctx, upd)

			if err := l.sendBotResponse(resp); err != nil {
				log.Printf("[ERROR] failed to respond on update, %v", err)
			}
		}
	}
}

// sendBotResponse sends bot'service answer to tg channel and saves it to log
func (l *TelegramListener) sendBotResponse(resp api.TelegramMessage) error {
	if !resp.Send {
		return nil
	}

	if resp.InlineConfig != nil {
		log.Printf("[INFO] bot response - %+v", resp.InlineConfig.InlineQueryID)
	}

	if len(resp.Chattable) > 0 {
		for _, v := range resp.Chattable {
			log.Printf("[INFO] bot response - %v", v)
			_, err := l.TbAPI.Send(v)
			if err != nil {
				return errors.Wrapf(err, "can't send message to telegram %q", v)
			}
		}
	}
	return nil
}

func (l *TelegramListener) transform(msg *tbapi.Message) *api.Message {
	if msg == nil {
		return nil
	}
	message := api.Message{
		ID:   msg.MessageID,
		Sent: msg.Time(),
		Text: msg.Text,
	}

	message.Chat = &api.Chat{
		ID:   msg.Chat.ID,
		Type: msg.Chat.Type,
	}

	if msg.From != nil {
		message.From = api.User{
			ID:          msg.From.ID,
			Username:    msg.From.UserName,
			DisplayName: msg.From.FirstName + " " + msg.From.LastName,
		}
	}

	switch {
	case msg.Entities != nil && len(*msg.Entities) > 0:
		message.Entities = l.transformEntities(msg.Entities)

	case msg.Photo != nil && len(*msg.Photo) > 0:
		sizes := *msg.Photo
		lastSize := sizes[len(sizes)-1]
		message.Image = &api.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: l.transformEntities(msg.CaptionEntities),
		}
	}

	return &message
}

func (l *TelegramListener) transformUpdate(u tbapi.Update) *api.Update {
	update := &api.Update{}

	if u.CallbackQuery != nil {
		update.CallbackQuery = &api.CallbackQuery{
			ID: u.CallbackQuery.ID,
			From: api.User{
				ID:          u.CallbackQuery.From.ID,
				Username:    u.CallbackQuery.From.UserName,
				DisplayName: u.CallbackQuery.From.FirstName + " " + u.CallbackQuery.From.LastName,
			},
			Message:         l.transform(u.CallbackQuery.Message),
			InlineMessageID: u.CallbackQuery.InlineMessageID,
		}
	}

	if u.InlineQuery != nil {
		i := u.InlineQuery
		update.InlineQuery = &api.InlineQuery{
			ID:     i.ID,
			Query:  i.Query,
			Offset: i.Offset,
			From: api.User{
				ID:          i.From.ID,
				Username:    i.From.UserName,
				DisplayName: i.From.FirstName + " " + i.From.LastName,
			},
		}
	}
	update.Message = l.transform(u.Message)
	return update
}

func (l *TelegramListener) transformEntities(entities *[]tbapi.MessageEntity) *[]api.Entity {
	if entities == nil || len(*entities) == 0 {
		return nil
	}

	var result []api.Entity
	for _, entity := range *entities {
		e := api.Entity{
			Type:   entity.Type,
			Offset: entity.Offset,
			Length: entity.Length,
			URL:    entity.URL,
		}
		if entity.User != nil {
			e.User = &api.User{
				ID:          entity.User.ID,
				Username:    entity.User.UserName,
				DisplayName: entity.User.FirstName + " " + entity.User.LastName,
			}
		}
		result = append(result, e)
	}

	return &result
}
