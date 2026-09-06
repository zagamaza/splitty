package repository

import (
	"reflect"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Интеграционные тесты удаления аккаунта: анонимизация встроенных снимков
// (AnonymizeUser) и tombstone (SoftDeleteUser).
//
// Против ЖИВОГО mongo, а не мока, потому что проверяется ровно то, что мок не
// умеет: поведение arrayFilters на документах, где recipients лежит как null.

const (
	anonTargetID = 501 // удаляемый пользователь
	anonOtherID  = 502 // сосед по комнате: его данные обязаны остаться нетронутыми
)

// legacyUserSnapshot — снимок пользователя эпохи ДО санитайза (Task 3): поля
// личности и PII лежат прямо в документе room. Именно их обязана вычистить
// анонимизация — из канонического документа они уже не восстановятся
func legacyUserSnapshot(id int, username, displayName string) bson.M {
	return bson.M{
		"_id":          id,
		"user_name":    username,
		"display_name": displayName,
		"email":        "leak@example.com",
		"google_sub":   "g-leak",
		"apple_sub":    "a-leak",
		"telegram_id":  id,
		"bank_details": "4276 0000 0000 0000",
		"push_tokens":  []interface{}{bson.M{"token": "fcm-leak", "platform": "ios"}},
	}
}

// seedAnonymizeRoom кладёт комнату С ОБЕИМИ формами документов операции.
//
// ⚠️ Обе формы обязательны и именно в ОДНОЙ комнате:
//   - архаичная операция: recipients — массив, recipients_with_sum отсутствует
//     вовсе (документы ранней истории проекта);
//   - современная: recipients: null (REST его явно обнуляет, а bson-тег без
//     omitempty пишет null), заполнен recipients_with_sum.
//
// Наивные arrayFilters по operations.$[].recipients.$[] на второй форме роняют
// ВЕСЬ UpdateMany ошибкой «cannot apply array updates to non-array» — то есть
// один такой документ превращает DELETE /me в 500. Если в seed есть только одна
// форма, падение не будет поймано и уедет в прод
func seedAnonymizeRoom(t *testing.T, db *mongo.Database) primitive.ObjectID {
	t.Helper()

	roomID := primitive.NewObjectID()
	archaicOp := bson.M{
		"_id":         primitive.NewObjectID(),
		"description": "Ужин",
		"sum":         100,
		"donor":       legacyUserSnapshot(anonTargetID, "zagir", "Загир"),
		"recipients": []interface{}{
			legacyUserSnapshot(anonTargetID, "zagir", "Загир"),
			legacyUserSnapshot(anonOtherID, "almaz", "Алмаз"),
		},
		// recipients_with_sum отсутствует ВОВСЕ — так выглядят самые старые документы
	}
	modernOp := bson.M{
		"_id":         primitive.NewObjectID(),
		"description": "Такси",
		"sum":         50,
		"donor":       legacyUserSnapshot(anonOtherID, "almaz", "Алмаз"),
		// recipients: null — форма ВСЕХ документов, которые пишет текущий код
		"recipients": nil,
		"recipients_with_sum": []interface{}{
			bson.M{"user": legacyUserSnapshot(anonTargetID, "zagir", "Загир"), "sum": 25.0},
			bson.M{"user": legacyUserSnapshot(anonOtherID, "almaz", "Алмаз"), "sum": 25.0},
		},
	}

	doc := bson.M{
		"_id":  roomID,
		"name": "Тестовая комната",
		"users": []interface{}{
			legacyUserSnapshot(anonTargetID, "zagir", "Загир"),
			legacyUserSnapshot(anonOtherID, "almaz", "Алмаз"),
		},
		"operations": []interface{}{archaicOp, modernOp},
	}
	if _, err := db.Collection("room").InsertOne(testCtx(t), doc); err != nil {
		t.Fatalf("не удалось засеять комнату: %v", err)
	}
	return roomID
}

// readRawRoom читает документ комнаты как есть, без декодирования в api.Room:
// отсутствие поля и пустая строка в api.Room неразличимы, а тест проверяет
// именно $unset
func readRawRoom(t *testing.T, db *mongo.Database, roomID primitive.ObjectID) bson.M {
	t.Helper()
	var raw bson.M
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": roomID}).Decode(&raw); err != nil {
		t.Fatalf("не удалось прочитать комнату: %v", err)
	}
	return raw
}

// assertAnonymized — снимок затёрт: имя заменено плейсхолдером, PII нет
func assertAnonymized(t *testing.T, where string, snapshot bson.M) {
	t.Helper()
	if got := snapshot["display_name"]; got != DeletedUserPlaceholder {
		t.Errorf("%s: display_name = %v, want %q", where, got, DeletedUserPlaceholder)
	}
	if id, _ := toInt(snapshot["_id"]); id != anonTargetID {
		t.Errorf("%s: _id изменился (%v) — числовые id трогать нельзя", where, snapshot["_id"])
	}
	for _, field := range snapshotPIIFields {
		if _, ok := snapshot[field]; ok {
			t.Errorf("%s: поле %q осталось в снимке: %v", where, field, snapshot[field])
		}
	}
}

// assertUntouched — соседа анонимизация не касалась
func assertUntouched(t *testing.T, where string, snapshot bson.M) {
	t.Helper()
	if got := snapshot["display_name"]; got != "Алмаз" {
		t.Errorf("%s: чужой display_name = %v, want Алмаз", where, got)
	}
	if got := snapshot["user_name"]; got != "almaz" {
		t.Errorf("%s: чужой user_name = %v, want almaz", where, got)
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func TestAnonymizeUserBothOperationShapes(t *testing.T) {
	db := testDB(t)
	roomID := seedAnonymizeRoom(t, db)
	repo := NewRoomRepository(db)

	if err := repo.AnonymizeUser(testCtx(t), anonTargetID, DeletedUserPlaceholder); err != nil {
		t.Fatalf("AnonymizeUser: %v", err)
	}

	raw := readRawRoom(t, db, roomID)

	users := raw["users"].(primitive.A)
	assertAnonymized(t, "users[0]", users[0].(bson.M))
	assertUntouched(t, "users[1]", users[1].(bson.M))

	ops := raw["operations"].(primitive.A)
	archaic, modern := ops[0].(bson.M), ops[1].(bson.M)

	assertAnonymized(t, "архаичная.donor", archaic["donor"].(bson.M))
	recipients := archaic["recipients"].(primitive.A)
	assertAnonymized(t, "архаичная.recipients[0]", recipients[0].(bson.M))
	assertUntouched(t, "архаичная.recipients[1]", recipients[1].(bson.M))

	assertUntouched(t, "современная.donor", modern["donor"].(bson.M))
	if modern["recipients"] != nil {
		t.Errorf("современная.recipients = %v, ожидался неизменный null", modern["recipients"])
	}
	rws := modern["recipients_with_sum"].(primitive.A)
	assertAnonymized(t, "современная.recipients_with_sum[0].user", rws[0].(bson.M)["user"].(bson.M))
	assertUntouched(t, "современная.recipients_with_sum[1].user", rws[1].(bson.M)["user"].(bson.M))

	// доли и суммы — не трогаем: расчёт долгов обязан дать тот же результат
	if got := rws[0].(bson.M)["sum"]; got != 25.0 {
		t.Errorf("доля получателя изменилась: %v, want 25", got)
	}
	if got, _ := toInt(archaic["sum"]); got != 100 {
		t.Errorf("сумма операции изменилась: %v, want 100", archaic["sum"])
	}
}

// Расчётная часть документа обязана остаться байт-в-байт прежней: удаление
// аккаунта меняет только отображаемые имена, но не долги (инвариант плана)
func TestAnonymizeUserKeepsCalculationData(t *testing.T) {
	db := testDB(t)
	roomID := seedAnonymizeRoom(t, db)
	repo := NewRoomRepository(db)

	decode := func() api.Room {
		t.Helper()
		var room api.Room
		if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": roomID}).Decode(&room); err != nil {
			t.Fatalf("не удалось декодировать комнату: %v", err)
		}
		// имена и личности как раз и должны меняться — сравниваем всё остальное
		strip := func(u *api.User) {
			if u == nil {
				return
			}
			*u = api.User{ID: u.ID, CountInPage: u.CountInPage}
		}
		for i := range *room.Members {
			strip(&(*room.Members)[i])
		}
		for i := range *room.Operations {
			op := &(*room.Operations)[i]
			strip(op.Donor)
			if op.Recipients != nil {
				for j := range *op.Recipients {
					strip(&(*op.Recipients)[j])
				}
			}
			for j := range op.RecipientsWithSum {
				strip(&op.RecipientsWithSum[j].User)
			}
		}
		// Ревизия комнаты растёт при ЛЮБОЙ записи, и анонимизация — не
		// исключение: пересчёт шкалы обязан по ней понять, что снимки
		// участников трогали, и не затереть затирание. К расчётным данным она
		// отношения не имеет, поэтому из сравнения исключается.
		room.Revision = 0
		return room
	}

	before := decode()
	if err := repo.AnonymizeUser(testCtx(t), anonTargetID, DeletedUserPlaceholder); err != nil {
		t.Fatalf("AnonymizeUser: %v", err)
	}
	after := decode()

	if !reflect.DeepEqual(before, after) {
		t.Errorf("анонимизация изменила расчётные данные комнаты:\nбыло:  %+v\nстало: %+v", before, after)
	}
}

// Повторный вызов обязан быть безопасным: DELETE /me повторяют, если он упал
// после tombstone
func TestAnonymizeUserIsRepeatable(t *testing.T) {
	db := testDB(t)
	roomID := seedAnonymizeRoom(t, db)
	repo := NewRoomRepository(db)

	for i := 0; i < 2; i++ {
		if err := repo.AnonymizeUser(testCtx(t), anonTargetID, DeletedUserPlaceholder); err != nil {
			t.Fatalf("AnonymizeUser (попытка %d): %v", i+1, err)
		}
	}
	raw := readRawRoom(t, db, roomID)
	assertAnonymized(t, "users[0] после повтора", raw["users"].(primitive.A)[0].(bson.M))
}

// Комнаты без операций/участников и комнаты чужих людей не должны ни падать,
// ни меняться
func TestAnonymizeUserSkipsUnrelatedDocuments(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	emptyID, foreignID := primitive.NewObjectID(), primitive.NewObjectID()
	docs := []interface{}{
		bson.M{"_id": emptyID, "name": "Пустая", "users": nil, "operations": nil},
		bson.M{"_id": foreignID, "name": "Чужая",
			"users":      []interface{}{legacyUserSnapshot(anonOtherID, "almaz", "Алмаз")},
			"operations": []interface{}{bson.M{"_id": primitive.NewObjectID(), "donor": legacyUserSnapshot(anonOtherID, "almaz", "Алмаз"), "recipients": nil}},
		},
	}
	if _, err := db.Collection("room").InsertMany(testCtx(t), docs); err != nil {
		t.Fatalf("не удалось засеять комнаты: %v", err)
	}

	if err := repo.AnonymizeUser(testCtx(t), anonTargetID, DeletedUserPlaceholder); err != nil {
		t.Fatalf("AnonymizeUser на неподходящих документах: %v", err)
	}
	assertUntouched(t, "чужая комната", readRawRoom(t, db, foreignID)["users"].(primitive.A)[0].(bson.M))
}

func TestSoftDeleteUser(t *testing.T) {
	db := testDB(t)
	tgID := 700
	seedUsers(t, db, api.User{
		ID: anonTargetID, Username: "zagir", DisplayName: "Загир", TelegramID: &tgID,
		GoogleSub: "g-1", AppleSub: "a-1", Email: "z@example.com", AppleRefreshToken: "r-1",
		BankDetails: "4276", Aliases: []string{"Заги"}, PushTokens: []api.PushToken{{Token: "fcm-1"}},
	})
	repo := NewUserRepository(db)
	ctx := testCtx(t)

	if err := repo.SoftDeleteUser(ctx, anonTargetID); err != nil {
		t.Fatalf("SoftDeleteUser: %v", err)
	}

	// документ ОСТАЛСЯ: удаление его целиком воскресили бы upsert-методы
	user, err := repo.FindById(ctx, anonTargetID)
	if err != nil {
		t.Fatalf("документ пропал после SoftDeleteUser: %v", err)
	}
	if !user.IsDeleted() {
		t.Error("deleted_at не выставлен — middleware не отличит удалённого")
	}
	if user.DisplayName != DeletedUserPlaceholder {
		t.Errorf("display_name = %q, want %q", user.DisplayName, DeletedUserPlaceholder)
	}
	if user.Username != "" || user.Email != "" || user.BankDetails != "" ||
		user.AppleRefreshToken != "" || user.Aliases != nil || user.PushTokens != nil {
		t.Errorf("PII осталась в tombstone: %+v", user)
	}

	// личности освобождены и по ним больше не находится
	for name, find := range map[string]func() (*api.User, error){
		"google":   func() (*api.User, error) { return repo.FindByGoogleSub(ctx, "g-1") },
		"apple":    func() (*api.User, error) { return repo.FindByAppleSub(ctx, "a-1") },
		"telegram": func() (*api.User, error) { return repo.FindByTelegramID(ctx, tgID) },
	} {
		if _, err := find(); err != mongo.ErrNoDocuments {
			t.Errorf("%s: удалённый всё ещё находится по личности (err=%v)", name, err)
		}
	}

	// и главное — той же личностью можно зарегистрироваться заново
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := repo.CreateIdentityUser(ctx, api.User{ID: 999, GoogleSub: "g-1"}); err != nil {
		t.Errorf("повторная регистрация с тем же google_sub не удалась: %v", err)
	}
}

func TestSoftDeleteUserIsRepeatableAndStrict(t *testing.T) {
	db := testDB(t)
	seedUsers(t, db, api.User{ID: anonTargetID, Username: "zagir", DisplayName: "Загир"})
	repo := NewUserRepository(db)
	ctx := testCtx(t)

	for i := 0; i < 2; i++ {
		if err := repo.SoftDeleteUser(ctx, anonTargetID); err != nil {
			t.Fatalf("SoftDeleteUser (попытка %d): %v", i+1, err)
		}
	}
	// несуществующего не создаём: upsert воскресил бы пользователя пустышкой
	if err := repo.SoftDeleteUser(ctx, 12345); err != mongo.ErrNoDocuments {
		t.Errorf("SoftDeleteUser несуществующего вернул %v, want mongo.ErrNoDocuments", err)
	}
	if n, err := db.Collection("user").CountDocuments(ctx, bson.M{"_id": 12345}); err != nil || n != 0 {
		t.Errorf("SoftDeleteUser создал документ несуществующему пользователю (n=%d, err=%v)", n, err)
	}
}

func TestDeleteByUserIdCleansSideCollections(t *testing.T) {
	db := testDB(t)
	ctx := testCtx(t)

	if _, err := db.Collection("bug_report").InsertMany(ctx, []interface{}{
		bson.M{"user_id": anonTargetID, "text": "всё сломалось", "display_name": "Загир"},
		bson.M{"user_id": anonOtherID, "text": "чужой репорт"},
	}); err != nil {
		t.Fatalf("seed bug_report: %v", err)
	}
	if _, err := db.Collection("login_code").InsertMany(ctx, []interface{}{
		bson.M{"code": "AAA", "user_id": anonTargetID, "used": false},
		bson.M{"code": "BBB", "user_id": anonOtherID, "used": false},
	}); err != nil {
		t.Fatalf("seed login_code: %v", err)
	}
	if _, err := db.Collection("push_outbox").InsertMany(ctx, []interface{}{
		bson.M{"user_id": anonTargetID, "title": "Загир добавил расход", "body": "Ужин, 100"},
		bson.M{"user_id": anonOtherID, "title": "чужой пуш"},
	}); err != nil {
		t.Fatalf("seed push_outbox: %v", err)
	}
	// chat_state держит СВОБОДНЫЙ ТЕКСТ расхода (CallbackData.ExternalData) и
	// стал нагруженным сразу в двух потоках — DELETE /me и отвязка telegram, —
	// поэтому проверяется против живой mongo наравне с остальными
	if _, err := db.Collection("chat_state").InsertMany(ctx, []interface{}{
		bson.M{"user_id": anonTargetID, "callback_data": bson.M{"external_data": "Ужин с Машей 1500"}},
		bson.M{"user_id": anonTargetID, "callback_data": bson.M{"external_data": "Такси 300"}},
		bson.M{"user_id": anonOtherID, "callback_data": bson.M{"external_data": "чужое состояние"}},
	}); err != nil {
		t.Fatalf("seed chat_state: %v", err)
	}

	if err := NewBugReportRepository(db).DeleteByUserId(ctx, anonTargetID); err != nil {
		t.Fatalf("bug_report DeleteByUserId: %v", err)
	}
	if err := NewLoginCodeRepository(db).DeleteByUserId(ctx, anonTargetID); err != nil {
		t.Fatalf("login_code DeleteByUserId: %v", err)
	}
	if err := NewPushOutboxRepository(db).DeleteByUserId(ctx, anonTargetID); err != nil {
		t.Fatalf("push_outbox DeleteByUserId: %v", err)
	}
	if err := NewChatStateRepository(db).DeleteByUserId(ctx, anonTargetID); err != nil {
		t.Fatalf("chat_state DeleteByUserId: %v", err)
	}

	for _, col := range []string{"bug_report", "login_code", "push_outbox", "chat_state"} {
		mine, err := db.Collection(col).CountDocuments(ctx, bson.M{"user_id": anonTargetID})
		if err != nil {
			t.Fatalf("count %s: %v", col, err)
		}
		if mine != 0 {
			t.Errorf("%s: осталось %d записей удалённого пользователя", col, mine)
		}
		others, err := db.Collection(col).CountDocuments(ctx, bson.M{"user_id": anonOtherID})
		if err != nil {
			t.Fatalf("count %s: %v", col, err)
		}
		if others != 1 {
			t.Errorf("%s: задеты чужие записи (осталось %d, want 1)", col, others)
		}
	}
}
