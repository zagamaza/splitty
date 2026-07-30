package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// telegramIDOf читает telegram_id сырым документом: api.User декодирует
// отсутствующее поле и null одинаково, а тесту важно отличать «поля нет».
func telegramIDOf(t *testing.T, db *mongo.Database, id int) (value int, present bool) {
	t.Helper()
	var doc bson.M
	if err := db.Collection("user").FindOne(testCtx(t), bson.M{"_id": id}).Decode(&doc); err != nil {
		t.Fatalf("пользователь _id=%d не найден: %v", id, err)
	}
	raw, ok := doc["telegram_id"]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case int:
		return v, true
	default:
		t.Fatalf("telegram_id у _id=%d неожиданного типа %T", id, raw)
		return 0, false
	}
}

// TestBackfillTelegramID — основной прогон: исторический telegram-пользователь
// получает telegram_id == _id, а всё, что бэкфилл трогать не должен, остаётся
// нетронутым.
func TestBackfillTelegramID(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)

	deletedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedUsers(t, db,
		// исторический telegram-пользователь — единственный кандидат
		api.User{ID: 111, Username: "old", DisplayName: "Old Telegram"},
		// уже привязанный: telegram_id задан вручную и отличается от _id
		api.User{ID: 222, Username: "linked", DisplayName: "Linked", TelegramID: intPtr(999)},
		// google-пользователь без telegram_id, но с синтетическим _id
		api.User{ID: 1_000_000_000_001, Username: "g", DisplayName: "Google", GoogleSub: "sub-g"},
		// google-пользователь с «человеческим» _id — отсекается по google_sub
		api.User{ID: 333, Username: "g2", DisplayName: "Google Low Id", GoogleSub: "sub-g2"},
		// apple-пользователь — отсекается по apple_sub
		api.User{ID: 444, Username: "a", DisplayName: "Apple", AppleSub: "sub-a"},
		// tombstone: telegram_id вычищен намеренно, возвращать его нельзя
		api.User{ID: 555, Username: "dead", DisplayName: "Deleted", DeletedAt: &deletedAt},
		// синтетический номер без всякой личности — тоже не telegram
		api.User{ID: 1_000_000_000_002, Username: "synth", DisplayName: "Synthetic"},
	)

	modified, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("бэкфилл упал: %v", err)
	}
	if modified != 1 {
		t.Fatalf("ожидался ровно 1 обновлённый документ, получено %d", modified)
	}

	if got, present := telegramIDOf(t, db, 111); !present || got != 111 {
		t.Fatalf("историческому пользователю не проставлен telegram_id: got=%d present=%v", got, present)
	}

	if got, present := telegramIDOf(t, db, 222); !present || got != 999 {
		t.Fatalf("существующий telegram_id перезаписан: got=%d present=%v", got, present)
	}

	untouched := []struct {
		name string
		id   int
	}{
		{"google с синтетическим _id", 1_000_000_000_001},
		{"google с низким _id", 333},
		{"apple", 444},
		{"tombstone", 555},
		{"синтетический _id без личности", 1_000_000_000_002},
	}
	for _, tc := range untouched {
		t.Run(tc.name, func(t *testing.T) {
			if got, present := telegramIDOf(t, db, tc.id); present {
				t.Fatalf("бэкфилл затронул документ, который трогать нельзя: _id=%d telegram_id=%d", tc.id, got)
			}
		})
	}
}

// TestBackfillTelegramIDIdempotent — второй прогон обязан быть no-op.
//
// Проверка сформулирована наблюдаемо: после первого прогона в базу кладётся
// sentinel, идеально подходящий под фильтр (без telegram_id, _id < 10^12, без
// личностей и deleted_at). Если бы маркер не сработал, второй прогон его
// обязательно бы обновил.
func TestBackfillTelegramIDIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)

	seedUsers(t, db, api.User{ID: 111, Username: "old", DisplayName: "Old Telegram"})

	first, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("первый прогон упал: %v", err)
	}
	if first != 1 {
		t.Fatalf("первый прогон обновил %d документов, ожидался 1", first)
	}

	// sentinel появляется ПОСЛЕ первого прогона — ровно такой, какой бэкфилл
	// обновил бы, если бы его вообще запустили
	seedUsers(t, db, api.User{ID: 112, Username: "sentinel", DisplayName: "Sentinel"})

	second, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("второй прогон упал: %v", err)
	}
	if second != 0 {
		t.Fatalf("второй прогон обновил %d документов, ожидался 0", second)
	}
	if got, present := telegramIDOf(t, db, 112); present {
		t.Fatalf("второй прогон тронул sentinel: telegram_id=%d", got)
	}

	// третий прогон — тоже no-op, маркер не пересоздаётся
	third, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("третий прогон упал: %v", err)
	}
	if third != 0 {
		t.Fatalf("третий прогон обновил %d документов, ожидался 0", third)
	}

	n, err := db.Collection(migrationCollection).CountDocuments(ctx, bson.M{"_id": backfillTelegramIDMarker})
	if err != nil {
		t.Fatalf("не удалось посчитать маркеры: %v", err)
	}
	if n != 1 {
		t.Fatalf("маркер миграции должен существовать ровно в одном экземпляре, найдено %d", n)
	}
}

// TestBackfillTelegramIDMarkerCreatedOnEmptyDB — маркер ставится даже когда
// обновлять было нечего, иначе следующий рестарт снова пошёл бы в user.
func TestBackfillTelegramIDMarkerCreatedOnEmptyDB(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)

	modified, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("бэкфилл на пустой базе упал: %v", err)
	}
	if modified != 0 {
		t.Fatalf("на пустой базе ожидалось 0 обновлений, получено %d", modified)
	}

	var doc bson.M
	if err := db.Collection(migrationCollection).FindOne(ctx, bson.M{"_id": backfillTelegramIDMarker}).Decode(&doc); err != nil {
		t.Fatalf("маркер миграции не создан: %v", err)
	}
	if _, ok := doc["applied_at"]; !ok {
		t.Fatalf("в маркере нет времени применения: %+v", doc)
	}
}

// TestBackfillTelegramIDRespectsUniqueIndex — бэкфилл совместим с unique sparse
// индексом из Task 3: индексы строятся первыми, бэкфилл после них.
func TestBackfillTelegramIDRespectsUniqueIndex(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)
	repo := NewUserRepository(db)

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("не удалось создать индексы: %v", err)
	}

	seedUsers(t, db,
		api.User{ID: 111, Username: "a", DisplayName: "A"},
		api.User{ID: 222, Username: "b", DisplayName: "B"},
	)

	modified, err := BackfillTelegramID(ctx, db)
	if err != nil {
		t.Fatalf("бэкфилл поверх unique-индекса упал: %v", err)
	}
	if modified != 2 {
		t.Fatalf("ожидалось 2 обновления, получено %d", modified)
	}

	for _, id := range []int{111, 222} {
		u, err := repo.FindByTelegramID(ctx, id)
		if err != nil {
			t.Fatalf("после бэкфилла пользователь не ищется по telegram_id=%d: %v", id, err)
		}
		if u.ID != id {
			t.Fatalf("по telegram_id=%d найден _id=%d", id, u.ID)
		}
	}
}
