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
			RecipientsWithSum: []api.RecipientWithSum{{User: m[3], Sum: 1}},
			Status:            "active",
			Sum:               1,
		},
		{
			Donor:             &m[2],
			RecipientsWithSum: []api.RecipientWithSum{{User: m[0], Sum: 10}},
			Status:            "active",
			Sum:               10,
		},
		{
			Donor:             &m[4],
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
	assert.Equal(t, map[string]int{"A": -10, "B": -10, "C": 11, "D": -1, "E": 10}, netByName(debt))

	o = append(o, api.Operation{
		Donor:             &m[3],
		RecipientsWithSum: []api.RecipientWithSum{{User: m[2], Sum: 1}},
		Status:            "active",
		Sum:               1,
		IsDebtRepayment:   true,
	})
	room.Operations = &o
	debt, _ = GetRoomDebts(room)
	// D вернул C рубль — его позиция закрылась, остальные не изменились.
	assert.Equal(t, map[string]int{"A": -10, "B": -10, "C": 10, "E": 10}, netByName(debt))
}

// netByName — чистая позиция каждого участника: сколько ему должны минус
// сколько должен он. Проверять надо именно её, а не пары: жадная развёртка
// выбирает один из многих законных маршрутов расчёта, и после любой правки
// движка пары законно переподключаются, не меняя того, кто сколько получает.
func netByName(debts []api.Debt) map[string]int {
	net := map[string]int{}
	for _, d := range debts {
		net[d.Lender.DisplayName] += d.Sum
		net[d.Debtor.DisplayName] -= d.Sum
	}
	for k, v := range net {
		if v == 0 {
			delete(net, k)
		}
	}
	return net
}

// Тест с данными из файла (тестовые данные должны представлять сбалансированную ситуацию, т.е. долгов не должно оставаться)
func TestGetRoomDebtsByTestData(t *testing.T) {
	dat, err := ioutil.ReadFile("test_room.json")
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
	// По тестовым данным ожидаем, что все долги взаимно погашены
	assert.Empty(t, debts)
}
