package repository

import (
	"context"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDebtReminderRepository — память о том, кому и когда напоминали про долг
// (коллекция debt_reminder).
type MongoDebtReminderRepository struct {
	col *mongo.Collection
}

func NewDebtReminderRepository(db *mongo.Database) *MongoDebtReminderRepository {
	return &MongoDebtReminderRepository{col: db.Collection("debt_reminder")}
}

// Claim атомарно забирает право напомнить пользователю и возвращает true, если
// оно досталось этому вызову.
//
// Захват и проверка — ОДНА операция, а не «прочитал, решил, записал»: два
// инстанса приложения (или перекрывающийся деплой) при раздельных чтении и
// записи оба сочли бы человека подходящим и прислали ему два одинаковых пуша.
// Дубль про деньги люди не прощают.
//
// Право даётся, если прошло cooldown с прошлого раза И серия ещё не исчерпана
// (streak < maxStreak). Смена debtsKey — новый эпизод: человек вернул прежний
// долг и взял новый, молчать про него нельзя.
func (r MongoDebtReminderRepository) Claim(
	ctx context.Context, userId int, debtsKey string, now time.Time, cooldown time.Duration, maxStreak int,
) (bool, *api.DebtReminder, error) {
	deadline := now.Add(-cooldown)

	// Условие «можно слать» целиком в фильтре: документа нет; либо остыл и серия
	// не исчерпана; либо набор долгов сменился (эпизод начинается заново).
	filter := bson.M{
		"_id": userId,
		"$or": []bson.M{
			{"last_sent": bson.M{"$lte": deadline}, "streak": bson.M{"$lt": maxStreak}},
			{"debts_key": bson.M{"$ne": debtsKey}},
		},
	}

	var previous api.DebtReminder
	err := r.col.FindOneAndUpdate(ctx, filter,
		bson.M{"$set": bson.M{"last_sent": now, "debts_key": debtsKey}, "$inc": bson.M{"streak": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&previous)

	switch {
	case err == nil:
		// Эпизод сменился — серия начинается заново, а не продолжает прежнюю.
		if previous.DebtsKey != debtsKey {
			if _, uErr := r.col.UpdateOne(ctx, bson.M{"_id": userId}, bson.M{"$set": bson.M{"streak": 1}}); uErr != nil {
				log.Error().Err(uErr).Int("user", userId).Msg("debt reminder: cannot reset streak")
			}
		}
		return true, &previous, nil

	case err == mongo.ErrNoDocuments:
		// Либо человека ещё нет в коллекции, либо ему рано. Различаем вставкой
		// с условием «документа нет»: upsert по фильтру _id вставит ровно один
		// раз даже при гонке (дубль по _id отвергнет сама mongo).
		res, iErr := r.col.UpdateOne(ctx,
			bson.M{"_id": userId},
			bson.M{"$setOnInsert": bson.M{"last_sent": now, "debts_key": debtsKey, "streak": 1}},
			options.Update().SetUpsert(true))
		if iErr != nil {
			if IsDuplicateKey(iErr) {
				return false, nil, nil // гонку выиграл кто-то другой
			}
			return false, nil, iErr
		}
		return res.UpsertedCount == 1, nil, nil

	default:
		return false, nil, err
	}
}

// Release возвращает состояние, каким оно было до Claim: пуш не удалось даже
// поставить в очередь, и сжигать попытку не за что.
func (r MongoDebtReminderRepository) Release(ctx context.Context, userId int, previous *api.DebtReminder) error {
	if previous == nil {
		// Документа до захвата не было — убираем его целиком.
		_, err := r.col.DeleteOne(ctx, bson.M{"_id": userId})
		return err
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": userId}, bson.M{"$set": bson.M{
		"last_sent": previous.LastSent,
		"streak":    previous.Streak,
		"debts_key": previous.DebtsKey,
	}})
	return err
}

// Reset обнуляет серию: долгов у человека не осталось, следующий эпизод
// начинается с чистого листа.
func (r MongoDebtReminderRepository) Reset(ctx context.Context, userId int) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": userId},
		bson.M{"$set": bson.M{"streak": 0, "debts_key": ""}})
	return err
}

// Get читает состояние; nil — записи нет.
func (r MongoDebtReminderRepository) Get(ctx context.Context, userId int) (*api.DebtReminder, error) {
	var state api.DebtReminder
	if err := r.col.FindOne(ctx, bson.M{"_id": userId}).Decode(&state); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

// DeleteByUserId убирает состояние при удалении аккаунта: иначе id человека и
// история напоминаний остаются в базе после tombstone навсегда.
func (r MongoDebtReminderRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": userId})
	return err
}
