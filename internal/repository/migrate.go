package repository

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// migrationCollection — коллекция маркеров выполненных миграций. Один документ
// на миграцию, _id — её имя.
const migrationCollection = "migration"

// backfillTelegramIDMarker — _id маркера бэкфилла telegram_id
const backfillTelegramIDMarker = "backfill_telegram_id"

// BackfillTelegramID проставляет исторические telegram_id = _id: до этого плана
// _id пользователя И БЫЛ его telegram id, а теперь это отдельные вещи.
//
// Защита двойная и обе половины нужны:
//
//  1. МАРКЕР в коллекции migration — миграция отрабатывает ровно один раз за всю
//     жизнь инсталляции. Без него каждый рестарт сервера проставлял бы
//     telegram_id google-пользователям (у них _id >= firstSyntheticUserID), и
//     нотифаер полез бы слать сообщения в Telegram на несуществующий chat_id,
//     аватар ушёл бы в Telegram API за чужим user_id, а отвязка telegram
//     (Task 12) молча откатывалась бы при каждом старте.
//
//  2. УЗКИЙ ФИЛЬТР — страховка на случай, если маркер потеряют (восстановление
//     базы из старого дампа, ручная чистка коллекции migration). Затрагиваются
//     только документы, которые заведомо являются историческими
//     telegram-пользователями:
//     - telegram_id ещё нет (иначе перезаписали бы существующую привязку);
//     - google_sub/apple_sub нет — пользователь пришёл не из Telegram;
//     - deleted_at нет: у tombstone (Task 13) telegram_id вычищен намеренно,
//     вернуть его — значит слать уведомления удалённому аккаунту и заблокировать
//     unique-индексом повторную регистрацию того же telegram-аккаунта;
//     - _id < firstSyntheticUserID — синтетические номера telegram id не были
//     никогда;
//     - dev_auth нет — аккаунт заведён не через POST /auth/dev. Dev-аккаунт по
//     содержимому документа неотличим от исторического telegram-пользователя
//     (маленький _id, ни одного поля личности), и без этого условия ему
//     проставился бы telegram_id, которого у него нет и не было: нотифаер полез
//     бы слать в несуществующий чат, а /users/{id}/avatar — в Telegram API за
//     чужим user id. Раньше от этого спасал пропуск всей миграции при
//     API_DEV_AUTH=true — привязка миграции данных к флагу АВТОРИЗАЦИИ, из-за
//     которой маркер не записывался вовсе, а первый же старт с выключенным
//     флагом мог упасть на duplicate key и увести сервер в crash-loop.
//
// Возвращает число обновлённых документов.
func BackfillTelegramID(ctx context.Context, db *mongo.Database) (int64, error) {
	migrations := db.Collection(migrationCollection)

	res := migrations.FindOne(ctx, bson.D{{Key: "_id", Value: backfillTelegramIDMarker}})
	switch {
	case res.Err() == nil:
		// уже выполнялась — к коллекции user не идём вовсе
		return 0, nil
	case errors.Is(res.Err(), mongo.ErrNoDocuments):
		// первый запуск, продолжаем
	default:
		return 0, errors.Wrap(res.Err(), "cannot read migration marker")
	}

	filter := bson.D{
		{Key: "telegram_id", Value: bson.D{{Key: "$exists", Value: false}}},
		{Key: "google_sub", Value: bson.D{{Key: "$exists", Value: false}}},
		{Key: "apple_sub", Value: bson.D{{Key: "$exists", Value: false}}},
		{Key: "deleted_at", Value: bson.D{{Key: "$exists", Value: false}}},
		{Key: "_id", Value: bson.D{{Key: "$lt", Value: firstSyntheticUserID}}},
		{Key: "dev_auth", Value: bson.D{{Key: "$exists", Value: false}}},
	}

	// агрегационный pipeline-update: только он умеет присвоить полю значение
	// другого поля того же документа ("$_id")
	update := mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.D{{Key: "telegram_id", Value: "$_id"}}}},
	}

	ur, err := db.Collection("user").UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, errors.Wrap(err, "backfill telegram_id failed")
	}

	marker := bson.D{
		{Key: "_id", Value: backfillTelegramIDMarker},
		{Key: "applied_at", Value: time.Now().UTC()},
		{Key: "modified", Value: ur.ModifiedCount},
	}
	if _, err = migrations.InsertOne(ctx, marker); err != nil {
		// маркер уже вставил параллельно стартовавший процесс — данные в порядке,
		// вторая запись pipeline-update идемпотентна по построению
		if !IsDuplicateKey(err) {
			return ur.ModifiedCount, errors.Wrap(err, "cannot write migration marker")
		}
	}

	log.Info().Int64("modified", ur.ModifiedCount).Msg("backfill telegram_id done")
	return ur.ModifiedCount, nil
}
