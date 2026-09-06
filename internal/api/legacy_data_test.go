package api

import "testing"

// Контрпримеры на ФОРМАХ ДАННЫХ, которые реально лежат в базе.
//
// Ревью 06.09.2026 нашло, что все прежние тесты денег строились на фикстурах,
// уже согласованных вручную (7/7/6, 694/693/693). Формы, которую производит
// старый бот, — `float64(total)/n`, то есть 33.333… — не было ни в одном
// тесте, и поштучный перевод долей в минорные проходил незамеченным.
//
// Любой новый тест денег обязан начинаться отсюда.

// botEqualSplit — операция в том виде, в каком её хранит бот: доля каждого
// получателя это float64(total)/n (internal/bot/operation_screen.go).
func botEqualSplit(total, n int) Operation {
	op := Operation{Sum: total, SplitType: SplitTypeEqually}
	share := float64(total) / float64(n)
	for i := 0; i < n; i++ {
		op.RecipientsWithSum = append(op.RecipientsWithSum, RecipientWithSum{Sum: share})
	}
	return op
}

func sumShares(op *Operation) int64 {
	var total int64
	for _, r := range op.RecipientsWithSum {
		if r.SumMinor == nil {
			return -1
		}
		total += *r.SumMinor
	}
	return total
}

// 100 на троих от бота: доли обязаны сойтись с итогом на ЛЮБОЙ шкале.
func TestBotEqualSplitSharesSumToTotal(t *testing.T) {
	for _, exp := range []int{0, 2} {
		op := botEqualSplit(100, 3)
		FillMoney(&op, exp)

		if got := sumShares(&op); got != *op.SumMinor {
			t.Errorf("шкала %d: сумма долей %d, итог %d — деньги разошлись", exp, got, *op.SumMinor)
		}
	}
}

// И после включения копеек тоже: раньше здесь получалось 9900 против 10000,
// то есть терялся целый рубль.
func TestBotEqualSplitSurvivesScaleUp(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, 0)
	RescaleOperation(&op, 0, 2)

	if *op.SumMinor != 10000 {
		t.Fatalf("итог = %d, want 10000", *op.SumMinor)
	}
	if got := sumShares(&op); got != 10000 {
		t.Errorf("сумма долей = %d, want 10000 — при включении копеек потерялся рубль", got)
	}
}

// Круг «включили копейки — выключили» на данных бота не должен двигать деньги.
func TestBotEqualSplitSurvivesRoundTrip(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, 0)
	RescaleOperation(&op, 0, 2)
	RescaleOperation(&op, 2, 0)

	if op.Sum != 100 {
		t.Errorf("сумма после круга = %d, want 100", op.Sum)
	}
	if got := sumShares(&op); got != 100 {
		t.Errorf("сумма долей = %d, want 100", got)
	}
}

// Правка суммы ботом на данных бота: минорное 3333 не кратно шкале, и прежнее
// правило считало его «настоящей дробью», выбрасывая правку человека.
func TestBotEditOfEqualSplitSurvivesWrite(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, 2) // прочитали комнату с копейками

	op.Sum = 120 // человек поправил сумму в боте
	ReconcileMoney(&op, 2)

	if op.Sum != 120 {
		t.Errorf("сумма = %d, want 120 — правка потерялась", op.Sum)
	}
	if *op.SumMinor != 12000 {
		t.Errorf("sumMinor = %d, want 12000", *op.SumMinor)
	}
	if got := sumShares(&op); got != 12000 {
		t.Errorf("сумма долей = %d, want 12000 — доли не пересобрали от новой суммы", got)
	}
}

// Правка доли ботом в расходе с точными суммами: там хранимые значения — это
// веса, и правка обязана дойти.
func TestBotEditOfExactShareSurvivesWrite(t *testing.T) {
	a, b := int64(5000), int64(5000)
	op := Operation{
		Sum: 100, SumMinor: ptr64(10000), SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{Sum: 50, SumMinor: &a}, {Sum: 50, SumMinor: &b},
		},
	}
	op.RecipientsWithSum[0].Sum = 60
	op.RecipientsWithSum[1].Sum = 40
	ReconcileMoney(&op, 2)

	if got := *op.RecipientsWithSum[0].SumMinor; got != 6000 {
		t.Errorf("доля = %d, want 6000 — правка доли потерялась", got)
	}
	if got := sumShares(&op); got != 10000 {
		t.Errorf("сумма долей = %d, want 10000", got)
	}
}

func ptr64(v int64) *int64 { return &v }

// Погашение: единственный получатель забирает весь итог.
func TestRepaymentShareEqualsTotal(t *testing.T) {
	op := Operation{Sum: 50, IsDebtRepayment: true,
		RecipientsWithSum: []RecipientWithSum{{Sum: 50}}}
	FillMoney(&op, 2)

	if got := sumShares(&op); got != *op.SumMinor {
		t.Errorf("сумма долей = %d, итог %d", got, *op.SumMinor)
	}
}

// Легаси-операция без splitType читается как равное деление — так же, как это
// делает проекция recipientShare.
func TestLegacyOperationWithoutSplitTypeIsEqual(t *testing.T) {
	op := Operation{Sum: 100, RecipientsWithSum: []RecipientWithSum{{}, {}, {}}}
	FillMoney(&op, 0)

	want := []int64{34, 33, 33}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, want[i])
		}
	}
}

// Контрпример Codex: итог 1,50 из трёх позиций по 0,50, первые две у A,
// третья у B. Позиции объявлены источником правды, значит плоские доли обязаны
// выводиться ИЗ них — иначе после выключения копеек по позициям получается
// A=2, B=0, а по плоским долям A=1, B=1, и долг зависит от пути расчёта.
func TestItemizedRescaleAgreesWithFlatShares(t *testing.T) {
	a, b := User{ID: 1}, User{ID: 2}
	total := int64(150)
	p1, p2, p3 := int64(50), int64(50), int64(50)
	aFlat, bFlat := int64(100), int64(50)

	op := Operation{
		SumMinor:  &total,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{User: a, SumMinor: &aFlat},
			{User: b, SumMinor: &bFlat},
		},
		Items: []OperationItem{
			{PriceMinor: &p1, Shares: []ItemShare{{UserId: 1, Weight: 1}}},
			{PriceMinor: &p2, Shares: []ItemShare{{UserId: 1, Weight: 1}}},
			{PriceMinor: &p3, Shares: []ItemShare{{UserId: 2, Weight: 1}}},
		},
	}
	RescaleOperation(&op, 2, 0)

	byItems, itemsTotal, err := DeriveSharesMinor(op.Items, 0)
	if err != nil {
		t.Fatalf("DeriveSharesMinor: %v", err)
	}
	if itemsTotal != *op.SumMinor {
		t.Errorf("сумма позиций = %d, итог расхода = %d", itemsTotal, *op.SumMinor)
	}
	for _, r := range op.RecipientsWithSum {
		if byItems[r.User.ID] != *r.SumMinor {
			t.Errorf("участник %d: по позициям %d, по плоским долям %d — два разных долга",
				r.User.ID, byItems[r.User.ID], *r.SumMinor)
		}
	}
	if got := sumShares(&op); got != *op.SumMinor {
		t.Errorf("сумма долей = %d, итог = %d", got, *op.SumMinor)
	}
}

// Тот же расход вверх: включение копеек ничего не теряет и пути по-прежнему
// согласованы.
func TestItemizedRescaleUpAgreesWithFlatShares(t *testing.T) {
	a, b := User{ID: 1}, User{ID: 2}
	total := int64(150)
	p1, p2 := int64(100), int64(50)
	aFlat, bFlat := int64(100), int64(50)

	op := Operation{
		SumMinor:  &total,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{User: a, SumMinor: &aFlat},
			{User: b, SumMinor: &bFlat},
		},
		Items: []OperationItem{
			{PriceMinor: &p1, Shares: []ItemShare{{UserId: 1, Weight: 1}}},
			{PriceMinor: &p2, Shares: []ItemShare{{UserId: 2, Weight: 1}}},
		},
	}
	RescaleOperation(&op, 0, 2)

	byItems, itemsTotal, err := DeriveSharesMinor(op.Items, 2)
	if err != nil {
		t.Fatalf("DeriveSharesMinor: %v", err)
	}
	if itemsTotal != 15000 || *op.SumMinor != 15000 {
		t.Errorf("итог позиций %d, итог расхода %d, want 15000", itemsTotal, *op.SumMinor)
	}
	for _, r := range op.RecipientsWithSum {
		if byItems[r.User.ID] != *r.SumMinor {
			t.Errorf("участник %d: по позициям %d, по плоским долям %d",
				r.User.ID, byItems[r.User.ID], *r.SumMinor)
		}
	}
}
