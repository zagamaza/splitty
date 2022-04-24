package service

import (
	"encoding/json"
	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"testing"
)

func TestGetRoomDebts(t *testing.T) {

	m := []api.User{
		{ID: 0, DisplayName: "A"},
		{ID: 1, DisplayName: "B"},
		{ID: 2, DisplayName: "C"},
		{ID: 3, DisplayName: "D"},
		{ID: 4, DisplayName: "E"},
	}
	o := []api.Operation{
		{Donor: &m[2], Recipients: &[]api.User{m[3]}, Sum: 1},
		{Donor: &m[2], Recipients: &[]api.User{m[0]}, Sum: 10},
		{Donor: &m[4], Recipients: &[]api.User{m[1]}, Sum: 10},
	}
	room := api.Room{
		Members:    &m,
		Operations: &o,
	}

	debt, _ := GetRoomDebts(room)
	var debtForAssert [][]interface{}
	for _, d := range *debt {
		debtForAssert = append(debtForAssert, []interface{}{d.Debtor.DisplayName, d.Lender.DisplayName, d.Sum})
	}
	assert.ElementsMatch(t, debtForAssert, [][]interface{}{
		{"A", "C", 10},
		{"B", "E", 10},
		{"D", "C", 1},
	})

	o = append(o, api.Operation{Donor: &m[3], Recipients: &[]api.User{m[2]}, Sum: 1, IsDebtRepayment: true})
	room.Operations = &o
	debt, _ = GetRoomDebts(room)
	debtForAssert = [][]interface{}{}
	for _, d := range *debt {
		debtForAssert = append(debtForAssert, []interface{}{d.Debtor.DisplayName, d.Lender.DisplayName, d.Sum})
	}

	assert.ElementsMatch(t, debtForAssert, [][]interface{}{
		{"A", "C", 10},
		{"B", "E", 10},
	})

}

func TestGetRoomDebtsByTestData(t *testing.T) {
	dat, err := ioutil.ReadFile("test_room.json")
	room := &api.Room{}
	err = json.Unmarshal([]byte(dat), &room)
	if err != nil {
		fmt.Println(err)
		assert.Empty(t, err)
		return
	}

	debt, _ := GetRoomDebts(*room)

	assert.Empty(t, debt)

}
