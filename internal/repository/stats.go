package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Stats — снимок состояния для метрик. Считается раз в несколько минут, а не на
// каждый scrape: часть чисел требует прохода по комнатам, и дёргать это каждые
// 15 секунд значило бы греть базу ради графика.
type Stats struct {
	Rooms         int64
	RoomsActive7d int64
	Users         int64
	PushOutbox    int64
	DebtReminders int64
	// ProductEvents — сколько сырых событий лежит сейчас. Отказ от свёрток и
	// сэмплирования опирался на прикидку по числу людей, а прикидка — не
	// измерение: без этого числа «вернуться к вопросу, когда поток покажет»
	// было бы обещанием, которое нечем исполнить.
	ProductEvents int64
}

// MongoStatsRepository считает сводные числа по коллекциям.
type MongoStatsRepository struct {
	db *mongo.Database
}

func NewStatsRepository(db *mongo.Database) *MongoStatsRepository {
	return &MongoStatsRepository{db: db}
}

// Collect собирает снимок. Ошибка одного счётчика не отменяет остальные:
// метрика, которую не удалось посчитать, остаётся нулевой, но график по
// соседним не пропадает.
func (r *MongoStatsRepository) Collect(ctx context.Context, now time.Time) (Stats, error) {
	var stats Stats
	var firstErr error

	count := func(collection string, filter bson.M, into *int64) {
		n, err := r.db.Collection(collection).CountDocuments(ctx, filter)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		*into = n
	}

	count("room", bson.M{}, &stats.Rooms)
	count("user", bson.M{}, &stats.Users)
	// Только НЕотправленные: с появлением следа доставки (sent_at) в коллекции
	// рядом с очередью лежит недельный архив, и без фильтра «глубина очереди»
	// показывала бы очередь плюс архив — метрика перестала бы что-то значить.
	count("push_outbox", bson.M{"sent_at": bson.M{"$eq": nil}}, &stats.PushOutbox)
	count("debt_reminder", bson.M{}, &stats.DebtReminders)
	count("product_events", bson.M{}, &stats.ProductEvents)
	// «Живая» комната — та, где за неделю что-то происходило. Дата создания для
	// этого не годится: старая, но живая группа выпала бы из счёта.
	count("room", bson.M{"operations.create_at": bson.M{"$gte": now.AddDate(0, 0, -7)}}, &stats.RoomsActive7d)

	return stats, firstErr
}
