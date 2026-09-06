package api

import "testing"

func TestToFromMinorRoundTrip(t *testing.T) {
	for _, exp := range []int{0, 2} {
		for _, units := range []int{0, 1, -1, 7, -7, 1200, -1200, 1_000_000} {
			if got := FromMinor(ToMinor(units, exp), exp); got != units {
				t.Errorf("exp=%d units=%d: обратный перевод дал %d", exp, units, got)
			}
		}
	}
}

// Округление одно на весь продукт: к ближайшему, половина — от нуля.
// Симметрия важна: возврат и трата одного размера обязаны округляться
// одинаково, иначе долг «съедает» копейку при каждом развороте.
func TestFromMinorRounding(t *testing.T) {
	for _, tc := range []struct {
		minor int64
		want  int
	}{
		{0, 0}, {49, 0}, {50, 1}, {51, 1}, {149, 1}, {150, 2}, {2080, 21},
		{-49, 0}, {-50, -1}, {-51, -1}, {-150, -2}, {-2080, -21},
	} {
		if got := FromMinor(tc.minor, 2); got != tc.want {
			t.Errorf("FromMinor(%d, 2) = %d, want %d", tc.minor, got, tc.want)
		}
	}
}

// «20.8» во float64 после умножения на сто даёт 2079.9999…: усечение отняло бы
// копейку у каждой такой доли.
func TestFloatToMinorDoesNotTruncate(t *testing.T) {
	for _, tc := range []struct {
		sum  float64
		want int64
	}{
		{20.8, 2080}, {0.1, 10}, {0.2, 20}, {0.3, 30}, {6.93, 693}, {33.33, 3333}, {-6.93, -693},
	} {
		if got := FloatToMinor(tc.sum, 2); got != tc.want {
			t.Errorf("FloatToMinor(%v, 2) = %d, want %d", tc.sum, got, tc.want)
		}
	}
}

// Немигрированный документ отвечает так же, как мигрированный: вызывающему
// не нужно знать, какой ему достался.
func TestSumMinorAtFallsBackToLegacyField(t *testing.T) {
	if got := (Operation{Sum: 12}).SumMinorAt(2); got != 1200 {
		t.Errorf("операция без sum_minor: got %d, want 1200", got)
	}
	m := int64(2080)
	if got := (Operation{Sum: 21, SumMinor: &m}).SumMinorAt(2); got != 2080 {
		t.Errorf("операция с sum_minor: got %d, want 2080", got)
	}
	if got := (RecipientWithSum{Sum: 6.93}).SumMinorAt(2); got != 693 {
		t.Errorf("доля без sum_minor: got %d, want 693", got)
	}
}

// У ItemShare.Amount ноль ОСМЫСЛЕН: человек не платит за позицию. Отличить его
// от «фиксированной доли нет» больше нечем, отсюда второе возвращаемое значение.
func TestAmountMinorAtDistinguishesZeroFromAbsent(t *testing.T) {
	if _, ok := (ItemShare{}).AmountMinorAt(2); ok {
		t.Error("доли нет, а функция сказала, что есть")
	}
	zero := 0
	got, ok := (ItemShare{Amount: &zero}).AmountMinorAt(2)
	if !ok || got != 0 {
		t.Errorf("осознанный ноль: got (%d, %v), want (0, true)", got, ok)
	}
	m := int64(150)
	got, ok = (ItemShare{AmountMinor: &m}).AmountMinorAt(2)
	if !ok || got != 150 {
		t.Errorf("минорная доля: got (%d, %v), want (150, true)", got, ok)
	}
}

// Документ без минорных полей читается и пишется обратно без потерь.
func TestFillMoneyOnLegacyDocument(t *testing.T) {
	amount := 3
	op := Operation{
		Sum: 20,
		RecipientsWithSum: []RecipientWithSum{
			{Sum: 6.67}, {Sum: 6.67}, {Sum: 6.66},
		},
		Items: []OperationItem{{
			Price:  20,
			Shares: []ItemShare{{UserId: 1, Weight: 1, Amount: &amount}, {UserId: 2, Weight: 1}},
		}},
	}
	FillMoney(&op, 2)

	if op.SumMinor == nil || *op.SumMinor != 2000 {
		t.Fatalf("sumMinor = %v, want 2000", op.SumMinor)
	}
	if op.Sum != 20 {
		t.Errorf("старое поле изменилось: %d", op.Sum)
	}
	want := []int64{667, 667, 666}
	for i, r := range op.RecipientsWithSum {
		if r.SumMinor == nil || *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %v, want %d", i, r.SumMinor, want[i])
		}
	}
	if op.Items[0].PriceMinor == nil || *op.Items[0].PriceMinor != 2000 {
		t.Errorf("priceMinor = %v, want 2000", op.Items[0].PriceMinor)
	}
	if got := op.Items[0].Shares[0].AmountMinor; got == nil || *got != 300 {
		t.Errorf("amountMinor = %v, want 300", got)
	}
	if op.Items[0].Shares[1].AmountMinor != nil {
		t.Error("у доли без Amount появилось AmountMinor — отсутствие превратили в ноль")
	}
}

// Мигрированный документ: минорные поля источник правды, старые собираются
// из них проекцией, а не остаются как были.
func TestFillMoneyProjectsLegacyFromMinor(t *testing.T) {
	sum := int64(2080)
	price := int64(2080)
	share := int64(693)
	op := Operation{
		Sum:               0,
		SumMinor:          &sum,
		RecipientsWithSum: []RecipientWithSum{{SumMinor: &share}},
		Items:             []OperationItem{{PriceMinor: &price}},
	}
	FillMoney(&op, 2)

	if op.Sum != 21 {
		t.Errorf("проекция суммы = %d, want 21", op.Sum)
	}
	if op.Items[0].Price != 21 {
		t.Errorf("проекция цены = %d, want 21", op.Items[0].Price)
	}
	if op.RecipientsWithSum[0].Sum != 6.93 {
		t.Errorf("проекция доли = %v, want 6.93", op.RecipientsWithSum[0].Sum)
	}
}

// В комнате нулевой шкалы минорная единица равна единице валюты: перевод не
// должен ничего умножать.
func TestFillRoomMoneyAtExponentZero(t *testing.T) {
	room := &Room{
		Currency:   "RUB",
		Operations: &[]Operation{{Sum: 1200}},
	}
	FillRoomMoney(room)
	op := (*room.Operations)[0]
	if op.SumMinor == nil || *op.SumMinor != 1200 {
		t.Errorf("sumMinor = %v, want 1200 (шкала 0)", op.SumMinor)
	}
	if op.Sum != 1200 {
		t.Errorf("sum = %d, want 1200", op.Sum)
	}
}
