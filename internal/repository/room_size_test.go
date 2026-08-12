package repository

import (
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Потолок документа комнаты.
//
// Участники и операции лежат ВНУТРИ документа комнаты, а у mongo жёсткий предел
// 16 МБ. Долгоживущая группа однажды упрётся в него, и добавить расход станет
// нельзя вообще — не ошибка сети, а необратимое состояние. Никто при этом не
// измерял, насколько мы близко.

// TestRoomSizeIsMeasured — размер комнаты вообще считается и растёт вместе с
// содержимым. Без этого измерения все остальные проверки бессмысленны.
func TestRoomSizeIsMeasured(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}

	before, err := repo.roomSize(testCtx(t), hex)
	if err != nil || before == 0 {
		t.Fatalf("размер не измерился: %d, %v", before, err)
	}

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	op.Description = strings.Repeat("длинное описание расхода ", 200)
	addOperation(t, db, roomID, op)

	after, err := repo.roomSize(testCtx(t), hex)
	if err != nil {
		t.Fatalf("размер не измерился: %v", err)
	}
	if after <= before {
		t.Fatalf("размер не вырос после добавления расхода: было %d, стало %d", before, after)
	}
}

// TestOrdinaryRoomIsNotRejected — обычная комната пишется без вопросов: отказ
// на пустом месте означал бы, что расход нельзя добавить вообще никуда.
func TestOrdinaryRoomIsNotRejected(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}

	if err := repo.checkRoomSize(testCtx(t), hex); err != nil {
		t.Fatalf("обычная комната отклонена: %v", err)
	}
}

// TestMissingRoomDoesNotBlockWrite — комнаты нет: измерять нечего, и это НЕ
// повод отказать. Настоящий ответ (404) даст сам запрос записи.
func TestMissingRoomDoesNotBlockWrite(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	if err := repo.checkRoomSize(testCtx(t), primitive.NewObjectID()); err != nil {
		t.Fatalf("несуществующая комната дала отказ по размеру: %v", err)
	}
}

// TestLargeRoomIsRejectedWithDomainError — комната у потолка: отдаём понятную
// доменную ошибку, а не невнятный отказ mongo.
func TestLargeRoomIsRejectedWithDomainError(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}

	// Раздуваем документ до порога отказа: 15 кусков по мегабайту.
	for i := 0; i < 15; i++ {
		if _, err := db.Collection("room").UpdateOne(testCtx(t),
			bson.M{"_id": hex},
			bson.M{"$push": bson.M{"ballast": strings.Repeat("x", 1024*1024)}}); err != nil {
			t.Fatalf("не удалось раздуть комнату: %v", err)
		}
	}

	if err := repo.checkRoomSize(testCtx(t), hex); err != ErrRoomTooLarge {
		t.Fatalf("комната у потолка не отклонена: %v", err)
	}
}
