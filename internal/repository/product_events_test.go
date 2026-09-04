package repository

import (
	"context"
	"testing"
	"time"
)

func testEvent(userID int, id, name string) ProductEvent {
	return ProductEvent{
		ID:         id,
		UserID:     userID,
		Name:       name,
		At:         time.Now().UTC().Truncate(time.Millisecond),
		Session:    "sess-1",
		Platform:   "ios",
		AppVersion: "1.8",
		Locale:     "ru",
	}
}

// Повтор пачки не плодит документы и не считается ошибкой: очередь на телефоне
// переживает падение и повторяет отправку, это штатное состояние.
func TestProductEventsDedupe(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()

	events := []ProductEvent{testEvent(1, "a-1", "app_open"), testEvent(1, "a-2", "room_created")}
	got, err := repo.Insert(ctx, events)
	if err != nil {
		t.Fatalf("первая вставка: %v", err)
	}
	if got.Accepted != 2 || got.Duplicates != 0 {
		t.Fatalf("первая вставка дала %+v, ожидал 2 принятых", got)
	}

	got, err = repo.Insert(ctx, events)
	if err != nil {
		t.Fatalf("повтор не должен быть ошибкой: %v", err)
	}
	if got.Duplicates != 2 || got.Accepted != 0 {
		t.Errorf("повтор дал %+v, ожидал 2 дубля", got)
	}

	n, err := repo.col.CountDocuments(ctx, map[string]any{"user_id": 1})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("в базе %d документов, ожидал 2 — повтор удвоил бы воронку", n)
	}
}

// Дубль в середине пачки не должен обрывать вставку. Это и есть причина
// неупорядоченной записи: в упорядоченной события ЗА дублем теряются молча.
func TestProductEventsUnorderedInsertKeepsRest(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()

	if _, err := repo.Insert(ctx, []ProductEvent{testEvent(2, "b-1", "app_open")}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Insert(ctx, []ProductEvent{
		testEvent(2, "b-1", "app_open"),       // дубль первым
		testEvent(2, "b-2", "room_created"),   // должен записаться
		testEvent(2, "b-3", "settle_up_done"), // и этот тоже
	})
	if err != nil {
		t.Fatalf("пачка с дублем не должна быть ошибкой: %v", err)
	}
	if got.Duplicates != 1 {
		t.Errorf("получил %+v, ожидал 1 дубль", got)
	}

	// Проверяем БАЗУ, а не возвращённые числа: при упорядоченной вставке mongo
	// обрывается на первой ошибке, и события за дублем не записываются — а
	// счётчик, посчитанный арифметикой, об этом не узнает.
	n, err := repo.col.CountDocuments(ctx, map[string]any{"user_id": 2})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("в базе %d событий, ожидал 3: события за дублем потерялись", n)
	}
	if got.Accepted != 2 {
		t.Errorf("получил %+v, ожидал 2 принятых", got)
	}
}

// Один и тот же клиентский id у РАЗНЫХ людей — два разных события.
//
// Проверка формата пропускает и "1": если класть сырой id в _id, событие
// второго человека молча посчиталось бы дублем первого и не записалось.
func TestProductEventsIdsAreScopedToUser(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()

	if _, err := repo.Insert(ctx, []ProductEvent{testEvent(10, "1", "app_open")}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Insert(ctx, []ProductEvent{testEvent(11, "1", "app_open")})
	if err != nil {
		t.Fatalf("событие второго человека: %v", err)
	}
	if got.Accepted != 1 {
		t.Errorf("получил %+v — событие второго человека потерялось как дубль первого", got)
	}
}

// События удаляются вместе с аккаунтом: в коллекции лежит поведение конкретного
// человека, и пережить tombstone оно не должно.
func TestProductEventsDeleteByUser(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()

	if _, err := repo.Insert(ctx, []ProductEvent{testEvent(20, "c-1", "app_open"), testEvent(21, "c-2", "app_open")}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByUserId(ctx, 20); err != nil {
		t.Fatal(err)
	}

	n, err := repo.col.CountDocuments(ctx, map[string]any{"user_id": 20})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("после удаления осталось %d событий", n)
	}
	if n, _ = repo.col.CountDocuments(ctx, map[string]any{"user_id": 21}); n != 1 {
		t.Error("чистка задела чужие события")
	}
}

// Индексы создаются идемпотентно: EnsureIndexes зовётся на каждом старте.
func TestProductEventsEnsureIndexesTwice(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("первый вызов: %v", err)
	}
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("повторный вызов: %v", err)
	}
}

// Агрегаты считаются на живой базе: пайплайн легко написать так, что он
// молча вернёт пустоту или чужие строки, и на фейке это не видно.
func TestProductEventsAggregates(t *testing.T) {
	repo := NewProductEventsRepository(testDB(t))
	ctx := context.Background()

	now := time.Now().UTC()
	events := []ProductEvent{
		{ID: "g-1", UserID: 30, Name: "app_open", At: now, Session: "s", Platform: "ios"},
		{ID: "g-2", UserID: 30, Name: "app_open", At: now, Session: "s", Platform: "ios"},
		{ID: "g-3", UserID: 31, Name: "room_created", At: now, Session: "s", Platform: "android"},
		// За пределами окна: не должно попасть ни в один блок.
		{ID: "g-4", UserID: 32, Name: "app_open", At: now.AddDate(0, 0, -40), Session: "s", Platform: "ios"},
	}
	if _, err := repo.Insert(ctx, events); err != nil {
		t.Fatal(err)
	}

	daily, err := repo.Daily(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range daily {
		counts[row.Name] += row.Count
	}
	if counts["app_open"] < 2 {
		t.Errorf("app_open за неделю: %d, ожидал не меньше 2 (%+v)", counts["app_open"], daily)
	}
	if counts["app_open"] > 2 {
		t.Errorf("в окно попало старое событие: app_open = %d", counts["app_open"])
	}

	byName, err := repo.Daily(ctx, 7, "room_created")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range byName {
		if row.Name != "room_created" {
			t.Errorf("фильтр по имени пропустил %q", row.Name)
		}
	}

	platforms, err := repo.Platforms(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]PlatformRow{}
	for _, row := range platforms {
		seen[row.Platform] = row
	}
	if seen["ios"].Users != 1 || seen["android"].Users != 1 {
		t.Errorf("люди по платформам посчитаны неверно: %+v", platforms)
	}

	feed, err := repo.Feed(ctx, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) < 3 {
		t.Errorf("в ленте %d строк, ожидал не меньше 3", len(feed))
	}
	for i := 1; i < len(feed); i++ {
		if feed[i].At.After(feed[i-1].At) {
			t.Error("лента не отсортирована от свежих к старым")
		}
	}
}
