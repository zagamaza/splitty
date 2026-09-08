package bot

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Запись версий расхода: черновик правки и прежняя активная версия.
type editRecordingOperationService struct {
	OperationService
	updated  []api.Operation
	deleted  []primitive.ObjectID
	activated *api.Operation
}

func (o *editRecordingOperationService) UpdateOperation(_ context.Context, op *api.Operation, _ string) error {
	o.updated = append(o.updated, *op)
	return nil
}

func (o *editRecordingOperationService) DeleteOperation(_ context.Context, _ string, id primitive.ObjectID) (bool, error) {
	o.deleted = append(o.deleted, id)
	return true, nil
}

func (o *editRecordingOperationService) ActivateOperation(_ context.Context, op *api.Operation, _ string) error {
	o.activated = op
	return nil
}

// Отказ на подтверждении правки НЕ ДОЛЖЕН терять расход.
//
// Вход в редактор бота заранее архивирует прежнюю версию и заводит черновик.
// Если подтверждение отклонить и уйти молча, в долгах не останется ни той
// версии, ни другой — расход исчезнет у всех участников.
func TestRejectedFractionalEditRestoresPreviousVersion(t *testing.T) {
	members := []api.User{{ID: 1, DisplayName: "A"}, {ID: 2, DisplayName: "B"}}
	oldID := primitive.NewObjectID()
	draftID := primitive.NewObjectID()

	// Прежняя версия: дробный расход 20,80 пополам, записанный до отката.
	oldMinor := int64(2080)
	previous := api.Operation{
		ID: oldID, Description: "Ужин", Sum: 21, SumMinor: &oldMinor,
		Donor: &members[0], Status: archive, SplitType: equally,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: members[0], SumMinor: ptrMinor(1040)},
			{User: members[1], SumMinor: ptrMinor(1040)},
		},
	}
	// Черновик правки: деньги ИЗМЕНЕНЫ (30,90) — при опущенном рубильнике нельзя.
	draftMinor := int64(3090)
	draft := api.Operation{
		ID: draftID, Description: "Ужин", Sum: 31, SumMinor: &draftMinor,
		Donor: &members[0], Status: draft, SplitType: equally,
		OldOperationId: &oldID,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: members[0], SumMinor: ptrMinor(1545)},
			{User: members[1], SumMinor: ptrMinor(1545)},
		},
	}

	ops := []api.Operation{previous, draft}
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Туса", Currency: "RUB",
		Members: &members, Operations: &ops,
	}

	os := &editRecordingOperationService{}
	screen := NewOperationAdded(noopChatStateService{}, noopButtonService{},
		&recordingRoomService{room: room}, os,
		stubBotUserService{users: map[int]*api.User{1: &members[0], 2: &members[1]}}, &Config{})

	upd := canonicalUpdate(addedOperation, &api.CallbackData{
		RoomId: room.ID.Hex(), OperationId: draftID,
	})
	upd.Button = &api.Button{Action: addedOperation, CallbackData: upd.Button.CallbackData}

	screen.OnMessage(context.Background(), upd)

	if os.activated != nil {
		t.Fatal("черновик активирован при опущенном рубильнике")
	}
	var restored bool
	for _, op := range os.updated {
		if op.ID == oldID && op.Status == active {
			restored = true
		}
	}
	if !restored {
		t.Error("прежняя версия не возвращена в долги — расход исчез у всех участников")
	}
	var draftDropped bool
	for _, id := range os.deleted {
		if id == draftID {
			draftDropped = true
		}
	}
	if !draftDropped {
		t.Error("отклонённый черновик остался висеть")
	}
}

// Правка БЕЗ изменения денег проходит даже при опущенном рубильнике: иначе
// дробный расход нельзя было бы даже переименовать.
func TestUnchangedMoneyEditPassesWhileFlagOff(t *testing.T) {
	old := api.Operation{
		Sum: 21, SumMinor: ptrMinor(2080), Donor: &api.User{ID: 1},
		RecipientsWithSum: []api.RecipientWithSum{
			{User: api.User{ID: 1}, SumMinor: ptrMinor(1040)},
			{User: api.User{ID: 2}, SumMinor: ptrMinor(1040)},
		},
	}
	renamed := old
	renamed.Description = "Ужин с друзьями"
	if !sameOperationMoney(&old, &renamed) {
		t.Error("переименование сочтено сменой денег")
	}

	// Смена плательщика — смена обязательства, а не переименование.
	otherDonor := old
	otherDonor.Donor = &api.User{ID: 2}
	if sameOperationMoney(&old, &otherDonor) {
		t.Error("смена плательщика прошла как неизменные деньги")
	}
}

func ptrMinor(v int64) *int64 { return &v }
