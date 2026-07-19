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

func TestSanitize_NegativePriceBecomesUndefined(t *testing.T) {
	// Отрицательная цена — галлюцинация: превращается в price=0
	// («цена не определена»), участники позиции при этом не теряются.
	d := ai.Draft{Items: []ai.DraftItem{
		{Name: "A", Price: -100, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
		{Name: "B", Price: 200, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
	}}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 2 || got.Items[0].Price != 0 {
		t.Fatalf("отрицательная цена не обнулена: %+v", got.Items)
	}
	if got.Sum != 200 {
		t.Fatalf("Sum: %d, want 200", got.Sum)
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

func TestSanitize_FlatDraftKeepsModelSum(t *testing.T) {
	// Плоский черновик (позиций нет — цены не названы): сумма модели живёт,
	// иначе распознанное «такси 400» затиралось бы в 0.
	d := ai.Draft{Description: "Такси", Sum: 400}
	got := sanitizeDraft(d, members(1))
	if got.Sum != 400 || got.Description != "Такси" {
		t.Fatalf("плоский черновик испорчен: %+v", got)
	}
}

func TestSanitize_KeepsPricelessItemWithShares(t *testing.T) {
	// Позиция без цены (price=0 — «цена не определена»), но с участниками —
	// живёт в черновике: раскладку «кто что ел» терять нельзя, цену доспросит UI.
	d := ai.Draft{
		Sum: 1200,
		Items: []ai.DraftItem{
			{Name: "Пицца", Price: 0, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
			{Name: "Салат", Price: 300, Kind: "item", Shares: []ai.ItemShare{{UserId: 1, Weight: 1}}},
		},
	}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 2 {
		t.Fatalf("позиция без цены выброшена: %+v", got.Items)
	}
	// Sum — по известным ценам (0 у неопределённой).
	if got.Sum != 300 {
		t.Fatalf("Sum: %d, want 300", got.Sum)
	}
}

func TestSanitize_PricelessItemWithoutSharesDropped(t *testing.T) {
	// Без цены И без участников/unknown — мусор, выбрасывается.
	d := ai.Draft{Sum: 500, Items: []ai.DraftItem{{Name: "Пицца", Price: 0, Kind: "item"}}}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 0 {
		t.Fatalf("пустышка не выброшена: %+v", got.Items)
	}
	// Черновик стал плоским — сумма модели сохраняется.
	if got.Sum != 500 {
		t.Fatalf("сумма плоского черновика затёрта: %d, want 500", got.Sum)
	}
}

func TestSanitize_FlatSumClamped(t *testing.T) {
	// Бредовые значения суммы плоского черновика обнуляются. Граница — та же,
	// что у write-path (maxItemsTotal): более строгий порог обнулял бы суммы,
	// которые сохранить МОЖНО (валюты вроде IDR/VND).
	for _, sum := range []int{-5, maxItemsTotal + 1} {
		got := sanitizeDraft(ai.Draft{Sum: sum}, members(1))
		if got.Sum != 0 {
			t.Fatalf("sum %d не обнулён: %d", sum, got.Sum)
		}
	}
	// В пределах write-path сумма сохраняется, а не теряется.
	if got := sanitizeDraft(ai.Draft{Sum: maxItemPrice + 1}, members(1)); got.Sum != maxItemPrice+1 {
		t.Fatalf("сохраняемая сумма затёрта: %d", got.Sum)
	}
}

func TestSanitize_SurchargeOnlyCollapsesToFlat(t *testing.T) {
	// Чек из одних надбавок (модель пометила такси как surcharge) невалиден —
	// схлопывается в плоский черновик с сохранением суммы.
	d := ai.Draft{
		Description: "Такси",
		Sum:         400,
		Items: []ai.DraftItem{
			{Name: "Такси", Price: 400, Kind: "surcharge", Split: "proportional"},
		},
	}
	got := sanitizeDraft(d, members(1))
	if len(got.Items) != 0 {
		t.Fatalf("surcharge-only чек не схлопнут: %+v", got.Items)
	}
	if got.Sum != 400 {
		t.Fatalf("сумма потеряна: %d, want 400", got.Sum)
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
