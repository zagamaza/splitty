package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// До этих тестов правка операции уходила ТОЛЬКО в telegram: pushToUser
// вызывался из создания, возврата долга и приглашения, а NotifyOperationUpdated
// заканчивался одним n.send. Человек, вошедший через Google, об изменении своей
// доли не узнавал вовсе.

func updOp(desc string, donorID int, recipientID int, sum float64) api.Operation {
	return api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       desc,
		Sum:               200,
		Donor:             &api.User{ID: donorID, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: recipientID, DisplayName: "Гость"}, Sum: sum}},
	}
}

// updNotifier: автор — 1, получатель — 3 (с push-токеном). prefs задаёт
// канонические настройки получателя.
func updNotifier(t *testing.T, recipient *api.User) (*Notifier, *capturePayload) {
	t.Helper()
	loadLang(t)
	pushes := &capturePayload{}
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: recipient}}
	return NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes), pushes
}

// TestUpdatePushOnShareChanged — изменение доли это деньги, пуш обязан уйти
// на дефолтных настройках.
func TestUpdatePushOnShareChanged(t *testing.T) {
	n, pushes := updNotifier(t, pushableUser(3))
	old := updOp("Чай", 1, 3, 124)
	upd := old
	upd.RecipientsWithSum = []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 83}}

	n.NotifyOperationUpdated(context.Background(), room(), old, upd, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("ожидался один push об изменении доли, отправлено %d", len(pushes.notifs))
	}
	body := pushes.notifs[0].Body
	if !strings.Contains(body, "долю") {
		t.Errorf("тело пуша не про долю: %q", body)
	}
	if pushes.notifs[0].Data["operationId"] == "" {
		t.Error("в пуше нет operationId — тап не откроет операцию")
	}
}

// TestUpdatePushSilentOnRename — переименование по умолчанию не пушится:
// «Чай -> Чай (красный) -> Чай (чёрный)» это очередь баннеров ни о чём.
func TestUpdatePushSilentOnRename(t *testing.T) {
	n, pushes := updNotifier(t, pushableUser(3))
	old := updOp("Чай", 1, 3, 100)
	upd := old
	upd.Description = "Чай (красный)"

	n.NotifyOperationUpdated(context.Background(), room(), old, upd, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 0 {
		t.Fatalf("переименование не должно пушиться по умолчанию, отправлено %d: %q",
			len(pushes.notifs), pushes.notifs[0].Body)
	}
}

// TestUpdatePushOnRenameWhenOptedIn — но если человек сам включил категорию,
// пуш приходит.
func TestUpdatePushOnRenameWhenOptedIn(t *testing.T) {
	on := true
	recipient := pushableUser(3)
	recipient.Notify = &api.NotifySettings{Edits: api.ChannelPrefs{Push: &on}}
	n, pushes := updNotifier(t, recipient)

	old := updOp("Чай", 1, 3, 100)
	upd := old
	upd.Description = "Чай (красный)"

	n.NotifyOperationUpdated(context.Background(), room(), old, upd, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("с включённой категорией ожидался один push, отправлено %d", len(pushes.notifs))
	}
	if body := pushes.notifs[0].Body; !strings.Contains(body, "Чай (красный)") {
		t.Errorf("в теле нет нового названия: %q", body)
	}
}

// TestUpdatePushNotToAuthor — автор правки не уведомляется о себе.
func TestUpdatePushNotToAuthor(t *testing.T) {
	author := pushableUser(1)
	author.DisplayName = "Автор"
	loadLang(t)
	pushes := &capturePayload{}
	finder := stubUserFinder{users: map[int]*api.User{1: author, 3: pushableUser(3)}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	old := updOp("Чай", 1, 3, 100)
	upd := old
	upd.RecipientsWithSum = []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 60}}

	n.NotifyOperationUpdated(context.Background(), room(), old, upd, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("ожидался ровно один push (получателю), отправлено %d", len(pushes.notifs))
	}
}

// TestDeletedOperationPushes — удаление операции доходит пушем: оно делегирует
// в NotifyOperationUpdated с пустым списком получателей.
func TestDeletedOperationPushes(t *testing.T) {
	n, pushes := updNotifier(t, pushableUser(3))
	op := updOp("Чай", 1, 3, 100)

	n.NotifyOperationDeleted(context.Background(), room(), op, api.User{ID: 1, DisplayName: "Автор"})

	if len(pushes.notifs) != 1 {
		t.Fatalf("удаление операции должно пушиться получателю, отправлено %d", len(pushes.notifs))
	}
	if body := pushes.notifs[0].Body; !strings.Contains(body, "убрал") {
		t.Errorf("неожиданное тело: %q", body)
	}
}
