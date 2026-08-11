package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoInviteRepository хранит отношения «пользователь × комната» (коллекция
// room_invite): кто кого позвал, в каком состоянии отношение сейчас.
type MongoInviteRepository struct {
	col *mongo.Collection
}

func NewInviteRepository(db *mongo.Database) *MongoInviteRepository {
	return &MongoInviteRepository{col: db.Collection("room_invite")}
}

// EnsureIndexes создаёт индексы коллекции room_invite. Идемпотентно; вызывать
// при старте.
//   - уникальный по (room_id, invitee_id): запись описывает ТЕКУЩЕЕ состояние
//     отношения, а не историю, поэтому вторая запись на пару — всегда баг.
//     Без индекса конкурентные приглашения одного человека дали бы дубли, и
//     Find возвращал бы произвольный из них;
//   - по invitee_id: ListForUser читает раздел уведомлений на каждый его показ,
//     без индекса это полный скан растущей коллекции.
func (r MongoInviteRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "room_id", Value: ascParameter}, {Key: "invitee_id", Value: ascParameter}},
			Options: options.Index().SetUnique(true).SetName("uniq_room_invitee"),
		},
		{
			Keys:    bson.D{{Key: "invitee_id", Value: ascParameter}},
			Options: options.Index().SetName("idx_invitee"),
		},
	})
	return err
}

// versionFilter — условие «версия записи ровно такая, какой мы её прочитали».
// Нулевая версия значит «поля version у записи не было» (она создана до его
// появления), а равенство нулю такую запись НЕ находит: отсутствующее поле в
// mongo сравнимо только с null. Отсюда $in.
func versionFilter(version int) interface{} {
	if version == 0 {
		return bson.M{"$in": bson.A{0, nil}}
	}
	return version
}

// Upsert записывает состояние отношения, создавая запись или обновляя
// существующую. CreatedAt всегда обновляется на now: смена отношения — это
// новое событие для раздела уведомлений (см. комментарий у api.RoomInvite).
// Version растёт на каждую запись — по нему условная запись отличает «с момента
// чтения никто не писал» от «писали».
func (r MongoInviteRepository) Upsert(ctx context.Context, roomID primitive.ObjectID, inviteeID, inviterID int, status api.InviteStatus, now time.Time) error {
	filter := bson.M{"room_id": roomID, "invitee_id": inviteeID}
	update := bson.M{
		"$set": bson.M{
			"inviter_id": inviterID,
			"status":     status,
			"created_at": now,
		},
		"$inc": bson.M{"version": 1},
	}
	if _, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true)); err != nil {
		log.Error().Err(err).Msg("upsert room invite failed")
		return err
	}
	return nil
}

// UpsertIfUnchanged записывает состояние отношения, ТОЛЬКО если запись не
// менялась с момента чтения: since — сама прочитанная запись, nil означает
// «записи не было» (тогда она создаётся, и лишь если её не создали параллельно).
// false — с момента чтения кто-то записал более свежее решение, и затирать его
// нельзя.
//
// Нужен примирению записи по снимку комнаты: снимок стареет, а решение по нему
// пишется безусловным Upsert'ом и затирало бы, например, приглашение, выданное
// вышедшему человеку уже после его выхода — карточка «Принять» исчезала бы,
// хотя уведомление о приглашении ушло.
func (r MongoInviteRepository) UpsertIfUnchanged(ctx context.Context, roomID primitive.ObjectID, inviteeID, inviterID int,
	status api.InviteStatus, since *api.RoomInvite, now time.Time) (bool, error) {
	fields := bson.M{"inviter_id": inviterID, "status": status, "created_at": now}
	if since == nil {
		// Версию ставим здесь же: $inc рядом с $setOnInsert бил бы счётчик и при
		// совпадении фильтра, то есть «не записали, но версию сдвинули»
		insert := bson.M{}
		for k, v := range fields {
			insert[k] = v
		}
		insert["version"] = 1
		res, err := r.col.UpdateOne(ctx, bson.M{"room_id": roomID, "invitee_id": inviteeID},
			bson.M{"$setOnInsert": insert}, options.Update().SetUpsert(true))
		if err != nil {
			log.Error().Err(err).Msg("insert room invite failed")
			return false, err
		}
		return res.UpsertedCount > 0, nil
	}
	filter := bson.M{"room_id": roomID, "invitee_id": inviteeID, "version": versionFilter(since.Version)}
	res, err := r.col.UpdateOne(ctx, filter, bson.M{"$set": fields, "$inc": bson.M{"version": 1}})
	if err != nil {
		log.Error().Err(err).Msg("conditional upsert room invite failed")
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// Find возвращает отношение по паре или mongo.ErrNoDocuments, если его нет.
func (r MongoInviteRepository) Find(ctx context.Context, roomID primitive.ObjectID, inviteeID int) (*api.RoomInvite, error) {
	var inv api.RoomInvite
	if err := r.col.FindOne(ctx, bson.M{"room_id": roomID, "invitee_id": inviteeID}).Decode(&inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// ListForUser возвращает отношения пользователя, которые показываются в разделе
// уведомлений: pending (ждут решения) и added (карточка «вас добавили»).
// left и declined — завершённые состояния, в разделе им делать нечего.
func (r MongoInviteRepository) ListForUser(ctx context.Context, userID int) ([]api.RoomInvite, error) {
	filter := bson.M{
		"invitee_id": userID,
		"status":     bson.M{"$in": bson.A{api.InvitePending, api.InviteAdded}},
	}
	cur, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var invites []api.RoomInvite
	if err = cur.All(ctx, &invites); err != nil {
		return nil, err
	}
	return invites, nil
}

// SetStatusIfCurrent переводит отношение из ожидаемого состояния в новое и
// возвращает false, если текущее состояние оказалось другим.
//
// Compare-and-set, а не безусловная запись: конкурентные «принять» и
// «отклонить» одного приглашения по last-write-wins дали бы противоречивое
// состояние вроде «участник комнаты со статусом declined». Проигравший гонку
// получает false и отвечает 409, ничего не меняя.
func (r MongoInviteRepository) SetStatusIfCurrent(ctx context.Context, roomID primitive.ObjectID, inviteeID int, from, to api.InviteStatus, now time.Time) (bool, error) {
	filter := bson.M{"room_id": roomID, "invitee_id": inviteeID, "status": from}
	update := bson.M{"$set": bson.M{"status": to, "created_at": now}, "$inc": bson.M{"version": 1}}
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Error().Err(err).Msg("set invite status failed")
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// DeleteByUserId удаляет отношения удалённого пользователя — и те, где он
// приглашённый, и те, где он приглашающий: inviter_id тоже его id, и оставлять
// его в базе после удаления аккаунта нельзя.
func (r MongoInviteRepository) DeleteByUserId(ctx context.Context, userID int) error {
	filter := bson.M{"$or": bson.A{
		bson.M{"invitee_id": userID},
		bson.M{"inviter_id": userID},
	}}
	if _, err := r.col.DeleteMany(ctx, filter); err != nil {
		log.Error().Err(err).Msg("delete room invites failed")
		return err
	}
	return nil
}
