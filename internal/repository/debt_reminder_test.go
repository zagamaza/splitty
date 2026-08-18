package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testCooldown  = 7 * 24 * time.Hour
	testMaxStreak = 4
)

// Первый раз напоминаем, в течение недели — молчим.
func TestClaimRespectsCooldown(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	ok, _, err := repo.Claim(ctx, 1, "key-a", now, testCooldown, testMaxStreak)
	if err != nil {
		t.Fatalf("первый захват: %v", err)
	}
	if !ok {
		t.Fatal("первое напоминание не досталось никому")
	}

	// На следующий день тот же долг — рано.
	ok, _, err = repo.Claim(ctx, 1, "key-a", now.Add(24*time.Hour), testCooldown, testMaxStreak)
	if err != nil {
		t.Fatalf("повторный захват: %v", err)
	}
	if ok {
		t.Error("напомнили второй раз в тот же срок — это спам")
	}

	// Через неделю — можно.
	ok, _, err = repo.Claim(ctx, 1, "key-a", now.Add(testCooldown+time.Minute), testCooldown, testMaxStreak)
	if err != nil {
		t.Fatalf("захват через неделю: %v", err)
	}
	if !ok {
		t.Error("через неделю напомнить не дали")
	}
}

// После четвёртого напоминания замолкаем навсегда — пока долг тот же.
func TestClaimStopsAfterMaxStreak(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < testMaxStreak; i++ {
		ok, _, err := repo.Claim(ctx, 1, "key-a", now.Add(time.Duration(i)*testCooldown*2), testCooldown, testMaxStreak)
		if err != nil {
			t.Fatalf("напоминание %d: %v", i+1, err)
		}
		if !ok {
			t.Fatalf("напоминание %d не досталось", i+1)
		}
	}

	ok, _, err := repo.Claim(ctx, 1, "key-a", now.Add(100*testCooldown), testCooldown, testMaxStreak)
	if err != nil {
		t.Fatalf("пятый захват: %v", err)
	}
	if ok {
		t.Error("пятое напоминание по тому же долгу — человек уже всё понял")
	}
}

// Долг сменился — серия начинается заново, иначе про новый долг молчали бы
// навсегда только потому, что про старый уже отнапоминали.
func TestClaimNewDebtStartsNewEpisode(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < testMaxStreak; i++ {
		if ok, _, _ := repo.Claim(ctx, 1, "key-a", now.Add(time.Duration(i)*testCooldown*2), testCooldown, testMaxStreak); !ok {
			t.Fatalf("напоминание %d не досталось", i+1)
		}
	}

	ok, _, err := repo.Claim(ctx, 1, "key-b", now.Add(100*testCooldown), testCooldown, testMaxStreak)
	if err != nil {
		t.Fatalf("новый долг: %v", err)
	}
	if !ok {
		t.Fatal("про новый долг не напомнили — серия старого его заблокировала")
	}

	state, err := repo.Get(ctx, 1)
	if err != nil || state == nil {
		t.Fatalf("состояние: %v", err)
	}
	if state.Streak != 1 {
		t.Errorf("серия нового эпизода = %d, ожидалась 1", state.Streak)
	}
}

// Гонка: два инстанса приложения одновременно решают напомнить одному человеку.
// Право обязано достаться ровно одному, иначе человек получит два одинаковых
// пуша про деньги.
func TestClaimIsExclusive(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	now := time.Now().UTC()

	const racers = 8
	var won int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, _, err := repo.Claim(context.Background(), 1, "key-a", now, testCooldown, testMaxStreak)
			if err != nil {
				t.Errorf("захват: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&won, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Errorf("право получили %d гонщиков, ожидался ровно один", won)
	}
}

// Пуш не удалось поставить в очередь — попытка не должна сгореть.
func TestReleaseRestoresState(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// Первый захват: записи не было, откат обязан убрать её целиком.
	ok, previous, err := repo.Claim(ctx, 1, "key-a", now, testCooldown, testMaxStreak)
	if err != nil || !ok {
		t.Fatalf("захват: ok=%v err=%v", ok, err)
	}
	if err := repo.Release(ctx, 1, previous); err != nil {
		t.Fatalf("откат: %v", err)
	}
	if state, _ := repo.Get(ctx, 1); state != nil {
		t.Errorf("после отката осталось состояние: %+v", state)
	}

	// Право снова свободно — иначе провал очереди стоил бы человеку недели молчания.
	if ok, _, _ := repo.Claim(ctx, 1, "key-a", now, testCooldown, testMaxStreak); !ok {
		t.Error("после отката право не вернулось")
	}
}

// Долгов не осталось — серия обнуляется.
func TestResetClearsStreak(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < testMaxStreak; i++ {
		repo.Claim(ctx, 1, "key-a", now.Add(time.Duration(i)*testCooldown*2), testCooldown, testMaxStreak)
	}
	if err := repo.Reset(ctx, 1); err != nil {
		t.Fatalf("сброс: %v", err)
	}

	if ok, _, _ := repo.Claim(ctx, 1, "key-c", now.Add(200*testCooldown), testCooldown, testMaxStreak); !ok {
		t.Error("после возврата долга новая серия не началась")
	}
}

// Удаление аккаунта не должно оставлять id человека в базе.
func TestDeleteByUserIdRemovesState(t *testing.T) {
	db := testDB(t)
	repo := NewDebtReminderRepository(db)
	ctx := context.Background()

	repo.Claim(ctx, 1, "key-a", time.Now().UTC(), testCooldown, testMaxStreak)
	if err := repo.DeleteByUserId(ctx, 1); err != nil {
		t.Fatalf("удаление: %v", err)
	}
	if state, _ := repo.Get(ctx, 1); state != nil {
		t.Errorf("состояние пережило удаление аккаунта: %+v", state)
	}
}
