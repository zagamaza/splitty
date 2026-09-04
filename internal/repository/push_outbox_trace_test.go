package repository

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/push"
)

// След доставки появился после разбора «пуш не пришёл, а телеграм пришёл»:
// очередь удаляла запись после отправки, поэтому успех и «ушло в никуда»
// выглядели одинаково — пустой коллекцией, без единой строки в логе.

// Главная гарантия: закрытая запись больше не выдаётся воркеру. Без фильтра
// по sent_at доставленный пуш уходил бы по кругу каждые пять секунд, пока его
// не унесёт TTL.
func TestDueSkipsSentPushes(t *testing.T) {
	db := testDB(t)
	repo := NewPushOutboxRepository(db)
	ctx := context.Background()

	if err := repo.Enqueue(ctx, 1, push.Notification{Title: "Коворк", Body: "пицца"}); err != nil {
		t.Fatal(err)
	}

	due, err := repo.Due(ctx, time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("свежий пуш обязан быть в выдаче, got %d", len(due))
	}

	err = repo.MarkSent(ctx, due[0].ID, push.DeliveryResult{
		Outcome: push.OutcomeSent,
		Tokens:  []push.TokenOutcome{{Token: "6SE5gI"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	after, err := repo.Due(ctx, time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("отправленный пуш попал в выдачу повторно — воркер зациклится: %+v", after)
	}
}

// Запись не удаляется, а остаётся следом: по ней и разбирают «почему не пришло».
func TestMarkSentKeepsTrace(t *testing.T) {
	db := testDB(t)
	repo := NewPushOutboxRepository(db)
	ctx := context.Background()

	if err := repo.Enqueue(ctx, 42, push.Notification{Title: "Коворк", Body: "чай"}); err != nil {
		t.Fatal(err)
	}
	due, _ := repo.Due(ctx, time.Now(), 10)

	err := repo.MarkSent(ctx, due[0].ID, push.DeliveryResult{
		Outcome: push.OutcomeSent,
		Tokens: []push.TokenOutcome{
			{Token: "5-PrsE"},
			{Token: "4UvKvw", Error: "registration-token-not-registered"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var doc pushOutboxDoc
	if err := db.Collection("push_outbox").FindOne(ctx, map[string]any{"user_id": 42}).Decode(&doc); err != nil {
		t.Fatalf("запись пропала после отправки, а должна была остаться следом: %v", err)
	}
	if doc.SentAt == nil {
		t.Error("sent_at не проставлен — TTL такую запись никогда не уберёт")
	}
	if doc.Outcome != push.OutcomeSent {
		t.Errorf("outcome = %q, ожидался %q", doc.Outcome, push.OutcomeSent)
	}
	if len(doc.Tokens) != 2 || doc.Tokens[1].Error == "" {
		t.Errorf("ответ FCM по токенам не сохранён: %+v", doc.Tokens)
	}
	// Хвост, а не сам токен: копия секрета в соседней коллекции не нужна.
	if len(doc.Tokens[0].Token) > 6 {
		t.Errorf("в след попал полный токен, а не хвост: %q", doc.Tokens[0].Token)
	}
}

// TTL-индекс заменяет отдельную джобу чистки: без него следы копились бы вечно.
func TestEnsureIndexesCreatesSentTTL(t *testing.T) {
	db := testDB(t)
	repo := NewPushOutboxRepository(db)
	ctx := context.Background()

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}

	cur, err := db.Collection("push_outbox").Indexes().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var idx []map[string]any
	if err := cur.All(ctx, &idx); err != nil {
		t.Fatal(err)
	}
	for _, i := range idx {
		if i["name"] == "idx_sent_ttl" {
			if _, ok := i["expireAfterSeconds"]; !ok {
				t.Fatal("индекс idx_sent_ttl создан БЕЗ expireAfterSeconds — чистки не будет")
			}
			return
		}
	}
	t.Fatalf("индекса idx_sent_ttl нет: %+v", idx)
}
