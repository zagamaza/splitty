package service

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/stretchr/testify/assert"
)

// Тест для ручного формирования операций
func TestGetRoomDebts(t *testing.T) {
	// Создадим набор пользователей
	m := []api.User{
		{ID: 0, DisplayName: "A"},
		{ID: 1, DisplayName: "B"},
		{ID: 2, DisplayName: "C"},
		{ID: 3, DisplayName: "D"},
		{ID: 4, DisplayName: "E"},
	}
	o := []api.Operation{
		{
			Donor:             &m[2],
			DonorsWithSum:     []api.DonorWithSum{{User: m[2], Sum: 1}},
			RecipientsWithSum: []api.RecipientWithSum{{User: m[3], Sum: 1}},
			Status:            "active",
			Sum:               1,
		},
		{
			Donor:             &m[2],
			DonorsWithSum:     []api.DonorWithSum{{User: m[2], Sum: 10}},
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 10}},
			Status:            "active",
			Sum:               10,
		},
		{
			Donor:             &m[4],
			DonorsWithSum:     []api.DonorWithSum{{User: m[4], Sum: 10}},
			RecipientsWithSum: []api.RecipientWithSum{{User: m[1], Sum: 10}},
			Status:            "active",
			Sum:               10,
		},
	}
	room := api.Room{
		Members:    &m,
		Operations: &o,
	}

	debt, _ := GetRoomDebts(room)
	var debtForAssert [][]interface{}
	for _, d := range debt {
		debtForAssert = append(debtForAssert, []interface{}{d.Debtor.DisplayName, d.Lender.DisplayName, d.Sum})
	}
	assert.ElementsMatch(t, debtForAssert, [][]interface{}{
		{"A", "C", 10},
		{"B", "E", 10},
		{"D", "C", 1},
	})

	o = append(o, api.Operation{
		Donor:             &m[3],
		DonorsWithSum:     []api.DonorWithSum{{User: m[3], Sum: 1}},
		RecipientsWithSum: []api.RecipientWithSum{{User: m[2], Sum: 1}},
		Status:            "active",
		Sum:               1,
		IsDebtRepayment:   true,
	})
	room.Operations = &o
	debt, _ = GetRoomDebts(room)
	debtForAssert = [][]interface{}{}
	for _, d := range debt {
		debtForAssert = append(debtForAssert, []interface{}{d.Debtor.DisplayName, d.Lender.DisplayName, d.Sum})
	}

	assert.ElementsMatch(t, debtForAssert, [][]interface{}{
		{"A", "C", 10},
		{"B", "E", 10},
	})

}

// Тест с данными из файла (тестовые данные должны представлять сбалансированную ситуацию, т.е. долгов не должно оставаться)
func TestGetRoomDebtsByTestData(t *testing.T) {
	dat, err := ioutil.ReadFile("test_room.json")
	assert.NoError(t, err)

	room := &api.Room{}
	err = json.Unmarshal(dat, room)
	assert.NoError(t, err)

	debts, err := GetRoomDebts(*room)
	assert.NoError(t, err)
	// По тестовым данным ожидаем, что все долги взаимно погашены
	assert.Empty(t, debts)
}
