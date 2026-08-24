package repository

import (
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestEnsureBindingTokenCreatesOnce — токен заводится при первом обращении и
// при последующих отдаётся тот же самый.
//
// Стабильность здесь не косметика: токен вшивается в уже совершённые покупки
// (appAccountToken), и новое значение оторвало бы их от аккаунта.
func TestEnsureBindingTokenCreatesOnce(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db, api.User{ID: 500, Username: "buyer", DisplayName: "Buyer"})

	first, err := repo.EnsureBindingToken(ctx, 500)
	if err != nil {
		t.Fatalf("первый вызов: %v", err)
	}
	if first == "" {
		t.Fatal("токен пустой")
	}

	second, err := repo.EnsureBindingToken(ctx, 500)
	if err != nil {
		t.Fatalf("второй вызов: %v", err)
	}
	if second != first {
		t.Errorf("токен изменился: было %q, стало %q", first, second)
	}
}

// TestEnsureBindingTokenKeepsExisting — уже записанный токен не перетирается.
func TestEnsureBindingTokenKeepsExisting(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	const existing = "11111111-2222-3333-4444-555555555555"
	seedUsers(t, db, api.User{ID: 501, Username: "old", DisplayName: "Old", PurchaseBindingToken: existing})

	got, err := repo.EnsureBindingToken(ctx, 501)
	if err != nil {
		t.Fatalf("EnsureBindingToken: %v", err)
	}
	if got != existing {
		t.Errorf("токен перетёрт: хотели %q, получили %q", existing, got)
	}
}

// TestEnsureBindingTokenConcurrent — параллельные вызовы получают ОДНО значение.
//
// Экран оплаты и восстановление покупок стартуют одновременно, поэтому два
// запроса подряд — обычное дело, а не редкая гонка. Разные токены означали бы,
// что чек, купленный по первому, перестал сходиться с аккаунтом.
func TestEnsureBindingTokenConcurrent(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	seedUsers(t, db, api.User{ID: 502, Username: "race", DisplayName: "Race"})

	const goroutines = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		tokens  = make(map[string]int)
		firstEr error
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			token, err := repo.EnsureBindingToken(ctx, 502)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstEr == nil {
				firstEr = err
				return
			}
			tokens[token]++
		}()
	}
	wg.Wait()

	if firstEr != nil {
		t.Fatalf("параллельный вызов упал: %v", firstEr)
	}
	if len(tokens) != 1 {
		t.Errorf("получено %d разных токенов, ожидался 1: %v", len(tokens), tokens)
	}
}

// TestEnsureBindingTokenRejectsMissingAndDeleted — несуществующий и удалённый
// аккаунт токена не получают.
//
// Для tombstone это важно отдельно: выдать ему рабочий токен привязки значило
// бы позволить привязать покупку к трупу.
func TestEnsureBindingTokenRejectsMissingAndDeleted(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	deletedAt := time.Now().UTC()
	seedUsers(t, db, api.User{ID: 503, Username: "gone", DisplayName: "Gone", DeletedAt: &deletedAt})

	tests := []struct {
		name   string
		userId int
	}{
		{"нет такого пользователя", 999_999},
		{"удалённый аккаунт", 503},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := repo.EnsureBindingToken(ctx, tc.userId); err != mongo.ErrNoDocuments {
				t.Errorf("хотели mongo.ErrNoDocuments, получили %v", err)
			}
		})
	}
}
