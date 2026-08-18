package api

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// NormalizedRoom — вход расчёта долгов, и зовут его теперь не только из REST.
// Проверяем ровно то, ради чего он существует: легаси-формы бота не должны ни
// ронять расчёт, ни тихо превращаться в «долгов нет».

// Легаси-операции лежат в базе с ПУСТЫМ status. Без нормализации они не
// проходят фильтр активных, и комната с долгами выглядела бы рассчитанной.
func TestNormalizedRoomKeepsLegacyOperations(t *testing.T) {
	donor := User{ID: 1, DisplayName: "Загир"}
	room := Room{
		ID:      primitive.NewObjectID(),
		Members: &[]User{donor, {ID: 2, DisplayName: "Алмаз"}},
		Operations: &[]Operation{
			// Легаси-форма: пустой status и доли только в recipients.
			{
				Description: "Ужин",
				Sum:         100,
				Donor:       &donor,
				Recipients:  &[]User{{ID: 1, DisplayName: "Загир"}, {ID: 2, DisplayName: "Алмаз"}},
			},
		},
	}

	norm := NormalizedRoom(&room)

	if got := len(*norm.Operations); got != 1 {
		t.Fatalf("операций после нормализации %d, легаси-расход потерян", got)
	}
	if (*norm.Operations)[0].Status != StatusActive {
		t.Errorf("статус не нормализован: %q", (*norm.Operations)[0].Status)
	}
	// Доли синтезированы из легаси-recipients — без них расчёт долгов пуст.
	if got := len((*norm.Operations)[0].RecipientsWithSum); got != 2 {
		t.Errorf("долей %d, ожидалось 2", got)
	}
}

// Архивные версии отредактированных расходов и драфты бота в расчёт не идут.
func TestNormalizedRoomDropsInactive(t *testing.T) {
	donor := User{ID: 1}
	shares := []RecipientWithSum{{User: donor, Sum: 100}}
	room := Room{
		Members: &[]User{donor},
		Operations: &[]Operation{
			{Description: "Живой", Sum: 100, Donor: &donor, Status: StatusActive, RecipientsWithSum: shares},
			{Description: "Архивный", Sum: 50, Donor: &donor, Status: StatusArchive, RecipientsWithSum: shares},
			{Description: "Драфт", Sum: 70, Donor: &donor, Status: StatusDraft, RecipientsWithSum: shares},
		},
	}

	norm := NormalizedRoom(&room)

	if got := len(*norm.Operations); got != 1 {
		t.Fatalf("операций %d, ожидалась одна активная", got)
	}
	if (*norm.Operations)[0].Description != "Живой" {
		t.Errorf("в расчёт попала не та операция: %q", (*norm.Operations)[0].Description)
	}
}

// GetRoomDebts разыменовывает Members и Operations без проверок: nil в них —
// это паника джоба на первой же кривой комнате.
func TestNormalizedRoomNeverReturnsNilSlices(t *testing.T) {
	cases := map[string]*Room{
		"пустая комната":  {},
		"без операций":    {Members: &[]User{{ID: 1}}},
		"без участников":  {Operations: &[]Operation{}},
		"nil вместо комнаты":  nil,
	}
	for name, room := range cases {
		norm := NormalizedRoom(room)
		if norm.Members == nil {
			t.Errorf("%s: Members = nil", name)
		}
		if norm.Operations == nil {
			t.Errorf("%s: Operations = nil", name)
		}
	}
}
