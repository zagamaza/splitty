package ai

import (
	"context"
	"testing"
)

// Подставной разбор обязан быть предсказуемым: на нём держатся сквозные
// UI-прогоны, и «иногда другой чек» сделал бы их флейками.
func TestFakeParserBuildsSingleItemForAllParticipants(t *testing.T) {
	res, err := FakeParser{}.Parse(context.Background(), ParseInput{
		Text:         "Такси 100",
		Participants: []Participant{{UserId: 1}, {UserId: 2}, {UserId: 3}},
		RequesterId:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.Sum != 100 || res.Draft.Description != "Такси" {
		t.Fatalf("черновик не тот: %+v", res.Draft)
	}
	if len(res.Draft.Items) != 1 || len(res.Draft.Items[0].Shares) != 3 {
		t.Fatalf("ожидал одну позицию на троих: %+v", res.Draft.Items)
	}
	if res.Draft.DonorId == nil || *res.Draft.DonorId != 1 {
		t.Fatalf("плательщик — тот, кто надиктовал: %+v", res.Draft.DonorId)
	}
}

func TestFakeParserAsksWhenNoAmount(t *testing.T) {
	res, _ := FakeParser{}.Parse(context.Background(), ParseInput{Text: "такси"})
	if len(res.Questions) == 0 || len(res.Draft.Items) != 0 {
		t.Fatalf("без суммы — вопрос, а не выдуманный чек: %+v", res)
	}
}
