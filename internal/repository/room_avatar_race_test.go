package repository

import (
	"sync"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Замена авы обязана возвращать ПРЕЖНЮЮ ссылку из той же операции записи.
//
// Пока предыдущее значение читали из снимка комнаты, две одновременные загрузки
// видели один и тот же снимок и удаляли один и тот же файл: проигравший гонку
// оставался в базе навсегда, никем не адресуемый. Повторяя это, участник мог
// безнаказанно раздувать базу.
func TestSetAvatarFileIdReturnsDisplacedId(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	roomId := seedRoom(t, db, api.User{ID: 1, DisplayName: "Загир"})
	ctx := testCtx(t)

	// У комнаты без авы вытеснять нечего.
	previous, err := repo.SetAvatarFileId(ctx, roomId, "file-a")
	if err != nil {
		t.Fatalf("первая установка: %v", err)
	}
	if previous != "" {
		t.Errorf("у комнаты без фото вытеснено %q", previous)
	}

	previous, err = repo.SetAvatarFileId(ctx, roomId, "file-b")
	if err != nil {
		t.Fatalf("замена: %v", err)
	}
	if previous != "file-a" {
		t.Errorf("вытеснено %q, ожидался file-a", previous)
	}

	previous, err = repo.SetAvatarFileId(ctx, roomId, "")
	if err != nil {
		t.Fatalf("снятие: %v", err)
	}
	if previous != "file-b" {
		t.Errorf("при снятии вытеснено %q, ожидался file-b", previous)
	}
}

// Гонка: N одновременных замен. Каждая запись вытесняет РОВНО ОДНУ прежнюю
// ссылку, поэтому объединение «вытесненных» плюс уцелевшая ссылка обязаны
// покрыть все загруженные файлы — ни один не должен потеряться.
func TestConcurrentAvatarSwapsLoseNothing(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	roomId := seedRoom(t, db, api.User{ID: 1, DisplayName: "Загир"})

	const writers = 8
	displaced := make([]string, writers)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			prev, err := repo.SetAvatarFileId(testCtx(t), roomId, fileIdFor(i))
			if err != nil {
				t.Errorf("запись %d: %v", i, err)
				return
			}
			displaced[i] = prev
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]int{}
	for _, id := range displaced {
		if id != "" {
			seen[id]++
		}
	}
	// Один и тот же файл не может быть вытеснен дважды — иначе его удалили бы
	// оба запроса, а чей-то другой файл остался бы сиротой.
	for id, times := range seen {
		if times > 1 {
			t.Errorf("файл %s вытеснен %d раз", id, times)
		}
	}

	var room struct {
		AvatarFileId *string `bson:"avatar_file_id"`
	}
	hex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&room); err != nil {
		t.Fatalf("чтение комнаты: %v", err)
	}
	if room.AvatarFileId == nil {
		t.Fatal("после гонки у комнаты нет авы")
	}
	seen[*room.AvatarFileId]++

	// Итог: каждый загруженный файл либо вытеснен кем-то (и будет удалён), либо
	// остался текущей авой. Потерянных — ноль.
	if len(seen) != writers {
		t.Errorf("учтено %d файлов из %d — остальные стали сиротами", len(seen), writers)
	}
}

func fileIdFor(i int) string {
	return string(rune('a'+i)) + "-file"
}
