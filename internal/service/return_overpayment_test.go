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

// Реальная комната, на которой долги перед переплатившим участником пропадали:
// Александр вернул Артуру и No Mercy больше своего расчётного долга по тратам,
// излишек 2626 терялся, и экран долгов показывал, что ему никто не должен.
func TestGetRoomDebtsByKazahRoom(t *testing.T) {
	dat, err := ioutil.ReadFile("test_room-kazah.json")
	assert.NoError(t, err)

	room := &api.Room{}
	err = json.Unmarshal(dat, room)
	assert.NoError(t, err)

	debts, err := GetRoomDebts(*room)
	assert.NoError(t, err)

	var got [][]interface{}
	for _, d := range debts {
		got = append(got, []interface{}{d.Debtor.DisplayName, d.Lender.DisplayName, d.Sum})
	}
	expected := [][]interface{}{
		{"Артур", "Александр", 1313},
		{"Алмаз", "No Mercy", 1313},
		{"Zagir Nurgaliev", "Артур", 9611},
		{"No Mercy", "Александр", 1313},
	}
	assert.Equal(t, expected, got)
}
