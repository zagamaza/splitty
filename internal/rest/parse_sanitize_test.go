package rest

import (
	"testing"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
)

func members(ids ...int) []api.User {
	u := make([]api.User, 0, len(ids))
	for _, id := range ids {
		u = append(u, api.User{ID: id})
	}
	return u
}

func ptr(v int) *int { return &v }

func TestSanitize_DropsForeignUserId(t *testing.T) {
	d := ai.Draft{Items: []ai.DraftItem{{
		Name: "Пицца", Price: 300, Kind: "item",
		Shares: []ai.ItemShare{{UserId: 1, Weight: 1}, {UserId: 99, Weight: 1}}, // 99 не в комнате
	}}}
	got := sanitizeDraft(d, members(1, 2))
	if len(got.Items) != 1 || len(got.Items[0].Shares) != 1 || got.Items[0].Shares[0].UserId != 1 {
		t.Fatalf("чужой userId не убран: %+v", got.Items)
	}
}

func TestSanitize_NegativePriceDropped(t *testing.T) {
	d := ai.Draft{Items: []ai.DraftItem{
		{Name: "A", Price: -100, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
		{Name: "B", Price: 200, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
	}}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 1 || got.Items[0].Name != "B" {
		t.Fatalf("позиция с отрицательной ценой не отброшена: %+v", got.Items)
	}
}

func TestSanitize_ItemLimit(t *testing.T) {
	var its []ai.DraftItem
	for i := 0; i < maxDraftItems+10; i++ {
		its = append(its, ai.DraftItem{Name: "x", Price: 10, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}})
	}
	got := sanitizeDraft(ai.Draft{Items: its}, members(1))
	if len(got.Items) != maxDraftItems {
		t.Fatalf("лимит позиций не применён: %d", len(got.Items))
	}
}

func TestSanitize_SumRecomputed(t *testing.T) {
	d := ai.Draft{
		Sum: 99999, // модель наврала
		Items: []ai.DraftItem{
			{Name: "A", Price: 300, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
			{Name: "Сбор", Price: 30, Kind: "surcharge", Split: "proportional"},
		},
	}
	got := sanitizeDraft(d, members(1))
	if got.Sum != 330 {
		t.Fatalf("Sum не пересчитан: %d, want 330", got.Sum)
	}
}

func TestSanitize_SurchargeDefaultSplit(t *testing.T) {
	d := ai.Draft{Items: []ai.DraftItem{
		{Name: "A", Price: 100, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
		{Name: "Сбор", Price: 10, Kind: "surcharge", Split: "мусор"},
	}}
	got := sanitizeDraft(d, members(1))
	var sur *ai.DraftItem
	for i := range got.Items {
		if got.Items[i].Kind == "surcharge" {
			sur = &got.Items[i]
		}
	}
	if sur == nil || sur.Split != "proportional" {
		t.Fatalf("split надбавки не нормализован: %+v", sur)
	}
}

func TestSanitize_SurchargeZeroPriceDropped(t *testing.T) {
	d := ai.Draft{Items: []ai.DraftItem{
		{Name: "A", Price: 100, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
		{Name: "Сбор", Price: 0, Kind: "surcharge", Percent: ptr(10)},
	}}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 1 || got.Items[0].Kind != "item" {
		t.Fatalf("надбавка с нулевой ценой не отброшена: %+v", got.Items)
	}
}

func TestSanitize_ForeignDonorCleared(t *testing.T) {
	d := ai.Draft{DonorId: ptr(99), Items: []ai.DraftItem{
		{Name: "A", Price: 100, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
	}}
	got := sanitizeDraft(d, members(1))
	if got.DonorId != nil {
		t.Fatalf("чужой donorId не сброшен: %v", *got.DonorId)
	}
}

func TestSanitize_KeepsUnknownItem(t *testing.T) {
	// позиция без валидных долей, но с нераспознанным именем — сохраняется
	d := ai.Draft{Items: []ai.DraftItem{{
		Name: "Пиво", Price: 200, Kind: "item", Unknown: []string{"Саня"},
	}}}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 1 || len(got.Items[0].Unknown) != 1 {
		t.Fatalf("позиция с Unknown отброшена: %+v", got.Items)
	}
}

func TestHasUnknown(t *testing.T) {
	with := ai.Draft{Items: []ai.DraftItem{{Unknown: []string{"Саня"}}}}
	without := ai.Draft{Items: []ai.DraftItem{{Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}}}}
	if !hasUnknown(with) {
		t.Fatal("hasUnknown должен вернуть true")
	}
	if hasUnknown(without) {
		t.Fatal("hasUnknown должен вернуть false")
	}
}
