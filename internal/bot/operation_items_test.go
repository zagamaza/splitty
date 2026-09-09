package bot

import (
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

func intPtr(v int) *int { return &v }

func testRoom() *api.Room {
	members := []api.User{
		{ID: 1, DisplayName: "Аня"},
		{ID: 2, DisplayName: "Лёха"},
		{ID: 3, DisplayName: "Маша"},
	}
	return &api.Room{Members: &members, Currency: "RUB"}
}

func TestIsItemized(t *testing.T) {
	tests := []struct {
		name string
		op   api.Operation
		want bool
	}{
		{name: "nil items", op: api.Operation{}, want: false},
		{name: "empty items", op: api.Operation{Items: []api.OperationItem{}}, want: false},
		{name: "one item", op: api.Operation{Items: []api.OperationItem{{Name: "Пицца", Price: 1200}}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isItemized(tt.op); got != tt.want {
				t.Fatalf("isItemized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestItemLabel(t *testing.T) {
	tests := []struct {
		name string
		item api.OperationItem
		want string
	}{
		{name: "qty 1", item: api.OperationItem{Name: "Пицца", Qty: 1, Kind: api.ItemKindItem}, want: "Пицца"},
		{name: "qty n", item: api.OperationItem{Name: "Баурсаки", Qty: 10, Kind: api.ItemKindItem}, want: "Баурсаки ×10"},
		{name: "surcharge percent", item: api.OperationItem{Name: "Сбор", Percent: intPtr(10), Kind: api.ItemKindSurcharge}, want: "Сбор 10%"},
		{name: "surcharge no percent", item: api.OperationItem{Name: "Доставка", Kind: api.ItemKindSurcharge}, want: "Доставка"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := itemLabel(tt.item); got != tt.want {
				t.Fatalf("itemLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemParticipants(t *testing.T) {
	members := testRoom().Members

	weighted := api.OperationItem{
		Kind: api.ItemKindItem,
		Shares: []api.ItemShare{
			{UserId: 2, Weight: 5},
			{UserId: 1, Weight: 3},
			{UserId: 3, Weight: 2},
		},
	}
	if got := itemParticipants(weighted, members, "RUB"); got != "Лёха ×5, Аня ×3, Маша ×2" {
		t.Fatalf("weighted participants = %q", got)
	}

	fixed := api.OperationItem{
		Kind: api.ItemKindItem,
		Shares: []api.ItemShare{
			{UserId: 3, Amount: intPtr(500)},
			{UserId: 1, Weight: 1},
		},
	}
	if got := itemParticipants(fixed, members, "RUB"); got != "Маша (500 ₽), Аня" {
		t.Fatalf("fixed participants = %q", got)
	}

	unknown := api.OperationItem{Kind: api.ItemKindItem, Shares: []api.ItemShare{{UserId: 99, Weight: 1}}}
	if got := itemParticipants(unknown, members, "RUB"); got != "?" {
		t.Fatalf("unknown participant = %q", got)
	}

	surchargeProp := api.OperationItem{Kind: api.ItemKindSurcharge, Split: api.SplitProportional}
	if got := itemParticipants(surchargeProp, members, "RUB"); got != "пропорционально" {
		t.Fatalf("surcharge proportional = %q", got)
	}
	surchargeEq := api.OperationItem{Kind: api.ItemKindSurcharge, Split: api.SplitEqually}
	if got := itemParticipants(surchargeEq, members, "RUB"); got != "поровну" {
		t.Fatalf("surcharge equally = %q", got)
	}
}

func TestRenderOperationItems_Empty(t *testing.T) {
	op := api.Operation{Description: "обычная", Sum: 300}
	if got := renderOperationItems(op, testRoom()); got != "" {
		t.Fatalf("expected empty render for non-itemized op, got %q", got)
	}
}

func TestRenderOperationItems_Itemized(t *testing.T) {
	op := api.Operation{
		Items: []api.OperationItem{
			{Name: "Пицца", Price: 1200, Qty: 1, Kind: api.ItemKindItem, Shares: []api.ItemShare{
				{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1},
			}},
			{Name: "Баурсаки", Price: 500, Qty: 10, Kind: api.ItemKindItem, Shares: []api.ItemShare{
				{UserId: 2, Weight: 5}, {UserId: 1, Weight: 3}, {UserId: 3, Weight: 2},
			}},
			{Name: "Сбор", Price: 170, Kind: api.ItemKindSurcharge, Split: api.SplitProportional, Percent: intPtr(10)},
		},
	}

	got := renderOperationItems(op, testRoom())

	for _, want := range []string{
		"🧾 Позиции чека:",
		"Баурсаки ×10",
		"Сбор 10%",
		"Итого",
		"👥 Кто участвует:",
		"Лёха ×5, Аня ×3, Маша ×2",
		"пропорционально",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("render missing %q in:\n%s", want, got)
		}
	}

	// итог = сумма цен всех позиций (1200 + 500 + 170 = 1870)
	if !strings.Contains(got, "1 870") {
		t.Fatalf("render missing total 1 870 in:\n%s", got)
	}
}

// Блок позиций уходит в Telegram с ParseMode=HTML, а название позиции и
// DisplayName задаёт пользователь. Неэкранированный "<" — это и 400 от Telegram
// (экран операции перестаёт открываться у всей комнаты), и инъекция ссылки в
// общее сообщение.
func TestRenderOperationItems_EscapesUserInput(t *testing.T) {
	members := []api.User{{ID: 1, DisplayName: `<a href="evil">Аня</a>`}}
	room := &api.Room{Members: &members, Currency: "RUB"}
	op := api.Operation{Items: []api.OperationItem{{
		Name:   "Пицца <b>x</b> & a < b",
		Price:  100,
		Qty:    1,
		Kind:   api.ItemKindItem,
		Shares: []api.ItemShare{{UserId: 1, Weight: 1}},
	}}}

	got := renderOperationItems(op, room)
	for _, raw := range []string{"<b>", `<a href="evil">`, "a < b"} {
		if strings.Contains(got, raw) {
			t.Fatalf("сырой HTML %q попал в сообщение: %s", raw, got)
		}
	}
	if !strings.Contains(got, "&lt;b&gt;") {
		t.Fatalf("название позиции не экранировано: %s", got)
	}
	if !strings.Contains(got, "&lt;a href=&#34;evil&#34;&gt;") {
		t.Fatalf("имя участника не экранировано: %s", got)
	}
}

func TestUserLink_EscapesDisplayName(t *testing.T) {
	tgID := 42
	// ID (номер Splitty) и telegram id намеренно различаются: ссылка обязана
	// собираться по telegram id
	got := userLink(&api.User{ID: 1_000_000_000_007, TelegramID: &tgID, DisplayName: `<b>bad</b>`})
	if strings.Contains(got, "<b>") {
		t.Fatalf("DisplayName не экранирован: %s", got)
	}
	if !strings.Contains(got, `<a href="tg://user?id=42">`) {
		t.Fatalf("ссылка сломана: %s", got)
	}
}

// Чек в боте печатается ТОЧНЫМИ величинами.
//
// Позиции печатались округлёнными: 20,80 показывалось как 21, две такие давали
// итог 42 вместо сохранённых 41,60, а фикс 10,40 — как 10. Человек видел в боте
// одни числа, в приложении другие, и расхождение выглядело ошибкой расчёта.
func TestRenderOperationItemsPrintsExactMoney(t *testing.T) {
	// Узкий неразрывный пробел — тот же, что ставит форматтер сумм.
	const nbsp = "\u202f"
	minor := func(v int64) *int64 { return &v }

	room := testRoom()
	room.Currency = "USD"
	op := api.Operation{
		Status: active,
		Items: []api.OperationItem{
			{
				Name: "Кофе", Kind: api.ItemKindItem, Qty: 1,
				Price: 21, PriceMinor: minor(2080),
				Shares: []api.ItemShare{
					{UserId: 1, Amount: intPtr(10), AmountMinor: minor(1040)},
					{UserId: 2, Weight: 1},
				},
			},
			{
				Name: "Десерт", Kind: api.ItemKindItem, Qty: 1,
				Price: 21, PriceMinor: minor(2080),
				Shares: []api.ItemShare{{UserId: 2, Weight: 1}},
			},
		},
	}

	text := renderOperationItems(op, room)
	for _, want := range []string{
		"20,80" + nbsp + "$", // цена позиции
		"41,60" + nbsp + "$", // итог: сумма точных, а не округлённых
		"10,40" + nbsp + "$", // фикс-доля участника
	} {
		if !strings.Contains(text, want) {
			t.Errorf("в чеке нет %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "42"+nbsp+"$") {
		t.Errorf("итог округлился до 42 — сложены округлённые цены:\n%s", text)
	}
}
