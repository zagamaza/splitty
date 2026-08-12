package events

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/safe"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type ChatStateService interface {
	FindByUserId(ctx context.Context, userId int) (*api.ChatState, error)
}

type ButtonService interface {
	FindById(ctx context.Context, id string) (*api.Button, error)
}

type UserService interface {
	UpsertUser(ctx context.Context, u api.User) (*api.User, error)
	// UpsertTelegramUser резолвит личность по telegram_id и возвращает
	// КАНОНИЧЕСКИЙ документ пользователя. UpsertUser здесь не годится: он ищет
	// по _id, а _id равен telegram id только у исторических аккаунтов — у
	// пришедшего через Google и привязавшего telegram он синтетический, и
	// UpsertUser завёл бы ему второй профиль
	UpsertTelegramUser(ctx context.Context, tgID int, username, displayName, userLang string) (*api.User, error)
}

type DeIntegrationService interface {
	StartPostScheduler()
}

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI            tbAPI
	Bots             bot.Interface
	ChatStateService ChatStateService
	ButtonService    ButtonService
	upds             chan tbapi.Update
	UserService      UserService

	DeIntegrationService DeIntegrationService
}

type tbAPI interface {
	GetUpdatesChan(config tbapi.UpdateConfig) (tbapi.UpdatesChannel, error)
	Send(c tbapi.Chattable) (tbapi.Message, error)
	PinChatMessage(config tbapi.PinChatMessageConfig) (tbapi.APIResponse, error)
	UnpinChatMessage(config tbapi.UnpinChatMessageConfig) (tbapi.APIResponse, error)
	GetChat(config tbapi.ChatConfig) (tbapi.Chat, error)
	RestrictChatMember(config tbapi.RestrictChatMemberConfig) (tbapi.APIResponse, error)
	AnswerInlineQuery(config tbapi.InlineConfig) (tbapi.APIResponse, error)
	AnswerCallbackQuery(config tbapi.CallbackConfig) (tbapi.APIResponse, error)
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

			l.handleUpdate(ctx, update)
		}
	}
}

// handleUpdate обрабатывает ОДНО обновление. Паника внутри не выходит наружу:
// бот и REST живут в одном процессе, и одно кривое сообщение (пост в канале,
// анонимный админ, неожиданная форма апдейта) уносило вместе с собой и
// приложение — все теряли доступ из-за одного отправителя.
func (l *TelegramListener) handleUpdate(ctx context.Context, update tbapi.Update) {
	defer safe.Recover("обработка обновления telegram")

	upd := transformUpdate(update)

	user, err := getFrom(upd)
	if err != nil {
		log.Error().Err(err).Stack().Msg("failed define user")
		return
	}

	// user — СЫРОЙ пользователь из апдейта, его ID это telegram id.
	// Дальше по коду бота законен только upd.User: у него ID — номер
	// Splitty, который у google-первого аккаунта с привязанным telegram
	// telegram-овскому id не равен
	upd.User, err = l.UserService.UpsertTelegramUser(ctx, user.ID, user.Username, user.DisplayName, user.UserLang)
	if err != nil {
		log.Error().Err(err).Stack().Msgf("failed to upsert user, %v", err)
		return
	}

	if err := l.populateBtn(ctx, upd); err != nil {
		log.Error().Err(err).Stack().Msgf("failed to populateBtn, %v", err)
	}

	if err := l.populateChatState(ctx, upd); err != nil {
		log.Error().Err(err).Stack().Msgf("failed to populateChatState")
	}

	log.Debug().Msgf("incoming msg: %+v; btn:%+v", upd.Message, upd.Button)

	l.processUpdate(ctx, upd)
}

func (l *TelegramListener) processUpdate(ctx context.Context, upd *api.Update) {
	resp := l.Bots.OnMessage(ctx, upd)
	l.sendBotResponse(ctx, resp)
}

func (l *TelegramListener) populateBtn(ctx context.Context, upd *api.Update) error {
	if upd.CallbackQuery != nil {
		id := upd.CallbackQuery.Data
		btn, err := l.ButtonService.FindById(ctx, id)
		if err != nil {
			return errors.Wrapf(err, "failed to find Button by id %s, %q", id, err)
		}
		upd.Button = btn
	}
	return nil
}

// populateChatState подтягивает незавершённый многошаговый сценарий бота.
//
// Ключ — КАНОНИЧЕСКИЙ номер Splitty (upd.User.ID), а не сырой telegram id из
// апдейта. Часть экранов и раньше сохраняла состояние по u.User.ID, и находилось
// оно лишь потому, что сегодня _id == telegram id. У пользователя, пришедшего
// через Google и привязавшего telegram, _id ≥ 10^12 при telegram id порядка
// 10^9 — состояния, записанные по канонику, по сырому id не нашлись бы никогда,
// и многошаговые сценарии (ввод суммы, добавление файла) молча ломались бы.
//
// Поиск по сырому telegram id — ПЕРЕХОДНЫЙ fallback: в момент выкатки у людей
// есть незавершённые сценарии, записанные по telegram/chat id. Убрать можно,
// когда такие состояния протухнут.
func (l *TelegramListener) populateChatState(ctx context.Context, upd *api.Update) error {
	var rawTelegramID int
	if upd.Message != nil {
		rawTelegramID = upd.Message.From.ID
	} else if upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil {
		rawTelegramID = upd.CallbackQuery.From.ID
	}
	if rawTelegramID == 0 {
		return nil
	}

	userId := rawTelegramID
	if upd.User != nil {
		userId = upd.User.ID
	}

	cs, err := l.ChatStateService.FindByUserId(ctx, userId)
	if err != nil {
		return errors.Wrapf(err, "failed to find ChatState by id %q", err)
	}
	if cs == nil && userId != rawTelegramID {
		if cs, err = l.ChatStateService.FindByUserId(ctx, rawTelegramID); err != nil {
			return errors.Wrapf(err, "failed to find ChatState by telegram id %q", err)
		}
	}
	upd.ChatState = cs
	return nil
}

// sendBotResponse sends bot'service answer to tg channel and saves it to log
func (l *TelegramListener) sendBotResponse(ctx context.Context, resp api.TelegramMessage) {
	if !resp.Send {
		return
	}

	if resp.Redirect != nil {
		if resp.Redirect.FromRedirect {
			log.Error().Stack().Msg("recursive multiple redirection")
		} else {
			resp.Redirect.FromRedirect = true
			l.processUpdate(ctx, resp.Redirect)
		}
	}

	if resp.InlineConfig != nil {
		response, err := l.TbAPI.AnswerInlineQuery(*resp.InlineConfig)
		if err != nil {
			log.Error().Err(err).Msgf("can't send query to telegram %v", response)
		}
		log.Debug().Msgf("bot response - %v", resp.InlineConfig)
	}

	if len(resp.Chattable) > 0 {
		for _, v := range resp.Chattable {
			if v == nil {
				continue
			}
			response, err := l.TbAPI.Send(v)
			if err != nil {
				log.Error().Err(err).Msgf("can't send message to telegram %v", v)
			}
			log.Debug().Msgf("bot response chat - %v, text - %v, messageId - %v", response.Chat, response.Text, response.MessageID)
		}
	}
	if resp.CallbackConfig != nil {
		response, err := l.TbAPI.AnswerCallbackQuery(*resp.CallbackConfig)
		if err != nil {
			log.Error().Err(err).Msgf("can't send calback to telegram %v", resp.CallbackConfig)
		}
		log.Debug().Msgf("bot response - %+v", response)
	}
}

func transform(msg *tbapi.Message) *api.Message {
	if msg == nil {
		return nil
	}
	message := api.Message{
		ID:   msg.MessageID,
		Sent: msg.Time(),
		Text: msg.Text,
	}

	// Chat у сообщения бывает пустым: telegram отдаёт такие апдейты, а разыменование
	// роняло весь процесс
	if msg.Chat != nil {
		message.Chat = &api.Chat{
			ID:   msg.Chat.ID,
			Type: msg.Chat.Type,
		}
	}

	if msg.From != nil {
		message.From = transformUser(msg.From)
	}

	switch {
	case msg.Entities != nil && len(*msg.Entities) > 0:
		message.Entities = transformEntities(msg.Entities)

	case msg.Document != nil:
		message.Document = &api.Document{
			FileID:   msg.Document.FileID,
			FileSize: msg.Document.FileSize,
			MimeType: msg.Document.MimeType,
		}

	case msg.Video != nil:
		message.Video = &api.Video{
			FileID:   msg.Video.FileID,
			FileSize: msg.Video.FileSize,
			MimeType: msg.Video.MimeType,
		}

	case msg.Photo != nil && len(*msg.Photo) > 0:
		sizes := *msg.Photo
		lastSize := sizes[len(sizes)-1]
		message.Image = &api.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: transformEntities(msg.CaptionEntities),
		}
	}

	return &message
}

func transformUpdate(u tbapi.Update) *api.Update {
	update := &api.Update{}

	if u.CallbackQuery != nil {
		update.CallbackQuery = &api.CallbackQuery{
			ID:              u.CallbackQuery.ID,
			From:            transformUser(u.CallbackQuery.From),
			Message:         transform(u.CallbackQuery.Message),
			InlineMessageID: u.CallbackQuery.InlineMessageID,
			Data:            u.CallbackQuery.Data,
		}
	}

	if u.InlineQuery != nil {
		i := u.InlineQuery
		update.InlineQuery = &api.InlineQuery{
			ID:     i.ID,
			Query:  i.Query,
			Offset: i.Offset,
			From:   transformUser(i.From),
		}
	}

	if u.EditedMessage != nil {
		update.Message = transform(u.EditedMessage)
	}

	if u.Message != nil {
		update.Message = transform(u.Message)
	}
	return update
}

// transformUser переносит отправителя. Пустой From — не исключение: посты в
// канале и анонимные админы приходят без него, и разыменование здесь роняло
// процесс целиком
func transformUser(i *tbapi.User) api.User {
	if i == nil {
		return api.User{}
	}
	return api.User{
		ID:          i.ID,
		Username:    i.UserName,
		DisplayName: i.FirstName + " " + i.LastName,
		UserLang:    i.LanguageCode,
	}
}

func transformEntities(entities *[]tbapi.MessageEntity) *[]api.Entity {
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

func getFrom(update *api.Update) (*api.User, error) {
	var user api.User
	if update.CallbackQuery != nil {
		user = update.CallbackQuery.From
	} else if update.Message != nil {
		user = update.Message.From
	} else if update.InlineQuery != nil {
		user = update.InlineQuery.From
	} else {
		return nil, errors.Errorf("Not define user, update - %v", update)
	}
	// Нулевой id — отправителя не было вовсе (пост в канале, анонимный админ).
	// Раньше такой апдейт заводил пользователя с telegram id 0 и шёл дальше по
	// экранам от его имени
	if user.ID == 0 {
		return nil, errors.Errorf("update without sender - %v", update)
	}
	return &user, nil
}
