package repository

import (
	"sync"
	"testing"
)

// Интеграционные тесты аллокатора номеров: атомарность $inc и поведение upsert
// мок не воспроизводит, поэтому проверяем против живого mongo (см. testDB).

func TestNextUserIDFirstCall(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	seq := NewSequenceRepository(db)

	got, err := seq.NextUserID(ctx)
	if err != nil {
		t.Fatalf("NextUserID: %v", err)
	}
	if got != firstSyntheticUserID {
		t.Fatalf("первый номер = %d, ожидался %d", got, firstSyntheticUserID)
	}
}

func TestNextUserIDMonotonic(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	seq := NewSequenceRepository(db)

	prev := 0
	for i := 0; i < 5; i++ {
		got, err := seq.NextUserID(ctx)
		if err != nil {
			t.Fatalf("NextUserID #%d: %v", i, err)
		}
		if want := firstSyntheticUserID + i; got != want {
			t.Fatalf("номер #%d = %d, ожидался %d", i, got, want)
		}
		if i > 0 && got <= prev {
			t.Fatalf("номера не растут: %d после %d", got, prev)
		}
		prev = got
	}
}

// TestNextUserIDConcurrent — главная проверка: 10 параллельных горутин обязаны
// получить 10 РАЗНЫХ номеров. Если бы аллокатор читал значение и писал его
// отдельным запросом (вместо атомарного FindOneAndUpdate с $inc), тест поймал
// бы выданные дубликаты — а в проде дубликат номера означал бы duplicate key на
// вставке нового пользователя, то есть сорванный вход.
func TestNextUserIDConcurrent(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	seq := NewSequenceRepository(db)

	const n = 10
	ids := make([]int, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = seq.NextUserID(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[int]bool, n)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("NextUserID в горутине %d: %v", i, err)
		}
		if ids[i] < firstSyntheticUserID {
			t.Fatalf("номер %d меньше стартового %d", ids[i], firstSyntheticUserID)
		}
		if seen[ids[i]] {
			t.Fatalf("номер %d выдан дважды", ids[i])
		}
		seen[ids[i]] = true
	}
	if len(seen) != n {
		t.Fatalf("получено %d различных номеров, ожидалось %d", len(seen), n)
	}
}

// TestUserRepositoryNextUserID — аллокатор доступен прямо из UserRepository:
// именно так его получит UpsertTelegramUser в графе бота, где rest.Server (и
// проброшенный туда аллокатор) недоступен
func TestUserRepositoryNextUserID(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	first, err := repo.NextUserID(ctx)
	if err != nil {
		t.Fatalf("NextUserID: %v", err)
	}
	if first != firstSyntheticUserID {
		t.Fatalf("первый номер = %d, ожидался %d", first, firstSyntheticUserID)
	}

	// счётчик общий с отдельным экземпляром аллокатора над той же базой
	second, err := NewSequenceRepository(db).NextUserID(ctx)
	if err != nil {
		t.Fatalf("NextUserID (отдельный экземпляр): %v", err)
	}
	if second != firstSyntheticUserID+1 {
		t.Fatalf("второй номер = %d, ожидался %d — счётчик не общий", second, firstSyntheticUserID+1)
	}
}
