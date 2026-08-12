package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Конфликт правок через API.
//
// Расход в общей группе правит кто угодно, и правки идут не по одной. Пока
// запись была безусловной, второй сохранивший затирал первого молча: обоим
// показывали успех, а исправление одного из них просто исчезало.

// putOperation — правка расхода; version < 0 означает «поле не отправлено»
// (сборка про версии не знает).
func putOperation(t *testing.T, s *Server, roomId, operationId string, userId int, description string, sum, version int) *httpResponse {
	t.Helper()
	body := fmt.Sprintf(`{"description": %q, "sum": %d, "donorId": 1, "recipientIds": [1, 2]`, description, sum)
	if version >= 0 {
		body += fmt.Sprintf(`, "version": %d`, version)
	}
	body += "}"
	rec := doRequest(t, s, http.MethodPut,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", roomId, operationId),
		mustToken(t, s, userId), body)
	return &httpResponse{code: rec.Code, body: rec.Body.String()}
}

type httpResponse struct {
	code int
	body string
}

// version достаёт версию расхода из ответа.
func (r *httpResponse) version(t *testing.T) int {
	t.Helper()
	var dto struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(r.body), &dto); err != nil {
		t.Fatalf("ответ не разобрался: %v, тело: %s", err, r.body)
	}
	return dto.Version
}

// errorCode достаёт код ошибки из ответа.
func (r *httpResponse) errorCode(t *testing.T) string {
	t.Helper()
	var dto struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.body), &dto); err != nil {
		t.Fatalf("ошибка не разобралась: %v, тело: %s", err, r.body)
	}
	return dto.Error.Code
}

// TestUpdateOperationConflictsOnStaleVersion — головной случай: двое открыли
// расход, первый сохранил. Второй обязан получить отказ, а не затереть.
func TestUpdateOperationConflictsOnStaleVersion(t *testing.T) {
	room := newTestRoom()
	repo := newFakeRoomRepo(room)
	operationId := (*room.Operations)[0].ID.Hex()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	// оба видели версию 0
	first := putOperation(t, s, room.ID.Hex(), operationId, testUser1.ID, "Ужин в баре", 200, 0)
	if first.code != http.StatusOK {
		t.Fatalf("первая правка: %d, тело: %s", first.code, first.body)
	}
	if v := first.version(t); v != 1 {
		t.Fatalf("после правки клиенту вернули версию %d — следующая правка сразу упрётся в конфликт", v)
	}

	second := putOperation(t, s, room.ID.Hex(), operationId, testUser2.ID, "Такси", 300, 0)
	if second.code != http.StatusConflict {
		t.Fatalf("вторая правка: %d, ожидался 409; тело: %s", second.code, second.body)
	}
	if code := second.errorCode(t); code != "stale_operation" {
		t.Fatalf("код ошибки %q, ожидался stale_operation — клиенту нечем отличить конфликт от прочих 409", code)
	}

	stored := (*repo.rooms[room.ID.Hex()].Operations)[0]
	if stored.Description != "Ужин в баре" || stored.Sum != 200 {
		t.Fatalf("в базе %q на %d — проигравший всё-таки затёр первого", stored.Description, stored.Sum)
	}
}

// TestUpdateOperationWithFreshVersionPasses — конфликт разрешается перечиткой:
// с новой версией правка обязана пройти, иначе расход стал бы неправимым.
func TestUpdateOperationWithFreshVersionPasses(t *testing.T) {
	room := newTestRoom()
	operationId := (*room.Operations)[0].ID.Hex()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	first := putOperation(t, s, room.ID.Hex(), operationId, testUser1.ID, "Ужин в баре", 200, 0)
	if first.code != http.StatusOK {
		t.Fatalf("первая правка: %d, тело: %s", first.code, first.body)
	}
	second := putOperation(t, s, room.ID.Hex(), operationId, testUser2.ID, "Такси", 300, first.version(t))
	if second.code != http.StatusOK {
		t.Fatalf("правка по свежей версии: %d, тело: %s", second.code, second.body)
	}
}

// TestUpdateOperationWithoutVersionStillWorks — совместимость: у людей на руках
// сборки, которые про версию не знают. Требовать её — значит сломать
// установленное, поэтому запрос без поля правит расход как раньше.
func TestUpdateOperationWithoutVersionStillWorks(t *testing.T) {
	room := newTestRoom()
	repo := newFakeRoomRepo(room)
	operationId := (*room.Operations)[0].ID.Hex()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	// расход уже правили из нового приложения — версия ушла вперёд
	if rec := putOperation(t, s, room.ID.Hex(), operationId, testUser1.ID, "Ужин в баре", 200, 0); rec.code != http.StatusOK {
		t.Fatalf("подготовка: %d, тело: %s", rec.code, rec.body)
	}

	old := putOperation(t, s, room.ID.Hex(), operationId, testUser2.ID, "Такси", 300, -1)
	if old.code != http.StatusOK {
		t.Fatalf("правка из сборки без версий: %d, ожидался 200; тело: %s", old.code, old.body)
	}
	stored := (*repo.rooms[room.ID.Hex()].Operations)[0]
	if stored.Description != "Такси" {
		t.Fatalf("в базе %q — правка старой сборки не записалась", stored.Description)
	}
	if stored.Version <= 1 {
		t.Fatalf("версия %d — безусловная правка её не растит, и конкурент из приложения её не заметит", stored.Version)
	}
}
