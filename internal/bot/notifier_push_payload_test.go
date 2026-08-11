package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Payload пуша по операции закреплён здесь на СЕРВЕРНОЙ стороне — как ключи
// приглашения в notifier_invite_test.go. Клиентские тесты (PushRouteTests.swift,
// PushRouteTest.kt) сверяются с теми же литералами, но общего кода с этим
// файлом у них нет: без такого теста переименование ключа или возврат к
// fmt-форматированию id прошли бы с зелёными тестами на всех трёх сторонах.

// TestOperationPushPayloadKeys — deeplink живёт ровно на этих четырёх ключах.
func TestOperationPushPayloadKeys(t *testing.T) {
	loadLang(t)
	pushes := &capturePayload{}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		3: pushableUser(3),
	}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	r := room()
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), r, op, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("ожидался один push, отправлено %d", len(pushes.notifs))
	}
	data := pushes.notifs[0].Data
	if data["channel"] != "operations" {
		t.Errorf("channel = %q, ожидался operations (без него Android 8+ не покажет фоновый пуш)", data["channel"])
	}
	if data["type"] != "operation" {
		t.Errorf("type = %q, ожидался operation", data["type"])
	}
	if data["roomId"] != r.ID.Hex() {
		t.Errorf("roomId = %q, ожидался %q", data["roomId"], r.ID.Hex())
	}
	// Главное: id операции — тот же hex, что уходит в поле `id` REST-ответа.
	// fmt.Sprintf("%v", op.ID) давал `ObjectID("68f2…")`, и клиент искал бы в
	// комнате операцию с несуществующим id — тап открывал бы «не найдено».
	if data["operationId"] != op.ID.Hex() {
		t.Errorf("operationId = %q, ожидался чистый hex %q", data["operationId"], op.ID.Hex())
	}
	if strings.Contains(data["operationId"], "ObjectID") {
		t.Errorf("operationId несёт обёртку типа: %q", data["operationId"])
	}
}

// TestDebtPushPayloadHasNoOperationId — возврат долга ведёт в комнату, а не в
// карточку: пустой/отсутствующий operationId это и означает на обоих клиентах.
func TestDebtPushPayloadHasNoOperationId(t *testing.T) {
	loadLang(t)
	pushes := &capturePayload{}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Должник"),
		3: pushableUser(3),
	}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	r := room()
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Sum:               100,
		IsDebtRepayment:   true,
		Donor:             &api.User{ID: 1, DisplayName: "Должник"},
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Кредитор"}, Sum: 100}},
	}

	n.NotifyRepaymentCreated(context.Background(), r, op, api.User{ID: 1, DisplayName: "Должник"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("ожидался один push, отправлено %d", len(pushes.notifs))
	}
	data := pushes.notifs[0].Data
	if data["type"] != "debt" {
		t.Errorf("type = %q, ожидался debt", data["type"])
	}
	if data["roomId"] != r.ID.Hex() {
		t.Errorf("roomId = %q, ожидался %q", data["roomId"], r.ID.Hex())
	}
	if data["operationId"] != "" {
		t.Errorf("operationId = %q, ожидалось пусто: карточка погашения по пушу не открывается", data["operationId"])
	}
}

// pushableUser — канонический документ с push-токеном и без ограничений.
func pushableUser(id int) *api.User {
	return &api.User{ID: id, DisplayName: "Гость", PushTokens: []api.PushToken{{Token: "t"}}}
}
