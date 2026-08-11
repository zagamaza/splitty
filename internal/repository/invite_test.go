package repository

import (
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// newInviteRepo поднимает репозиторий приглашений с индексами на свежей базе.
func newInviteRepo(t *testing.T) (*MongoInviteRepository, primitive.ObjectID) {
	t.Helper()
	db := testDB(t)
	repo := NewInviteRepository(db)
	if err := repo.EnsureIndexes(testCtx(t)); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repo, primitive.NewObjectID()
}

func TestInviteUpsertCreatesAndUpdatesSingleRecord(t *testing.T) {
	repo, room := newInviteRepo(t)
	ctx := testCtx(t)

	first := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	if err := repo.Upsert(ctx, room, 100, 1, api.InviteAdded, first); err != nil {
		t.Fatalf("первый upsert: %v", err)
	}

	got, err := repo.Find(ctx, room, 100)
	if err != nil {
		t.Fatalf("Find после создания: %v", err)
	}
	if got.Status != api.InviteAdded || got.InviterID != 1 {
		t.Fatalf("создана не та запись: %+v", got)
	}
	if !got.CreatedAt.Equal(first) {
		t.Fatalf("CreatedAt при создании: ожидалось %v, получено %v", first, got.CreatedAt)
	}

	// Смена отношения — это новое событие, поэтому CreatedAt обязан подвинуться:
	// иначе карточка «вас добавили» окажется старше отметки прочитанного и
	// никогда не покажется.
	second := time.Now().UTC().Truncate(time.Millisecond)
	if err = repo.Upsert(ctx, room, 100, 2, api.InvitePending, second); err != nil {
		t.Fatalf("второй upsert: %v", err)
	}

	got, err = repo.Find(ctx, room, 100)
	if err != nil {
		t.Fatalf("Find после обновления: %v", err)
	}
	if got.Status != api.InvitePending || got.InviterID != 2 {
		t.Fatalf("запись не обновилась: %+v", got)
	}
	if !got.CreatedAt.Equal(second) {
		t.Fatalf("CreatedAt не подвинулся: ожидалось %v, получено %v", second, got.CreatedAt)
	}

	n, err := repo.col.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("upsert создал вторую запись вместо обновления: документов %d", n)
	}
}

func TestInviteUniqueIndexRejectsDuplicatePair(t *testing.T) {
	repo, room := newInviteRepo(t)
	ctx := testCtx(t)

	now := time.Now().UTC()
	if err := repo.Upsert(ctx, room, 100, 1, api.InviteAdded, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Прямая вставка мимо репозитория: проверяем именно индекс, а не логику
	// upsert — она дубль и так бы не создала.
	_, err := repo.col.InsertOne(ctx, api.RoomInvite{
		ID: primitive.NewObjectID(), RoomID: room, InviteeID: 100, InviterID: 7,
		Status: api.InvitePending, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("unique-индекс по (room_id, invitee_id) не сработал: дубль вставился")
	}
	if !IsDuplicateKey(err) {
		t.Fatalf("ожидалась ошибка duplicate key, получено: %v", err)
	}
}

func TestInviteListForUserSkipsFinishedStatuses(t *testing.T) {
	repo, _ := newInviteRepo(t)
	ctx := testCtx(t)
	now := time.Now().UTC()

	cases := []struct {
		status  api.InviteStatus
		visible bool
	}{
		{api.InviteAdded, true},
		{api.InvitePending, true},
		{api.InviteLeft, false},
		{api.InviteDeclined, false},
	}
	for _, c := range cases {
		if err := repo.Upsert(ctx, primitive.NewObjectID(), 100, 1, c.status, now); err != nil {
			t.Fatalf("upsert %s: %v", c.status, err)
		}
	}
	// Чужая запись в видимом статусе не должна попасть в выдачу.
	if err := repo.Upsert(ctx, primitive.NewObjectID(), 200, 1, api.InvitePending, now); err != nil {
		t.Fatalf("upsert чужой записи: %v", err)
	}

	got, err := repo.ListForUser(ctx, 100)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ожидалось 2 видимых отношения, получено %d: %+v", len(got), got)
	}
	for _, inv := range got {
		if inv.Status == api.InviteLeft || inv.Status == api.InviteDeclined {
			t.Fatalf("завершённый статус %s попал в раздел", inv.Status)
		}
		if inv.InviteeID != 100 {
			t.Fatalf("чужая запись в выдаче: %+v", inv)
		}
	}
}

// TestInviteListForUserNewestFirst — порядок карточек в разделе: свежая сверху.
// Без сортировки mongo отдаёт натуральный порядок вставки, и приглашение,
// пришедшее только что, пряталось бы под прошлогодним.
func TestInviteListForUserNewestFirst(t *testing.T) {
	repo, _ := newInviteRepo(t)
	ctx := testCtx(t)
	now := time.Now().UTC()

	older, newer := primitive.NewObjectID(), primitive.NewObjectID()
	// Вставляем в обратном порядке: натуральный порядок дал бы старую первой.
	if err := repo.Upsert(ctx, older, 100, 1, api.InvitePending, now.Add(-time.Hour)); err != nil {
		t.Fatalf("upsert старой записи: %v", err)
	}
	if err := repo.Upsert(ctx, newer, 100, 1, api.InvitePending, now); err != nil {
		t.Fatalf("upsert свежей записи: %v", err)
	}

	got, err := repo.ListForUser(ctx, 100)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ожидалось 2 отношения, получено %d", len(got))
	}
	if got[0].RoomID != newer {
		t.Fatal("карточки идут не от свежих к старым — новое приглашение уедет вниз списка")
	}
}

func TestInviteSetStatusIfCurrent(t *testing.T) {
	repo, room := newInviteRepo(t)
	ctx := testCtx(t)
	now := time.Now().UTC()

	if err := repo.Upsert(ctx, room, 100, 1, api.InvitePending, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	ok, err := repo.SetStatusIfCurrent(ctx, room, 100, api.InvitePending, api.InviteAdded, now)
	if err != nil {
		t.Fatalf("первый переход: %v", err)
	}
	if !ok {
		t.Fatal("переход pending→added должен был пройти")
	}

	// Повтор из того же ожидаемого состояния уже не проходит: статус изменился.
	ok, err = repo.SetStatusIfCurrent(ctx, room, 100, api.InvitePending, api.InviteAdded, now)
	if err != nil {
		t.Fatalf("повторный переход: %v", err)
	}
	if ok {
		t.Fatal("повторный переход pending→added не должен был пройти")
	}
}

// TestInviteConcurrentAcceptDeclineSingleWinner — ключевой тест compare-and-set:
// человек тапает «Принять» и «Отклонить» почти одновременно (или ретрай сети
// дублирует запрос). Безусловная запись дала бы «участник со статусом declined».
func TestInviteConcurrentAcceptDeclineSingleWinner(t *testing.T) {
	repo, room := newInviteRepo(t)
	ctx := testCtx(t)
	now := time.Now().UTC()

	if err := repo.Upsert(ctx, room, 100, 1, api.InvitePending, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		errs    []error
		targets = []api.InviteStatus{api.InviteAdded, api.InviteDeclined}
	)
	for _, to := range targets {
		wg.Add(1)
		go func(to api.InviteStatus) {
			defer wg.Done()
			ok, err := repo.SetStatusIfCurrent(ctx, room, 100, api.InvitePending, to, time.Now().UTC())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if ok {
				wins++
			}
		}(to)
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("ошибки при конкурентных переходах: %v", errs)
	}
	if wins != 1 {
		t.Fatalf("ожидался ровно один успешный переход, получено %d", wins)
	}

	got, err := repo.Find(ctx, room, 100)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Status != api.InviteAdded && got.Status != api.InviteDeclined {
		t.Fatalf("итоговый статус неожиданный: %s", got.Status)
	}
}

// TestInviteDeleteByUserIdCleansBothSides — при удалении аккаунта обязаны
// исчезнуть и приглашения К человеку, и приглашения ОТ него: inviter_id это
// тоже его id, и оставлять его в базе после удаления нельзя.
func TestInviteDeleteByUserIdCleansBothSides(t *testing.T) {
	repo, _ := newInviteRepo(t)
	ctx := testCtx(t)
	now := time.Now().UTC()

	asInvitee := primitive.NewObjectID()
	asInviter := primitive.NewObjectID()
	untouched := primitive.NewObjectID()

	if err := repo.Upsert(ctx, asInvitee, 100, 1, api.InviteAdded, now); err != nil {
		t.Fatalf("upsert как приглашённый: %v", err)
	}
	if err := repo.Upsert(ctx, asInviter, 200, 100, api.InvitePending, now); err != nil {
		t.Fatalf("upsert как приглашающий: %v", err)
	}
	if err := repo.Upsert(ctx, untouched, 200, 1, api.InviteAdded, now); err != nil {
		t.Fatalf("upsert чужой записи: %v", err)
	}

	if err := repo.DeleteByUserId(ctx, 100); err != nil {
		t.Fatalf("DeleteByUserId: %v", err)
	}

	if _, err := repo.Find(ctx, asInvitee, 100); err != mongo.ErrNoDocuments {
		t.Fatalf("запись, где пользователь приглашённый, не удалена: %v", err)
	}
	if _, err := repo.Find(ctx, asInviter, 200); err != mongo.ErrNoDocuments {
		t.Fatalf("запись, где пользователь приглашающий, не удалена: %v", err)
	}
	if _, err := repo.Find(ctx, untouched, 200); err != nil {
		t.Fatalf("чужая запись пострадала: %v", err)
	}
}
