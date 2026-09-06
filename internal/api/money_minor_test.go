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
	a, b, c := int64(694), int64(693), int64(693)
	op := Operation{
		Sum:       0,
		SumMinor:  &sum,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{SumMinor: &a}, {SumMinor: &b}, {SumMinor: &c},
		},
		Items: []OperationItem{{PriceMinor: &price}},
	}
	FillMoney(&op, 2)

	if op.Sum != 21 {
		t.Errorf("проекция суммы = %d, want 21", op.Sum)
	}
	if op.Items[0].Price != 21 {
		t.Errorf("проекция цены = %d, want 21", op.Items[0].Price)
	}
	// Старую дробную долю FillMoney НЕ переписывает: по ней сервер узнаёт
	// легаси-комнату с несходящимися долями и отказывается считать долги.
	// Проекция для старых сборок собирается на границе API (recipientShares).
	if op.RecipientsWithSum[0].Sum != 0 {
		t.Errorf("старую долю переписали: %v", op.RecipientsWithSum[0].Sum)
	}
	var total int64
	for _, r := range op.RecipientsWithSum {
		total += *r.SumMinor
	}
	if total != 2080 {
		t.Errorf("сумма долей = %d, want 2080", total)
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

// Бот правит СТАРОЕ поле и про минорные ничего не знает. Если запись возьмёт
// устаревшее минорное значение, правка человека потеряется молча — он увидит
// прежнюю сумму и не поймёт, почему.
func TestReconcileMoneyKeepsLegacyEdit(t *testing.T) {
	stale := int64(2000)
	op := Operation{Sum: 30, SumMinor: &stale}
	ReconcileMoney(&op, 2)

	if op.Sum != 30 {
		t.Errorf("сумма = %d, want 30 — правка потерялась", op.Sum)
	}
	if op.SumMinor == nil || *op.SumMinor != 3000 {
		t.Errorf("sumMinor = %v, want 3000 — минорное не пересобрали из правки", op.SumMinor)
	}
}

// То же для долей: бот меняет долю получателя, минорное поле остаётся прежним.
func TestReconcileMoneyKeepsLegacyShareEdit(t *testing.T) {
	stale := int64(1000)
	op := Operation{
		Sum:               30,
		RecipientsWithSum: []RecipientWithSum{{Sum: 30, SumMinor: &stale}},
	}
	ReconcileMoney(&op, 2)

	if got := op.RecipientsWithSum[0].SumMinor; got == nil || *got != 3000 {
		t.Errorf("доля = %v, want 3000", got)
	}
}

// И для цен позиций с фиксированными долями.
func TestReconcileMoneyKeepsLegacyItemEdit(t *testing.T) {
	stalePrice, staleAmount := int64(2000), int64(500)
	amount := 7
	op := Operation{
		Sum: 30,
		Items: []OperationItem{{
			Price:      30,
			PriceMinor: &stalePrice,
			Shares:     []ItemShare{{UserId: 1, Amount: &amount, AmountMinor: &staleAmount}},
		}},
	}
	ReconcileMoney(&op, 2)

	if got := op.Items[0].PriceMinor; got == nil || *got != 3000 {
		t.Errorf("цена = %v, want 3000", got)
	}
	if got := op.Items[0].Shares[0].AmountMinor; got == nil || *got != 700 {
		t.Errorf("фикс-доля = %v, want 700", got)
	}
}

// Согласованную пару трогать не за что: REST пишет оба поля сам.
func TestReconcileMoneyLeavesConsistentPairAlone(t *testing.T) {
	minor := int64(2080)
	op := Operation{Sum: 21, SumMinor: &minor}
	ReconcileMoney(&op, 2)

	if *op.SumMinor != 2080 {
		t.Errorf("sumMinor = %d, want 2080 — дробную сумму испортили", *op.SumMinor)
	}
	if op.Sum != 21 {
		t.Errorf("sum = %d, want 21", op.Sum)
	}
}

// Расхождение пары не может означать ничего, кроме правки старого поля: REST
// пишет оба поля согласованными по построению. Значит и дробное минорное
// уступает — иначе правка выбрасывается молча.
//
// ⚠️ Отказывать старой сборке в правке ДРОБНОЙ операции — работа входного
// слоя (Задача 5 для REST, Задача 14 для бота), а не этой функции. Здесь
// последняя линия, и её дело — не потерять то, что человек написал.
func TestReconcileMoneyLegacyWinsOverFractionalMinor(t *testing.T) {
	minor := int64(2080)
	op := Operation{Sum: 30, SumMinor: &minor}
	ReconcileMoney(&op, 2)

	if *op.SumMinor != 3000 {
		t.Errorf("sumMinor = %d, want 3000 — правка человека потерялась", *op.SumMinor)
	}
	if op.Sum != 30 {
		t.Errorf("sum = %d, want 30", op.Sum)
	}
}

// Согласованную дробную пару никто не трогает.
func TestReconcileMoneyKeepsConsistentFractionalPair(t *testing.T) {
	minor := int64(2080)
	op := Operation{Sum: 21, SumMinor: &minor}
	ReconcileMoney(&op, 2)

	if *op.SumMinor != 2080 {
		t.Errorf("sumMinor = %d, want 2080 — дробь потеряли на ровном месте", *op.SumMinor)
	}
}

// В комнате нулевой шкалы минорная единица равна единице валюты, и никакой
// пересборки не требуется.
func TestReconcileMoneyAtExponentZero(t *testing.T) {
	stale := int64(100)
	op := Operation{Sum: 250, SumMinor: &stale}
	ReconcileMoney(&op, 0)

	if op.SumMinor == nil || *op.SumMinor != 250 {
		t.Errorf("sumMinor = %v, want 250", op.SumMinor)
	}
}
