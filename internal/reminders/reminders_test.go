package reminders

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	zagir = api.User{ID: 1, DisplayName: "Загир"}
	almaz = api.User{ID: 2, DisplayName: "Алмаз"}
	sanya = api.User{ID: 3, DisplayName: "Саня"}
)

// room собирает комнату с одним расходом: donor заплатил sum за всех.
func room(name, currency string, at time.Time, donor api.User, members ...api.User) api.Room {
	const sum = 600
	shares := make([]api.RecipientWithSum, 0, len(members))
	each := sum / len(members)
	for _, m := range members {
		shares = append(shares, api.RecipientWithSum{User: m, Sum: float64(each)})
	}
	d := donor
	return api.Room{
		ID:       primitive.NewObjectID(),
		Name:     name,
		Currency: currency,
		CreateAt: at,
		Members:  &members,
		Operations: &[]api.Operation{{
			ID:                primitive.NewObjectID(),
			Description:       "Ужин",
			Sum:               sum,
			Donor:             &d,
			RecipientsWithSum: shares,
			Status:            api.StatusActive,
			CreateAt:          at,
		}},
	}
}

func collector(now time.Time) *Collector {
	return &Collector{Now: now, MaxIdle: 60 * 24 * time.Hour}
}

// Должник попадает в рассылку, кредитор — нет: напоминание адресовано тому, кто
// не вернул.
func TestOnlyDebtorsAreTargeted(t *testing.T) {
	now := time.Now().UTC()
	c := collector(now)
	c.Add([]api.Room{room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)})

	targets := c.Targets()
	if len(targets) != 1 {
		t.Fatalf("целей %d: %+v", len(targets), targets)
	}
	if targets[0].UserId != almaz.ID {
		t.Errorf("напоминание ушло не должнику: %d", targets[0].UserId)
	}
	if targets[0].Groups != 1 || targets[0].RoomName != "Стамбул" {
		t.Errorf("цель собрана неверно: %+v", targets[0])
	}
}

// Мёртвая группа: последнее движение было слишком давно. Дата создания комнаты
// для этого не годится — важно, когда в ней последний раз что-то происходило.
func TestSilentRoomIsSkipped(t *testing.T) {
	now := time.Now().UTC()
	c := collector(now)
	c.Add([]api.Room{room("Забытая", "RUB", now.AddDate(0, 0, -200), zagir, zagir, almaz)})

	if got := c.Targets(); len(got) != 0 {
		t.Errorf("напомнили по мёртвой группе: %+v", got)
	}
}

// Архив персональный: убрал группу из своего списка — напоминать по ней не надо.
func TestArchivedRoomIsSkippedForThatUserOnly(t *testing.T) {
	now := time.Now().UTC()
	r := room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz, sanya)
	r.RoomStates.Archived = []int{almaz.ID}

	c := collector(now)
	c.Add([]api.Room{r})

	targets := c.Targets()
	if len(targets) != 1 {
		t.Fatalf("целей %d: %+v", len(targets), targets)
	}
	if targets[0].UserId != sanya.ID {
		t.Errorf("архив не учтён: напомнили %d", targets[0].UserId)
	}
}

// Один пуш на всё: должен в двух группах — одна цель с суммой по обеим.
func TestDebtsAcrossRoomsAreAggregated(t *testing.T) {
	now := time.Now().UTC()
	c := collector(now)
	c.Add([]api.Room{
		room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz),
		room("Квартира", "RUB", now.AddDate(0, 0, -5), sanya, sanya, almaz),
	})

	targets := c.Targets()
	if len(targets) != 1 {
		t.Fatalf("целей %d — должно быть одно уведомление на человека: %+v", len(targets), targets)
	}
	got := targets[0]
	if got.Groups != 2 {
		t.Errorf("групп %d, ожидалось 2", got.Groups)
	}
	if len(got.Totals) != 1 || got.Totals[0].Sum != 600 {
		t.Errorf("сумма собрана неверно: %+v", got.Totals)
	}
	// Деплинк ведёт в комнату с самым крупным долгом; здесь суммы равны, но
	// комната обязана быть заполнена — без неё тап не открывает ничего.
	if got.RoomId == "" {
		t.Error("цель без комнаты: тап по пушу никуда не приведёт")
	}
}

// Валюты не складываются между собой: курсов у нас нет.
func TestCurrenciesAreNotMixed(t *testing.T) {
	now := time.Now().UTC()
	c := collector(now)
	c.Add([]api.Room{
		room("Стамбул", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz),
		room("Париж", "EUR", now.AddDate(0, 0, -4), zagir, zagir, almaz),
	})

	got := c.Targets()[0]
	if len(got.Totals) != 2 {
		t.Fatalf("валют %d, ожидалось 2: %+v", len(got.Totals), got.Totals)
	}
	for _, total := range got.Totals {
		if total.Sum != 300 {
			t.Errorf("валюта %s: сумма %d", total.Currency, total.Sum)
		}
	}
}

// Отпечаток не зависит от порядка комнат, но меняется вместе с суммой: на нём
// держится различение эпизодов долга.
func TestFingerprintIsStableAndSensitive(t *testing.T) {
	a := roomDebt{roomId: "r1", currency: "RUB", sum: 100}
	b := roomDebt{roomId: "r2", currency: "RUB", sum: 200}

	if fingerprint([]roomDebt{a, b}) != fingerprint([]roomDebt{b, a}) {
		t.Error("отпечаток зависит от порядка комнат — серия сбрасывалась бы на ровном месте")
	}
	changed := roomDebt{roomId: "r2", currency: "RUB", sum: 250}
	if fingerprint([]roomDebt{a, b}) == fingerprint([]roomDebt{a, changed}) {
		t.Error("отпечаток не заметил смены суммы — про новый долг молчали бы")
	}
}

// Комнаты с неисчислимыми долгами не дают целей и считаются отдельно: по этому
// счётчику джоб решает, можно ли верить выводу «долгов не осталось».
func TestUncountableRoomIsSkippedAndCounted(t *testing.T) {
	now := time.Now().UTC()
	// Операция с долями, не сходящимися с суммой, — легаси-форма бота.
	broken := room("Легаси", "RUB", now.AddDate(0, 0, -3), zagir, zagir, almaz)
	(*broken.Operations)[0].RecipientsWithSum = []api.RecipientWithSum{
		{User: zagir, Sum: 1},
		{User: almaz, Sum: 1},
	}

	c := collector(now)
	c.Add([]api.Room{broken})

	if got := c.Targets(); len(got) != 0 {
		t.Errorf("напомнили по неисчислимой комнате: %+v", got)
	}
	if c.Skipped != 1 {
		t.Errorf("пропущено %d комнат — джоб не узнает, что расчёт был неполным", c.Skipped)
	}
}
