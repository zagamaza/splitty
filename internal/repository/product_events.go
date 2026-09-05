package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// productEventsTTL — сколько живут сырые события. Хватает на удержание до D30 и
// квартальную динамику; старше держать незачем.
const productEventsTTL = 90 * 24 * time.Hour

// ProductEvent — одно продуктовое событие. Поля описаны в
// docs/analytics-events.md, он источник правды и для клиентов, и для белого
// списка сервера.
//
// Содержимого здесь нет и не будет: ни сумм, ни названий тус, ни текста
// расхода. Только имя события и параметры из закрытых множеств.
type ProductEvent struct {
	ID string
	// UserID — 0 у события, присланного до входа. Тогда заполнен DeviceID.
	UserID int
	// DeviceID — установка приложения, а не человек. Живёт только у анонимных
	// событий и с аккаунтом НЕ связывается: склейка превратила бы обезличенный
	// поток в профиль.
	DeviceID   string
	Name       string
	At         time.Time
	Session    string
	Platform   string
	AppVersion string
	Locale     string
	Params     map[string]string
}

// productEventDoc — как событие лежит в mongo.
//
// _id составной: номер пользователя плюс клиентский идентификатор. Сырой
// клиентский id брать нельзя — проверка формата пропускает и "1", и тогда
// событие одного человека молча посчиталось бы дублем события другого и не
// записалось бы вовсе.
type productEventDoc struct {
	ID         string            `bson:"_id"`
	UserID     int               `bson:"user_id"`
	DeviceID   string            `bson:"device_id,omitempty"`
	Name       string            `bson:"name"`
	At         time.Time         `bson:"at"`
	Session    string            `bson:"session"`
	Platform   string            `bson:"platform"`
	AppVersion string            `bson:"app_version,omitempty"`
	Locale     string            `bson:"locale,omitempty"`
	Params     map[string]string `bson:"params,omitempty"`
	ExpiresAt  time.Time         `bson:"expires_at"`
}

// MongoProductEventsRepository — журнал продуктовых событий.
type MongoProductEventsRepository struct {
	col *mongo.Collection
}

func NewProductEventsRepository(db *mongo.Database) *MongoProductEventsRepository {
	return &MongoProductEventsRepository{col: db.Collection("product_events")}
}

// eventDocID — составной _id. У анонимного события номера человека нет, и
// префикс "d:" держит его в своём пространстве: иначе событие устройства с
// UserID 0 столкнулось бы с чужим по одному только клиентскому id.
func eventDocID(e ProductEvent) string {
	if e.UserID == 0 && e.DeviceID != "" {
		return "d:" + e.DeviceID + ":" + e.ID
	}
	return fmt.Sprintf("%d:%s", e.UserID, e.ID)
}

// EnsureIndexes создаёт TTL и индексы выборок. Идемпотентно; вызывать при старте.
func (r *MongoProductEventsRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires_at"),
		},
		{Keys: bson.D{{Key: "name", Value: 1}, {Key: "at", Value: 1}}, Options: options.Index().SetName("name_at")},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "at", Value: 1}}, Options: options.Index().SetName("user_at")},
		// Отдельный индекс по времени: запросу «последние события по всем»
		// первые два не помогают, у них ведущее поле другое.
		{Keys: bson.D{{Key: "at", Value: -1}}, Options: options.Index().SetName("at_desc")},
	})
	return err
}

// InsertResult — сколько событий пачки записалось, сколько было дублями.
//
// Три числа, а не «ок»: без них потеря событий и штатный дедуп выглядят
// одинаково, а именно на них держится обещание измерять фактический поток.
type InsertResult struct {
	Accepted   int
	Duplicates int
}

// Insert пишет пачку событий.
//
// Вставка НЕУПОРЯДОЧЕННАЯ: в упорядоченной первый же дубль обрывает пачку, и
// события за ним теряются молча — а дубль здесь штатное состояние, очередь на
// телефоне переживает падение и повторяет отправку.
//
// Терпимо трактуется ТОЛЬКО ошибка дублирующегося ключа, и разбирается она
// вручную: IsDuplicateKey отвечает «да» уже при одном дубле в пачке, то есть
// проглотила бы настоящий отказ записи рядом с ним и показала бы потерю данных
// как успех.
func (r *MongoProductEventsRepository) Insert(ctx context.Context, events []ProductEvent) (InsertResult, error) {
	if len(events) == 0 {
		return InsertResult{}, nil
	}

	docs := make([]interface{}, 0, len(events))
	for _, e := range events {
		docs = append(docs, productEventDoc{
			ID:         eventDocID(e),
			UserID:     e.UserID,
			DeviceID:   e.DeviceID,
			Name:       e.Name,
			At:         e.At,
			Session:    e.Session,
			Platform:   e.Platform,
			AppVersion: e.AppVersion,
			Locale:     e.Locale,
			Params:     e.Params,
			ExpiresAt:  e.At.Add(productEventsTTL),
		})
	}

	res, err := r.col.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	inserted := 0
	if res != nil {
		inserted = len(res.InsertedIDs)
	}
	if err == nil {
		return InsertResult{Accepted: inserted}, nil
	}

	dups, ok := duplicatesOnly(err, len(docs))
	if !ok {
		return InsertResult{}, err
	}
	if inserted == 0 {
		// Арифметика верна ТОЛЬКО для неупорядоченной вставки: там всё, что не
		// попало в ошибки, записалось. При упорядоченной mongo обрывается на
		// первой ошибке, и это число соврало бы в большую сторону.
		inserted = len(docs) - dups
	}
	return InsertResult{Accepted: inserted, Duplicates: dups}, nil
}

// duplicatesOnly отвечает, состоит ли ошибка пачки ИЗ ОДНИХ дублей, и сколько
// их было. Хотя бы одна другая ошибка записи — значит нет, и наверх уходит она.
func duplicatesOnly(err error, total int) (int, bool) {
	isDup := func(code int) bool { return code == 11000 || code == 11001 || code == 12582 }

	var bwe mongo.BulkWriteException
	if errors.As(err, &bwe) {
		for _, we := range bwe.WriteErrors {
			if !isDup(we.Code) {
				return 0, false
			}
		}
		if bwe.WriteConcernError != nil {
			return 0, false
		}
		return len(bwe.WriteErrors), len(bwe.WriteErrors) > 0
	}

	var we mongo.WriteException
	if errors.As(err, &we) {
		for _, e := range we.WriteErrors {
			if !isDup(e.Code) {
				return 0, false
			}
		}
		if we.WriteConcernError != nil {
			return 0, false
		}
		return len(we.WriteErrors), len(we.WriteErrors) > 0
	}

	return 0, false
}

// DeleteByUserId вычищает события человека. Реализует userDataCleaner: в
// коллекции лежит user_id, то есть поведение конкретного человека, и пережить
// удаление аккаунта оно не должно.
func (r *MongoProductEventsRepository) DeleteByUserId(ctx context.Context, userId int) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"user_id": userId})
	return err
}

// aggregateTimeout — потолок ОДНОГО запроса к mongo, а не всего обработчика.
// Слушатель админского API своего таймаута не имеет, и тяжёлая выборка иначе
// висела бы без ограничения, деля базу с продуктовыми запросами.
const aggregateTimeout = 10 * time.Second

// FeedRow — одна строка ленты событий.
type FeedRow struct {
	At       time.Time         `json:"at"`
	UserID   int               `json:"userId"`
	Name     string            `json:"name"`
	Platform string            `json:"platform"`
	Params   map[string]string `json:"params,omitempty"`
}

// DailyRow — сколько раз событие случилось за день.
type DailyRow struct {
	Date  string `json:"date"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// PlatformRow — события и люди по платформам. Разводить их обязательно:
// клиенты выкатываются не одновременно, и общее число читалось бы как «все
// пользователи», хотя половина платформы ещё не умеет слать события.
type PlatformRow struct {
	Platform string `json:"platform"`
	Events   int    `json:"events"`
	Users    int    `json:"users"`
}

// Feed отдаёт последние события окна. Единственный блок, который отдаёт записи,
// а не агрегат, поэтому у него жёсткий потолок строк.
func (r *MongoProductEventsRepository) Feed(ctx context.Context, days, limit int) ([]FeedRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days)
	opts := options.Find().
		SetSort(bson.D{{Key: "at", Value: -1}}).
		SetLimit(int64(limit)).
		SetMaxTime(aggregateTimeout)

	cur, err := r.col.Find(ctx, bson.M{"at": bson.M{"$gte": since}}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	rows := []FeedRow{}
	for cur.Next(ctx) {
		var doc productEventDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		rows = append(rows, FeedRow{
			At: doc.At, UserID: doc.UserID, Name: doc.Name,
			Platform: doc.Platform, Params: doc.Params,
		})
	}
	return rows, cur.Err()
}

// Daily считает события по дням и именам. name пустой — все события.
func (r *MongoProductEventsRepository) Daily(ctx context.Context, days int, name string) ([]DailyRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days)
	match := bson.M{"at": bson.M{"$gte": since}}
	if name != "" {
		match["name"] = name
	}

	pipeline := []bson.M{
		{"$match": match},
		{"$group": bson.M{
			"_id": bson.M{
				"date": bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$at"}},
				"name": "$name",
			},
			"count": bson.M{"$sum": 1},
		}},
		{"$sort": bson.M{"_id.date": -1, "count": -1}},
	}

	cur, err := r.col.Aggregate(ctx, pipeline, options.Aggregate().SetMaxTime(aggregateTimeout))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	rows := []DailyRow{}
	for cur.Next(ctx) {
		var doc struct {
			ID struct {
				Date string `bson:"date"`
				Name string `bson:"name"`
			} `bson:"_id"`
			Count int `bson:"count"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		rows = append(rows, DailyRow{Date: doc.ID.Date, Name: doc.ID.Name, Count: doc.Count})
	}
	return rows, cur.Err()
}

// Platforms — сколько событий и людей на каждой платформе за окно.
func (r *MongoProductEventsRepository) Platforms(ctx context.Context, days int) ([]PlatformRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days)
	pipeline := []bson.M{
		{"$match": bson.M{"at": bson.M{"$gte": since}}},
		{"$group": bson.M{
			"_id":    "$platform",
			"events": bson.M{"$sum": 1},
			"users":  bson.M{"$addToSet": "$user_id"},
		}},
		{"$project": bson.M{"events": 1, "users": bson.M{"$size": "$users"}}},
		{"$sort": bson.M{"events": -1}},
	}

	cur, err := r.col.Aggregate(ctx, pipeline, options.Aggregate().SetMaxTime(aggregateTimeout))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	rows := []PlatformRow{}
	for cur.Next(ctx) {
		var doc struct {
			ID     string `bson:"_id"`
			Events int    `bson:"events"`
			Users  int    `bson:"users"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		rows = append(rows, PlatformRow{Platform: doc.ID, Events: doc.Events, Users: doc.Users})
	}
	return rows, cur.Err()
}

// Count — сколько событий лежит в коллекции. Нужен снимку метрик: весь отказ от
// свёрток опирается на обещание измерить фактический поток, а мерить его пока
// нечем.
func (r *MongoProductEventsRepository) Count(ctx context.Context) (int64, error) {
	return r.col.CountDocuments(ctx, bson.M{})
}
