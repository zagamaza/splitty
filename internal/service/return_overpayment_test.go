package service

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/stretchr/testify/assert"
)

// Возврат больше расчётного долга: излишек должен стать долгом в обратную
// сторону, а не пропасть.
func TestReturnExceedsDebtCreatesReverseDebt(t *testing.T) {
	a := api.User{ID: 1, DisplayName: "A"}
	b := api.User{ID: 2, DisplayName: "B"}
	m := []api.User{a, b}

	o := []api.Operation{
		// A заплатил 100 за B
		{
			Donor:             &m[0],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[1], Sum: 100}},
			Status:            "active",
			Sum:               100,
		},
		// B вернул A 300 — на 200 больше долга
		{
			Donor:             &m[1],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 300}},
			Status:            "active",
			Sum:               300,
			IsDebtRepayment:   true,
		},
	}
	room := api.Room{Members: &m, Operations: &o}

	debts, err := GetRoomDebts(room)
	assert.NoError(t, err)
	assert.Len(t, debts, 1)
	assert.Equal(t, a.ID, debts[0].Debtor.ID)
	assert.Equal(t, b.ID, debts[0].Lender.ID)
	assert.Equal(t, 200, debts[0].Sum)
}

// Регрессия: возврат не должен учитываться дважды (прямой + общий баланс) и
// съедать долги по новым тратам.
func TestNewDebtSurvivesAfterEarlierReturn(t *testing.T) {
	a := api.User{ID: 1, DisplayName: "A"}
	b := api.User{ID: 2, DisplayName: "B"}
	m := []api.User{a, b}

	o := []api.Operation{
		// A заплатил 1000 за B
		{
			Donor:             &m[0],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[1], Sum: 1000}},
			Status:            "active",
			Sum:               1000,
		},
		// B вернул долг
		{
			Donor:             &m[1],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 1000}},
			Status:            "active",
			Sum:               1000,
			IsDebtRepayment:   true,
		},
		// A снова заплатил 1000 за B
		{
			Donor:             &m[0],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[1], Sum: 1000}},
			Status:            "active",
			Sum:               1000,
		},
	}
	room := api.Room{Members: &m, Operations: &o}

	debts, err := GetRoomDebts(room)
	assert.NoError(t, err)
	assert.Len(t, debts, 1)
	assert.Equal(t, b.ID, debts[0].Debtor.ID)
	assert.Equal(t, a.ID, debts[0].Lender.ID)
	assert.Equal(t, 1000, debts[0].Sum)
}

// Возврат «в обратную сторону» (кредитор перевёл своему же должнику): долг из
// трат и обратный долг из возврата — одна пара, в ответе должна быть одна
// запись с общей суммой, а не две
func TestReverseReturnMergesWithExistingDebt(t *testing.T) {
	a := api.User{ID: 1, DisplayName: "A"}
	b := api.User{ID: 2, DisplayName: "B"}
	m := []api.User{a, b}

	o := []api.Operation{
		// B заплатил 100 за A — A должен B
		{
			Donor:             &m[1],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 100}},
			Status:            "active",
			Sum:               100,
		},
		// B (кредитор) перевёл A ещё 10 операцией возврата — долг A растёт до 110
		{
			Donor:             &m[1],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 10}},
			Status:            "active",
			Sum:               10,
			IsDebtRepayment:   true,
		},
	}
	room := api.Room{Members: &m, Operations: &o}

	debts, err := GetRoomDebts(room)
	assert.NoError(t, err)
	assert.Len(t, debts, 1, "долги одной пары должны быть слиты в одну запись")
	assert.Equal(t, a.ID, debts[0].Debtor.ID)
	assert.Equal(t, b.ID, debts[0].Lender.ID)
	assert.Equal(t, 110, debts[0].Sum)
}

// Реальная комната, на которой долги перед переплатившим участником пропадали:
// Александр вернул Артуру и No Mercy больше своего расчётного долга по тратам,
// излишек 2626 терялся, и экран долгов показывал, что ему никто не должен.
func TestGetRoomDebtsByKazahRoom(t *testing.T) {
	dat, err := ioutil.ReadFile("test_room-kazah.json")
	assert.NoError(t, err)

	room := &api.Room{}
	err = json.Unmarshal(dat, room)
	assert.NoError(t, err)
	// Тот же шаг, что делает репозиторий на чтении: без него доли остаются
	// старыми дробными (33.333), в копейках не сходятся с итогом, и движок
	// честно выдаёт остатки в копейках, которых в проде не бывает.
	api.FillRoomMoney(room)

	debts, err := GetRoomDebts(*room)
	assert.NoError(t, err)

	// Проверяем ВЕКТОР позиций, а не пары. Прежнее ожидание фиксировало цепочку
	// «Артур→Александр 1313, No Mercy→Александр 1313, Алмаз→No Mercy 1313,
	// Zagir→Артур 9611» — четыре ребра на 13 550 ₽ при том, что чистых денег в
	// комнате на 10 926 ₽. Разница — транзит, который наводил старый движок:
	// он строил долги по одним расходам, а потом вычитал погашения по парам, и
	// непопавшие погашения разворачивались во встречные долги. Одна развёртка
	// транзит убирает; позиция Загира (−9611) при этом совпала в точности.
	assert.Equal(t, map[string]int{
		"Zagir Nurgaliev": -9611,
		"Алмаз":           -1315,
		"Артур":           8298,
		"Александр":       2627,
		"No Mercy":        1,
	}, netByName(debts))

	var total int
	for _, d := range debts {
		total += d.Sum
	}
	assert.Equal(t, 10926, total, "сумма рёбер равна сумме положительных позиций")
	assert.Len(t, debts, 4, "лишних транзитных рёбер быть не должно")
}
