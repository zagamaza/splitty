package repository

import (
	"bytes"
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Файлы лежат отдельной коллекцией, и её проверяем против живой mongo: важно
// ровно то, что мок не умеет — что Binary возвращается байт в байт, что размер
// считает сервер, а не клиент, и что удаление по комнате действительно сносит
// все её файлы.

func TestFileSaveAndGet(t *testing.T) {
	db := testDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("индексы: %v", err)
	}

	roomId := primitive.NewObjectID()
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	id, err := repo.Save(ctx, &api.StoredFile{
		RoomId:  roomId,
		OwnerId: 42,
		Kind:    api.StoredFileRoomAvatar,
		Mime:    "image/jpeg",
		Data:    data,
	})
	if err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	if id == "" {
		t.Fatal("сохранение не вернуло id")
	}

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got == nil {
		t.Fatal("сохранённый файл не читается")
	}
	if !bytes.Equal(got.Data, data) {
		t.Errorf("байты испорчены: %v", got.Data)
	}
	// Размер считает репозиторий: клиент мог соврать в заголовке.
	if got.Size != len(data) {
		t.Errorf("размер = %d, ожидался %d", got.Size, len(data))
	}
	if got.Mime != "image/jpeg" || got.Kind != api.StoredFileRoomAvatar || got.OwnerId != 42 {
		t.Errorf("метаданные не сохранились: %+v", got)
	}
	if got.RoomId != roomId {
		t.Errorf("комната = %s, ожидалась %s", got.RoomId.Hex(), roomId.Hex())
	}
	if got.CreatedAt.IsZero() {
		t.Error("время создания не проставлено")
	}
}

// Ненайденный файл — не ошибка: вызывающему надо отличать «нет такого» (уйдёт
// в телеграмный путь) от «база сломалась» (500).
func TestFileGetMissingIsNotError(t *testing.T) {
	db := testDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	for _, id := range []string{primitive.NewObjectID().Hex(), "не-objectid", ""} {
		got, err := repo.Get(ctx, id)
		if err != nil {
			t.Errorf("id %q: неожиданная ошибка %v", id, err)
		}
		if got != nil {
			t.Errorf("id %q: вернулся файл, которого нет", id)
		}
	}
}

func TestFileDelete(t *testing.T) {
	db := testDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	id, err := repo.Save(ctx, &api.StoredFile{
		RoomId: primitive.NewObjectID(),
		Kind:   api.StoredFileRoomAvatar,
		Mime:   "image/png",
		Data:   []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("удаление: %v", err)
	}
	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("чтение после удаления: %v", err)
	}
	if got != nil {
		t.Error("файл остался после удаления")
	}

	// Повторное удаление обязано быть тихим: оно идёт следом за заменой авы и
	// может прилететь дважды.
	if err := repo.Delete(ctx, id); err != nil {
		t.Errorf("повторное удаление вернуло ошибку: %v", err)
	}
}

func TestFileDeleteByRoom(t *testing.T) {
	db := testDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	mine := primitive.NewObjectID()
	other := primitive.NewObjectID()

	var ids []string
	for i := 0; i < 2; i++ {
		id, err := repo.Save(ctx, &api.StoredFile{RoomId: mine, Kind: api.StoredFileRoomAvatar, Mime: "image/png", Data: []byte{byte(i)}})
		if err != nil {
			t.Fatalf("сохранение: %v", err)
		}
		ids = append(ids, id)
	}
	survivor, err := repo.Save(ctx, &api.StoredFile{RoomId: other, Kind: api.StoredFileRoomAvatar, Mime: "image/png", Data: []byte{9}})
	if err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	if err := repo.DeleteByRoom(ctx, mine.Hex()); err != nil {
		t.Fatalf("удаление по комнате: %v", err)
	}

	for _, id := range ids {
		got, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("чтение: %v", err)
		}
		if got != nil {
			t.Errorf("файл %s остался после удаления комнаты", id)
		}
	}
	// Чужая комната не пострадала — иначе удаление одной группы уносило бы
	// аватары всех остальных.
	got, err := repo.Get(ctx, survivor)
	if err != nil {
		t.Fatalf("чтение чужого файла: %v", err)
	}
	if got == nil {
		t.Error("удаление комнаты снесло файл чужой комнаты")
	}
}
