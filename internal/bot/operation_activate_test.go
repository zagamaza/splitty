package bot

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Активация черновика в боте («Готово») — это рождение долга, и состав комнаты
// для неё важнее прочитанного снимка. Черновик живёт минутами и всё это время
// НИКОГО не держит в комнате: и api.HasOperations, и фильтр LeaveRoom смотрят
// только на активные операции. Значит получатель успевает выйти между
// созданием черновика и «Готово».

// activateRoomService — RoomService с одной комнатой; остальное не нужно.
type activateRoomService struct {
	RoomService
	room *api.Room
}

func (s *activateRoomService) FindById(context.Context, string) (*api.Room, error) {
	return s.room, nil
}

// activateOperationService повторяет семантику mongo: активация проходит,
// только если все связываемые расходом люди — участники комнаты.
type activateOperationService struct {
	OperationService
	room      *api.Room
	activated []api.Operation
	updated   []api.Operation
	deleted   []primitive.ObjectID
}

func (s *activateOperationService) ActivateOperation(_ context.Context, o *api.Operation, _ string) error {
	if o.Donor != nil && !roomHasMember(s.room, o.Donor.ID) {
		return repository.ErrParticipantLeft
	}
	for _, r := range o.RecipientsWithSum {
		if !roomHasMember(s.room, r.User.ID) {
			return repository.ErrParticipantLeft
		}
	}
	s.activated = append(s.activated, *o)
	return nil
}

func (s *activateOperationService) UpdateOperation(_ context.Context, o *api.Operation, _ string) error {
	s.updated = append(s.updated, *o)
	return nil
}

func (s *activateOperationService) DeleteOperation(_ context.Context, _ string, id primitive.ObjectID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func roomHasMember(r *api.Room, id int) bool {
	if r.Members == nil {
		return false
	}
	for _, m := range *r.Members {
		if m.ID == id {
			return true
		}
	}
	return false
}

// activateUserService — канонических документов нет: без telegram_id бот
// сообщений никому не шлёт, а тесту важна запись операции, а не рассылка.
type activateUserService struct {
	UserService
}

func (activateUserService) FindByIds(context.Context, []int) ([]api.User, error) { return nil, nil }

func (activateUserService) FindById(_ context.Context, id int) (*api.User, error) {
	return &api.User{ID: id}, nil
}

// addedOperationUpdate — тап по «Готово» на экране собранного черновика.
func addedOperationUpdate(room *api.Room, op api.Operation, user *api.User) *api.Update {
	return &api.Update{
		User: user,
		Button: &api.Button{
			ID:           primitive.NewObjectID(),
			Action:       addedOperation,
			CallbackData: &api.CallbackData{RoomId: room.ID.Hex(), OperationId: op.ID},
		},
		CallbackQuery: &api.CallbackQuery{ID: "cb"},
	}
}

func activateFixture(t *testing.T, members []api.User, op api.Operation) (*OperationAdded, *activateOperationService, *api.Room) {
	t.Helper()
	loadLang(t)
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира", Currency: "RUB",
		Members: &members, Operations: &[]api.Operation{op},
	}
	os := &activateOperationService{room: room}
	h := NewOperationAdded(nil, noopButtonService{}, &activateRoomService{room: room}, os, activateUserService{}, &Config{})
	return h, os, room
}

// TestOperationAddedRefusesWhenRecipientLeft — головной случай: получатель
// черновика вышел, пока черновик собирали. Записать его в долг молча нельзя —
// комнату он уже не видит и убрать себя из расхода не сможет.
func TestOperationAddedRefusesWhenRecipientLeft(t *testing.T) {
	donor := api.User{ID: 1, DisplayName: "Автор"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:    &donor,
		Status:   draft,
		CreateAt: time.Now(),
		RecipientsWithSum: []api.RecipientWithSum{
			{User: donor, Sum: 50}, {User: leaver, Sum: 50},
		},
	}
	// в комнате остался только автор: получатель вышел, черновик его не держал
	h, os, room := activateFixture(t, []api.User{donor}, op)

	res := h.OnMessage(context.Background(), addedOperationUpdate(room, op, &donor))

	if len(os.activated) != 0 {
		t.Fatal("расход записан на не-участника — долг у того, кто комнату не видит")
	}
	if len(os.updated) != 0 {
		t.Fatalf("операция записана мимо проверки состава: %+v", os.updated)
	}
	if res.CallbackConfig == nil || res.CallbackConfig.Text == "" {
		t.Fatal("человеку не сказали, почему расход не записался")
	}
}

// TestOperationAddedActivatesWhenAllPresent — обратная сторона: пока состав на
// месте, «Готово» обязано доводить черновик до действующего расхода.
func TestOperationAddedActivatesWhenAllPresent(t *testing.T) {
	donor := api.User{ID: 1, DisplayName: "Автор"}
	other := api.User{ID: 2, DisplayName: "Гость"}
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:    &donor,
		Status:   draft,
		CreateAt: time.Now(),
		RecipientsWithSum: []api.RecipientWithSum{
			{User: donor, Sum: 50}, {User: other, Sum: 50},
		},
	}
	h, os, room := activateFixture(t, []api.User{donor, other}, op)

	h.OnMessage(context.Background(), addedOperationUpdate(room, op, &donor))

	if len(os.activated) != 1 {
		t.Fatalf("черновик не стал расходом: %+v", os.activated)
	}
	if os.activated[0].Status != active {
		t.Fatalf("статус записанной операции %q", os.activated[0].Status)
	}
	if os.activated[0].OldOperationId != nil {
		t.Fatal("в активированной операции осталась ссылка на черновик правки")
	}
}

// TestOperationAddedKeepsOldVersionWhenActivationRefused — правка расхода в
// боте это новая запись плюс удаление прошлой. Удали мы прошлую версию до
// отказа активации — расход исчез бы целиком: старой версии нет, новая
// осталась черновиком, и в долгах нет ни той, ни другой.
func TestOperationAddedKeepsOldVersionWhenActivationRefused(t *testing.T) {
	donor := api.User{ID: 1, DisplayName: "Автор"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	oldID := primitive.NewObjectID()
	oldOp := api.Operation{
		ID: oldID, Description: "Ужин", Sum: 100, Donor: &donor, Status: archive,
		RecipientsWithSum: []api.RecipientWithSum{{User: donor, Sum: 50}, {User: leaver, Sum: 50}},
	}
	draftOp := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин на двоих", Sum: 100,
		Donor: &donor, Status: draft, OldOperationId: &oldID, CreateAt: time.Now(),
		RecipientsWithSum: []api.RecipientWithSum{{User: donor, Sum: 40}, {User: leaver, Sum: 60}},
	}
	h, os, room := activateFixture(t, []api.User{donor}, draftOp)
	ops := []api.Operation{oldOp, draftOp}
	room.Operations = &ops

	h.OnMessage(context.Background(), addedOperationUpdate(room, draftOp, &donor))

	if len(os.deleted) != 0 {
		t.Fatal("прошлая версия расхода удалена, а новая осталась черновиком — расход потерян")
	}
}
