package rest

import (
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// Правка расхода не двигает записанные доли.
//
// Полный PUT приходит и когда человек всего лишь переименовал расход. Доли при
// этом выводятся по ТЕКУЩЕМУ шагу тусы, поэтому после включения копеек
// переименование пересобрало бы записанные 34+33+33 в 33,34+33,33+33,33 и
// задним числом сдвинуло бы чьи-то долги. Это второе условие включения признака
// дробного ввода (docs/plans/20260905-fractional-amounts.md).
func TestEditKeepsRecordedShares(t *testing.T) {
	users := []api.User{{ID: 1}, {ID: 2}, {ID: 3}}
	recorded := []api.RecipientWithSum{
		{User: users[0], Sum: 34, SumMinor: ptr64(3400)},
		{User: users[1], Sum: 33, SumMinor: ptr64(3300)},
		{User: users[2], Sum: 33, SumMinor: ptr64(3300)},
	}
	old := api.Operation{
		Sum: 100, SumMinor: ptr64(10000),
		SplitType:         splitEqually,
		RecipientsWithSum: recorded,
	}
	// Тот же состав и та же сумма, но доли выведены с копеечным шагом.
	next := []api.RecipientWithSum{
		{User: users[0], Sum: 33.34, SumMinor: ptr64(3334)},
		{User: users[1], Sum: 33.33, SumMinor: ptr64(3333)},
		{User: users[2], Sum: 33.33, SumMinor: ptr64(3333)},
	}

	if !keepsRecordedShares(&old, 10000, splitEqually, next) {
		t.Error("переименование пересобрало доли — долги сдвинулись задним числом")
	}

	t.Run("смена суммы пересобирает", func(t *testing.T) {
		if keepsRecordedShares(&old, 12000, splitEqually, next) {
			t.Error("сумма изменилась, а доли остались прежними")
		}
	})

	t.Run("смена состава пересобирает", func(t *testing.T) {
		other := []api.RecipientWithSum{next[0], next[1], {User: api.User{ID: 9}}}
		if keepsRecordedShares(&old, 10000, splitEqually, other) {
			t.Error("состав изменился, а доли остались прежними")
		}
	})

	t.Run("деление по суммам не сохраняется", func(t *testing.T) {
		byAmount := old
		byAmount.SplitType = splitByExactAmount
		if keepsRecordedShares(&byAmount, 10000, splitByExactAmount, next) {
			t.Error("человек прислал свои суммы — их и надо записать")
		}
	})
}

func ptr64(v int64) *int64 { return &v }
