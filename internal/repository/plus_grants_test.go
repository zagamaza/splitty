package repository

import (
	"testing"
	"time"
)

// testNow — момент с точностью mongo: он хранит даты в миллисекундах, и
// наносекунды из time.Now() не переживают запись. Без усечения сравнение
// «срок продлился до ожидаемого» падало бы на разнице, которой нет.
func testNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

func grantRepo(t *testing.T) *MongoPlusGrantRepository {
	t.Helper()
	repo := NewPlusGrantRepository(testDB(t))
	if err := repo.EnsureIndexes(testCtx(t)); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repo
}

// Повторная выдача при живом гранте продлевает ТУ ЖЕ строку, а не заводит
// вторую: иначе «продлить на месяц» тихо превращалось бы в две записи, из
// которых отзыв нашёл бы одну.
func TestPlusGrantExtendsLiveRow(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	if err := repo.Grant(ctx, 700, now.Add(24*time.Hour), "друг", now); err != nil {
		t.Fatalf("первая выдача: %v", err)
	}
	if err := repo.Grant(ctx, 700, now.Add(72*time.Hour), "", now); err != nil {
		t.Fatalf("продление: %v", err)
	}

	count, err := repo.col.CountDocuments(ctx, liveFilter(700, now))
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 1 {
		t.Fatalf("живых строк %d, ожидал одну", count)
	}

	live, err := repo.LiveByUser(ctx, 700, now)
	if err != nil || live == nil {
		t.Fatalf("LiveByUser: %v, %+v", err, live)
	}
	if !live.ExpiresAt.Equal(now.Add(72 * time.Hour).UTC()) {
		t.Fatalf("срок не продлился: %v", live.ExpiresAt)
	}
	// Продление без причины не стирает ту, ради которой грант выдали.
	if live.Reason != "друг" {
		t.Fatalf("причина потеряна: %q", live.Reason)
	}
}

// Выдача после отзыва заводит НОВУЮ строку, а отозванная остаётся: это разные
// факты, и по одной перезаписанной строке их потом не различить.
func TestPlusGrantAfterRevokeCreatesNewRow(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	if err := repo.Grant(ctx, 700, now.Add(24*time.Hour), "первый", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Revoke(ctx, 700, "передумали", now); err != nil {
		t.Fatalf("отзыв: %v", err)
	}
	if err := repo.Grant(ctx, 700, now.Add(48*time.Hour), "второй", now); err != nil {
		t.Fatalf("повторная выдача: %v", err)
	}

	all, err := repo.col.CountDocuments(ctx, map[string]any{"user_id": 700})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if all != 2 {
		t.Fatalf("строк %d, ожидал две (отозванную и новую)", all)
	}

	live, err := repo.LiveByUser(ctx, 700, now)
	if err != nil || live == nil {
		t.Fatalf("LiveByUser: %v, %+v", err, live)
	}
	if live.Reason != "второй" {
		t.Fatalf("живым оказался не новый грант: %q", live.Reason)
	}
}

// Истёкший и отозванный живыми не считаются.
func TestPlusGrantExpiredAndRevokedAreNotLive(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	// Истёкший: выдан вчера на час.
	yesterday := now.Add(-24 * time.Hour)
	if err := repo.Grant(ctx, 701, yesterday.Add(time.Hour), "истёк", yesterday); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	live, err := repo.LiveByUser(ctx, 701, now)
	if err != nil {
		t.Fatalf("LiveByUser: %v", err)
	}
	if live != nil {
		t.Fatalf("истёкший грант вернулся живым: %+v", live)
	}

	if err := repo.Grant(ctx, 702, now.Add(24*time.Hour), "отзовём", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Revoke(ctx, 702, "", now); err != nil {
		t.Fatalf("отзыв: %v", err)
	}
	live, err = repo.LiveByUser(ctx, 702, now)
	if err != nil {
		t.Fatalf("LiveByUser: %v", err)
	}
	if live != nil {
		t.Fatalf("отозванный грант вернулся живым: %+v", live)
	}
}

// Две живые строки (гонка двух выдач) отзываются ОБЕ.
//
// Уникальность живого гранта не гарантируется сознательно, поэтому цена решения
// — здесь: UpdateOne оставил бы вторую строку раздавать Plus после «отозвано».
func TestPlusGrantRevokeClearsEveryLiveRow(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	// Вторую строку вставляем в обход Grant: он бы нашёл живую и продлил её.
	// Так выглядит результат гонки двух одновременных выдач.
	for _, expires := range []time.Time{now.Add(24 * time.Hour), now.Add(48 * time.Hour)} {
		if _, err := repo.col.InsertOne(ctx, map[string]any{
			"user_id":    703,
			"source":     "panel",
			"expires_at": expires,
			"created_at": now,
			"updated_at": now,
		}); err != nil {
			t.Fatalf("InsertOne: %v", err)
		}
	}

	if err := repo.Revoke(ctx, 703, "гонка", now); err != nil {
		t.Fatalf("отзыв: %v", err)
	}

	count, err := repo.col.CountDocuments(ctx, liveFilter(703, now))
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 0 {
		t.Fatalf("после отзыва осталось живых строк: %d", count)
	}
}

// Удаление аккаунта уносит гранты целиком.
func TestPlusGrantDeleteByUserId(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	if err := repo.Grant(ctx, 704, now.Add(24*time.Hour), "", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Revoke(ctx, 704, "", now); err != nil {
		t.Fatalf("отзыв: %v", err)
	}
	if err := repo.DeleteByUserId(ctx, 704); err != nil {
		t.Fatalf("DeleteByUserId: %v", err)
	}

	count, err := repo.col.CountDocuments(ctx, map[string]any{"user_id": 704})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 0 {
		t.Fatalf("после удаления осталось строк: %d", count)
	}
}

// ListLive отдаёт только живых и поздним сроком вперёд.
func TestPlusGrantListLive(t *testing.T) {
	repo := grantRepo(t)
	ctx := testCtx(t)
	now := testNow()

	if err := repo.Grant(ctx, 705, now.Add(24*time.Hour), "ближний", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Grant(ctx, 706, now.Add(72*time.Hour), "дальний", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Grant(ctx, 707, now.Add(24*time.Hour), "отозванный", now); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if err := repo.Revoke(ctx, 707, "", now); err != nil {
		t.Fatalf("отзыв: %v", err)
	}

	live, err := repo.ListLive(ctx, now)
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("живых %d, ожидал два: %+v", len(live), live)
	}
	if live[0].UserId != 706 {
		t.Fatalf("порядок не по сроку: %+v", live)
	}
}
