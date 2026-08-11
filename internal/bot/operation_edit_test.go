package bot

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestEditedOperationDraftResetsNotificationSent — правка расхода в боте это
// новая запись с новой датой: в ленте она всплывает как свежее событие. Список
// notification_sent, унаследованный от прошлой версии, описывал бы прежний
// состав — назначенный правкой плательщик в бейдж не попадал бы (а уведомление
// ему как раз ушло), выкинутый получатель попадал бы за чужой расход.
func TestEditedOperationDraftResetsNotificationSent(t *testing.T) {
	oldID := primitive.NewObjectID()
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	op := api.Operation{
		ID:               oldID,
		Description:      "Ужин",
		Sum:              1200,
		Status:           active,
		CreateAt:         created,
		NotificationSent: []int{7, 8},
	}

	next := editedOperationDraft(op, now)

	if len(next.NotificationSent) != 0 {
		t.Fatalf("новая версия унаследовала аудиторию прошлой: %v", next.NotificationSent)
	}
	if next.ID == oldID {
		t.Fatal("черновик правки обязан получить новый идентификатор")
	}
	if next.OldOperationId == nil || *next.OldOperationId != oldID {
		t.Fatal("потеряна ссылка на архивируемую версию")
	}
	if next.Status != draft {
		t.Fatalf("статус черновика %q", next.Status)
	}
	if !next.CreateAt.Equal(now) {
		t.Fatalf("дата новой версии %v вместо %v", next.CreateAt, now)
	}
	// оригинал не тронут: он ещё уходит в архив отдельной записью
	if len(op.NotificationSent) != 2 || op.ID != oldID {
		t.Fatal("исходная операция изменена — архивная версия ушла бы испорченной")
	}
}
