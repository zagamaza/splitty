package ai

import (
	"context"
	"testing"
	"time"
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

func TestFakeParserSplitsItemsByComma(t *testing.T) {
	res, err := FakeParser{}.Parse(context.Background(), ParseInput{
		Text:         "Ужин: хачапури 1200, вино 2400",
		Participants: []Participant{{UserId: 1}, {UserId: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.Description != "Ужин" || res.Draft.Sum != 3600 || len(res.Draft.Items) != 2 {
		t.Fatalf("черновик не тот: %+v", res.Draft)
	}
	if res.Draft.Items[1].Name != "вино" || res.Draft.Items[1].Price != 2400 {
		t.Fatalf("вторая позиция не та: %+v", res.Draft.Items[1])
	}
}

func TestFakeParserAsksWhenOneItemHasNoAmount(t *testing.T) {
	res, _ := FakeParser{}.Parse(context.Background(), ParseInput{Text: "хачапури 1200, вино"})
	if len(res.Questions) == 0 || len(res.Draft.Items) != 0 {
		t.Fatalf("позиция без цены — вопрос, а не половина чека: %+v", res)
	}
}

func TestFakeParserUsesFallbackWithoutText(t *testing.T) {
	res, _ := FakeParser{Fallback: "Ужин: чай 300"}.Parse(context.Background(), ParseInput{})
	if res.Draft.Sum != 300 || res.Draft.Description != "Ужин" {
		t.Fatalf("без текста — запасная фраза: %+v", res)
	}
}

func TestFakeParserDelayRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := FakeParser{Delay: time.Hour}.Parse(ctx, ParseInput{Text: "чай 100"})
	if err == nil {
		t.Fatal("отменённый запрос не должен ждать задержку")
	}
}
