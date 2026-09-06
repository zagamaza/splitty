package api

import "testing"

// Контрпримеры на ФОРМАХ ДАННЫХ, которые реально лежат в базе.
//
// Ревью нашло, что прежние тесты денег строились на фикстурах, уже
// согласованных вручную (7/7/6, 694/693/693). Формы, которую производит старый
// бот — float64(total)/n, то есть 33.333… — не было ни в одном тесте, и
// поштучный перевод долей в копейки проходил незамеченным.
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

func ptr64(v int64) *int64 { return &v }

// 100 на троих от бота: доли обязаны сойтись с итогом и с копейками, и без них.
func TestBotEqualSplitSharesSumToTotal(t *testing.T) {
	for _, fractional := range []bool{false, true} {
		op := botEqualSplit(100, 3)
		FillMoney(&op, fractional)

		if got := sumShares(&op); got != *op.SumMinor {
			t.Errorf("копейки=%v: сумма долей %d, итог %d — деньги разошлись",
				fractional, got, *op.SumMinor)
		}
	}
}

// Без копеек доли обязаны быть кратны единице валюты: иначе человеку показывают
// долю, которую он не может заплатить, а столбик на экране не сходится.
func TestSharesAreWholeUnitsWithoutFraction(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, false)

	want := []int64{3400, 3300, 3300}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, want[i])
		}
	}
	if got := sumShares(&op); got != 10000 {
		t.Errorf("сумма долей = %d, want 10000", got)
	}
}

// С копейками остаток раздаётся по копейке, а не по рублю.
func TestSharesSplitToKopeckWithFraction(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, true)

	want := []int64{3334, 3333, 3333}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, want[i])
		}
	}
}

// Переключение признака НЕ трогает записанные суммы: меняется только то, с
// какой точностью выводятся доли. Итог остаётся тем же до копейки.
func TestTogglingFractionKeepsTotal(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, true)
	total := *op.SumMinor

	FillMoney(&op, false)
	if *op.SumMinor != total {
		t.Errorf("итог изменился от переключения признака: %d, было %d", *op.SumMinor, total)
	}
	if got := sumShares(&op); got != total {
		t.Errorf("сумма долей = %d, итог %d", got, total)
	}
}

// Старая дробная сумма в тусе, где копейки выключили, остаётся дробной:
// округлять её значило бы соврать, а доли перестали бы сходиться с итогом.
func TestFractionalAmountSurvivesFractionOff(t *testing.T) {
	sum := int64(2080)
	op := Operation{SumMinor: &sum, RecipientsWithSum: []RecipientWithSum{{SumMinor: ptr64(2080)}}}
	FillMoney(&op, false)

	if *op.SumMinor != 2080 {
		t.Errorf("сумма = %d, want 2080 — дробь округлили", *op.SumMinor)
	}
	if got := sumShares(&op); got != 2080 {
		t.Errorf("сумма долей = %d, want 2080", got)
	}
}

// Правка суммы ботом на данных бота: минорное 3333 не кратно ста, и прежнее
// правило считало его «настоящей дробью», выбрасывая правку человека.
func TestBotEditOfEqualSplitSurvivesWrite(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, true)

	op.Sum = 120 // человек поправил сумму в боте
	ReconcileMoney(&op, true)

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

// Правка доли ботом в расходе с точными суммами: там хранимые значения — веса,
// и правка обязана дойти.
func TestBotEditOfExactShareSurvivesWrite(t *testing.T) {
	op := Operation{
		Sum: 100, SumMinor: ptr64(10000), SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{Sum: 50, SumMinor: ptr64(5000)}, {Sum: 50, SumMinor: ptr64(5000)},
		},
	}
	op.RecipientsWithSum[0].Sum = 60
	op.RecipientsWithSum[1].Sum = 40
	ReconcileMoney(&op, true)

	if got := *op.RecipientsWithSum[0].SumMinor; got != 6000 {
		t.Errorf("доля = %d, want 6000 — правка доли потерялась", got)
	}
	if got := sumShares(&op); got != 10000 {
		t.Errorf("сумма долей = %d, want 10000", got)
	}
}

// Погашение: единственный получатель забирает весь итог.
func TestRepaymentShareEqualsTotal(t *testing.T) {
	op := Operation{Sum: 50, IsDebtRepayment: true,
		RecipientsWithSum: []RecipientWithSum{{Sum: 50}}}
	FillMoney(&op, true)

	if got := sumShares(&op); got != *op.SumMinor {
		t.Errorf("сумма долей = %d, итог %d", got, *op.SumMinor)
	}
}

// Легаси-операция без splitType читается как равное деление — так же, как это
// делает проекция recipientShare.
func TestLegacyOperationWithoutSplitTypeIsEqual(t *testing.T) {
	op := Operation{Sum: 100, RecipientsWithSum: []RecipientWithSum{{}, {}, {}}}
	FillMoney(&op, false)

	want := []int64{3400, 3300, 3300}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, want[i])
		}
	}
}

// Переключение признака НЕ двигает уже записанные доли. Это обязательства
// конкретных людей друг перед другом: расход 100 на троих, сохранённый как
// 33,34 + 33,33 + 33,33, обязан остаться таким и после выключения копеек.
// Интерфейс обещает ровно это — «уже записанное не изменится».
func TestTogglingFractionKeepsRecordedShares(t *testing.T) {
	op := Operation{
		Sum: 100, SumMinor: ptr64(10000), SplitType: SplitTypeEqually,
		RecipientsWithSum: []RecipientWithSum{
			{SumMinor: ptr64(3334)}, {SumMinor: ptr64(3333)}, {SumMinor: ptr64(3333)},
		},
	}
	want := []int64{3334, 3333, 3333}

	for _, fractional := range []bool{false, true, false} {
		FillMoney(&op, fractional)
		for i, r := range op.RecipientsWithSum {
			if *r.SumMinor != want[i] {
				t.Fatalf("копейки=%v: доля[%d] = %d, want %d — долг человека поехал от тумблера",
					fractional, i, *r.SumMinor, want[i])
			}
		}
	}
}

// А несогласованный вектор — легаси-данные бота — по-прежнему выводится заново:
// иначе доли не сойдутся с итогом.
func TestInconsistentSharesAreStillDerived(t *testing.T) {
	op := botEqualSplit(100, 3)
	FillMoney(&op, false)

	if got := sumShares(&op); got != 10000 {
		t.Errorf("сумма долей = %d, want 10000", got)
	}
	if *op.RecipientsWithSum[0].SumMinor != 3400 {
		t.Errorf("доля[0] = %d, want 3400 — вектор не вывели", *op.RecipientsWithSum[0].SumMinor)
	}
}

// Очень старая операция эпохи master-2021: получатели лежат в легаси-поле
// recipients, а recipients_with_sum нет вовсе. Доли синтезируются при
// нормализации — и обязаны получить копейки там же, иначе в ответе API у
// операции сумма в копейках есть, а у каждой доли пусто.
func TestLegacyRecipientsGetMinorShares(t *testing.T) {
	op := NormalizedOperation(Operation{
		Sum:        100,
		Recipients: &[]User{{ID: 1}, {ID: 2}, {ID: 3}},
	})

	if len(op.RecipientsWithSum) != 3 {
		t.Fatalf("получателей %d, want 3", len(op.RecipientsWithSum))
	}
	var sum int64
	for i, r := range op.RecipientsWithSum {
		if r.SumMinor == nil {
			t.Fatalf("доля[%d] без минорного значения", i)
		}
		sum += *r.SumMinor
	}
	if sum != 10000 {
		t.Errorf("сумма долей = %d, want 10000", sum)
	}
	want := []int64{3400, 3300, 3300}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, want[i])
		}
	}
}
