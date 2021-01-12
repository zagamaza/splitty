package events

import (
	"context"
	"encoding/json"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"log"
	"sync"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"

	"github.com/almaznur91/splitty/internal/bot"
)

//go:generate mockery -inpkg -name tbAPI -case snake
//go:generate mockery -inpkg -name msgLogger -case snake

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI        tbAPI
	MsgLogger    msgLogger
	Bots         bot.Interface
	Debug        bool
	IdleDuration time.Duration
	SuperUsers   SuperUser
	chatID       int64
	Service      service.UserService

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

type msgLogger interface {
	Save(msg *api.Message)
}

// Do process all events, blocked call
func (l *TelegramListener) Do(ctx context.Context) (err error) {

	//if l.chatID, err = l.getChatID(l.Group); err != nil {
	//	return errors.Wrapf(err, "failed to get chat ID for group %q", l.Group)
	//}

	l.msgs.once.Do(func() {
		l.msgs.ch = make(chan api.Response, 100)
		if l.IdleDuration == 0 {
			l.IdleDuration = 30 * time.Second
		}
	})

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

			l.chatID = l.getChatID(update)

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

			//if update.Message.Chat == nil {
			//	log.Print("[DEBUG] ignoring message not from chat")
			//	continue
			//}

			fromChat := update.Message.Chat.ID

			upd := l.transformUpdate(update)
			if fromChat == l.chatID {
				l.MsgLogger.Save(upd.Message) // save an incoming update to report
			}

			if err := l.Service.UpsertUser(ctx, upd.Message.From); err != nil {
				log.Printf("[WARN] failed to respond on update, %v", err)
			}

			log.Printf("[DEBUG] incoming msg: %+v", upd.Message)

			resp := l.Bots.OnMessage(*upd)

			if err := l.sendBotResponse(resp, fromChat); err != nil {
				log.Printf("[ERROR] failed to respond on update, %v", err)
			}

		case resp := <-l.msgs.ch: // publish messages from outside clients
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				log.Printf("[WARN] failed to respond on rtjc event, %v", err)
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bots.OnMessage(api.Update{Message: &api.Message{Text: "idle"}})
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				log.Printf("[WARN] failed to respond on idle, %v", err)
			}
		}
	}
}

// sendBotResponse sends bot'service answer to tg channel and saves it to log
func (l *TelegramListener) sendBotResponse(resp api.Response, chatID int64) error {
	if !resp.Send {
		return nil
	}

	log.Printf("[DEBUG] bot response - %+v, pin: %t", resp.Text, resp.Pin)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.DisableWebPagePreview = !resp.Preview

	tbMsg.ReplyMarkup = resp.Button

	res, err := l.TbAPI.Send(tbMsg)
	if err != nil {
		return errors.Wrapf(err, "can't send message to telegram %q", resp.Text)
	}

	l.saveBotMessage(&res, chatID)

	if resp.Pin {
		_, err = l.TbAPI.PinChatMessage(tbapi.PinChatMessageConfig{ChatID: chatID, MessageID: res.MessageID, DisableNotification: true})
		if err != nil {
			return errors.Wrap(err, "can't pin message to telegram")
		}
	}

	if resp.Unpin {
		_, err = l.TbAPI.UnpinChatMessage(tbapi.UnpinChatMessageConfig{ChatID: chatID})
		if err != nil {
			return errors.Wrap(err, "can't unpin message to telegram")
		}
	}

	return nil
}

// Submit message text to telegram'service group
func (l *TelegramListener) Submit(ctx context.Context, text string, pin bool) error {
	l.msgs.once.Do(func() { l.msgs.ch = make(chan api.Response, 100) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.msgs.ch <- api.Response{Text: text, Pin: pin, Send: true, Preview: true}:
	}
	return nil
}

func (l *TelegramListener) getChatID(update tbapi.Update) int64 {
	var chatId int64
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		chatId = update.CallbackQuery.Message.Chat.ID
	}
	chatId = update.Message.Chat.ID
	return chatId
}

func (l *TelegramListener) saveBotMessage(msg *tbapi.Message, fromChat int64) {
	if fromChat != l.chatID {
		return
	}
	l.MsgLogger.Save(l.transform(msg))
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
			InlineMessageID: u.CallbackQuery.InlineMessageID,
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
