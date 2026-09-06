package api

import (
	"errors"
	"testing"
)

func TestDistributeSumsExactly(t *testing.T) {
	for _, tc := range []struct {
		total   int64
		weights []int64
	}{
		{2, []int64{50, 50, 50}},
		{21, []int64{694, 693, 693}},
		{100, []int64{1}},
		{7, []int64{1, 1, 1}},
		{0, []int64{5, 5}},
		{-21, []int64{694, 693, 693}},
		{13, []int64{0, 100, 0}},
	} {
		got, _ := Distribute(tc.total, tc.weights)
		var sum int64
		for _, v := range got {
			sum += v
		}
		if sum != tc.total {
			t.Errorf("Distribute(%d, %v) = %v, сумма %d != %d", tc.total, tc.weights, got, sum, tc.total)
		}
	}
}

// Ровно тот случай, ради которого доли раздаются заново, а не округляются
// поодиночке: 1.50 на троих это 50+50+50 копеек, а поштучное округление дало бы
// 1+1+1 = 3 при итоге 2.
func TestDistributeDoesNotInventMoney(t *testing.T) {
	got, _ := Distribute(2, []int64{50, 50, 50})
	var sum int64
	for _, v := range got {
		sum += v
	}
	if sum != 2 {
		t.Fatalf("доли %v дают %d, want 2", got, sum)
	}
}

// Нулевые веса не получают ничего: человек, который ни за что не платил, не
// должен получить копейку от смены шкалы.
func TestDistributeSkipsZeroWeights(t *testing.T) {
	got, _ := Distribute(10, []int64{0, 5, 5})
	if got[0] != 0 {
		t.Errorf("нулевой вес получил %d", got[0])
	}
}

// Делить не по чему — деньги всё равно не исчезают.
func TestDistributeAllZeroWeights(t *testing.T) {
	got, _ := Distribute(10, []int64{0, 0})
	if got[0]+got[1] != 10 {
		t.Errorf("Distribute(10, [0 0]) = %v, деньги потерялись", got)
	}
}

// Включение копеек точное: суммы на вид те же, ничего не потеряно.
//
// Фикстура СОГЛАСОВАННАЯ: позиции описывают тех же людей, что и плоские доли,
// и их сумма равна итогу. Несогласованная теперь отвергается — см.
// TestRescaleRefusesIncoherentItemized.
func TestRescaleUpIsExact(t *testing.T) {
	amount := 3
	op := Operation{
		Sum:       20,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{User: User{ID: 1}, Sum: 3},
			{User: User{ID: 2}, Sum: 17},
		},
		Items: []OperationItem{{
			Price:  20,
			Shares: []ItemShare{{UserId: 1, Weight: 1, Amount: &amount}, {UserId: 2, Weight: 1}},
		}},
	}
	if err := RescaleOperation(&op, 0, 2); err != nil {
		t.Fatalf("RescaleOperation: %v", err)
	}

	if *op.SumMinor != 2000 {
		t.Errorf("sumMinor = %d, want 2000", *op.SumMinor)
	}
	if op.Sum != 20 {
		t.Errorf("проекция суммы = %d, want 20 — на вид ничего не изменилось", op.Sum)
	}
	var sum int64
	for _, r := range op.RecipientsWithSum {
		sum += *r.SumMinor
	}
	if sum != 2000 {
		t.Errorf("сумма долей = %d, want 2000", sum)
	}
	if *op.Items[0].PriceMinor != 2000 {
		t.Errorf("цена позиции = %d, want 2000", *op.Items[0].PriceMinor)
	}
	if got := op.Items[0].Shares[0].AmountMinor; got == nil || *got != 300 {
		t.Errorf("фикс-доля = %v, want 300", got)
	}
	if op.Items[0].Shares[1].AmountMinor != nil {
		t.Error("весовой участник получил фиксированную долю")
	}
}

// Испорченный itemized-документ: сумма позиций не сходится с итогом расхода.
// Пересчитать его, не соврав, нельзя — и свалиться на плоские доли тоже:
// получилась бы комната, где два пути расчёта дают разные деньги.
func TestRescaleRefusesIncoherentItemized(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   Operation
	}{
		{
			name: "сумма позиций не равна итогу",
			op: Operation{
				Sum:       20,
				SplitType: SplitTypeByExactAmount,
				RecipientsWithSum: []RecipientWithSum{
					{User: User{ID: 1}, Sum: 10}, {User: User{ID: 2}, Sum: 10},
				},
				Items: []OperationItem{{
					Price:  15,
					Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
				}},
			},
		},
		{
			name: "в позициях не те люди",
			op: Operation{
				Sum:       20,
				SplitType: SplitTypeByExactAmount,
				RecipientsWithSum: []RecipientWithSum{
					{User: User{ID: 1}, Sum: 10}, {User: User{ID: 3}, Sum: 10},
				},
				Items: []OperationItem{{
					Price:  20,
					Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
				}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			if err := RescaleOperation(&op, 0, 2); !errors.Is(err, ErrRescaleImpossible) {
				t.Fatalf("RescaleOperation: %v, want ErrRescaleImpossible", err)
			}
		})
	}
}

// Отказ обязан быть на уровне КОМНАТЫ: пересчёт либо проходит целиком, либо не
// проходит вовсе. Половина сумм в одной шкале, половина в другой — это деньги,
// которых нет.
func TestRescaleRoomRefusesWholeRoomOnOneBadOperation(t *testing.T) {
	good := Operation{Sum: 100, RecipientsWithSum: []RecipientWithSum{{User: User{ID: 1}, Sum: 100}}}
	bad := Operation{
		Sum:       20,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{User: User{ID: 1}, Sum: 10}, {User: User{ID: 2}, Sum: 10},
		},
		Items: []OperationItem{{Price: 15, Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}}}},
	}
	room := &Room{Currency: "USD", Operations: &[]Operation{good, bad}}
	zero := 0
	room.DisplayExponent = &zero

	if err := RescaleRoom(room, 2); !errors.Is(err, ErrRescaleImpossible) {
		t.Fatalf("RescaleRoom: %v, want ErrRescaleImpossible", err)
	}
	if RoomExponent(room) != 0 {
		t.Errorf("шкала = %d, want 0 — комнату не должны были тронуть", RoomExponent(room))
	}
	if room.ScaleVersion != 0 {
		t.Errorf("версия шкалы выросла на отказе: %d", room.ScaleVersion)
	}
}

// Выключение копеек: итог округляется, доли раздаются заново — и продолжают
// сходиться с итогом.
func TestRescaleDownKeepsSharesConsistent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sumMinor  int64
		shares    []int64
		wantTotal int64
	}{
		{"20.80 на троих", 2080, []int64{694, 693, 693}, 21},
		{"1.50 на троих", 150, []int64{50, 50, 50}, 2},
		{"ровно 3.00", 300, []int64{100, 100, 100}, 3},
		{"2.50 пополам", 250, []int64{125, 125}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := Operation{SumMinor: &tc.sumMinor}
			for _, sh := range tc.shares {
				v := sh
				op.RecipientsWithSum = append(op.RecipientsWithSum, RecipientWithSum{SumMinor: &v})
			}
			RescaleOperation(&op, 2, 0)

			if *op.SumMinor != tc.wantTotal {
				t.Errorf("итог = %d, want %d", *op.SumMinor, tc.wantTotal)
			}
			var sum int64
			for _, r := range op.RecipientsWithSum {
				sum += *r.SumMinor
			}
			if sum != tc.wantTotal {
				t.Errorf("сумма долей = %d, а итог %d — деньги разошлись", sum, tc.wantTotal)
			}
		})
	}
}

// Цены позиций после выключения копеек тоже сходятся с итогом расхода.
func TestRescaleDownKeepsItemPricesConsistent(t *testing.T) {
	total := int64(1000)
	p1, p2, p3 := int64(333), int64(333), int64(334)
	op := Operation{
		SumMinor: &total,
		Items: []OperationItem{
			{PriceMinor: &p1}, {PriceMinor: &p2}, {PriceMinor: &p3},
		},
	}
	RescaleOperation(&op, 2, 0)

	if *op.SumMinor != 10 {
		t.Fatalf("итог = %d, want 10", *op.SumMinor)
	}
	var sum int64
	for _, it := range op.Items {
		sum += *it.PriceMinor
	}
	if sum != 10 {
		t.Errorf("сумма позиций = %d, а итог 10", sum)
	}
}

// Фиксы внутри позиции не должны раздуться до всей цены: остальное принадлежит
// весовым участникам.
func TestRescaleDownKeepsFixedSharesWithinPrice(t *testing.T) {
	total, price := int64(1000), int64(1000)
	f1, f2 := int64(250), int64(250)
	op := Operation{
		SumMinor: &total,
		Items: []OperationItem{{
			PriceMinor: &price,
			Shares: []ItemShare{
				{UserId: 1, AmountMinor: &f1},
				{UserId: 2, AmountMinor: &f2},
				{UserId: 3, Weight: 1},
			},
		}},
	}
	RescaleOperation(&op, 2, 0)

	var fixed int64
	for _, sh := range op.Items[0].Shares {
		if sh.AmountMinor != nil {
			fixed += *sh.AmountMinor
		}
	}
	if fixed != 5 {
		t.Errorf("фиксы = %d, want 5 (было 5.00 из 10.00)", fixed)
	}
	if op.Items[0].Shares[2].AmountMinor != nil {
		t.Error("весовой участник получил фиксированную долю")
	}
}

// Смена шкалы комнаты поднимает версию: без этого путь 0 → 2 → 0 выглядел бы
// для офлайн-очереди так, будто ничего не менялось.
func TestRescaleRoomBumpsScaleVersion(t *testing.T) {
	room := &Room{Currency: "USD", Operations: &[]Operation{{Sum: 20}}}
	before := RoomExponent(room)

	RescaleRoom(room, 0)
	if room.ScaleVersion != 1 {
		t.Errorf("после первой смены версия = %d, want 1", room.ScaleVersion)
	}
	RescaleRoom(room, before)
	if room.ScaleVersion != 2 {
		t.Errorf("после возврата версия = %d, want 2 — а шкала та же, что вначале", room.ScaleVersion)
	}
	if RoomExponent(room) != before {
		t.Errorf("шкала = %d, want %d", RoomExponent(room), before)
	}
}

// Смена на ту же шкалу — не событие: версия расти не должна.
func TestRescaleRoomNoopOnSameScale(t *testing.T) {
	room := &Room{Currency: "RUB", Operations: &[]Operation{{Sum: 100}}}
	RescaleRoom(room, RoomExponent(room))
	if room.ScaleVersion != 0 {
		t.Errorf("версия выросла на пустом месте: %d", room.ScaleVersion)
	}
}

// Круг «включили копейки — выключили» на ровных суммах возвращает исходные
// числа: люди не должны увидеть, что деньги поехали от одного тумблера.
func TestRescaleRoundTripOnWholeAmounts(t *testing.T) {
	op := Operation{
		Sum:               1200,
		RecipientsWithSum: []RecipientWithSum{{Sum: 400}, {Sum: 400}, {Sum: 400}},
	}
	FillMoney(&op, 0)
	RescaleOperation(&op, 0, 2)
	RescaleOperation(&op, 2, 0)

	if op.Sum != 1200 {
		t.Errorf("сумма после круга = %d, want 1200", op.Sum)
	}
	for i, r := range op.RecipientsWithSum {
		if *r.SumMinor != 400 {
			t.Errorf("доля[%d] = %d, want 400", i, *r.SumMinor)
		}
	}
}

// Большие суммы: комната в рупиях живёт числами порядка 10^8 единиц, а с
// копейками это 10^10 минорных. Наивное total*w/sum на таких числах уходит за
// int64 и молча даёт мусор — деньги, взятые ниоткуда.
func TestDistributeSurvivesLargeAmounts(t *testing.T) {
	// 1 000 000 000 рупий с копейками — это 10^11 минорных единиц. Наивное
	// total*w на таких числах даёт 10^20 и переполняет int64.
	const huge = int64(100_000_000_000)
	weights := []int64{huge / 3, huge / 3, huge - 2*(huge/3)}

	got, _ := Distribute(huge/100, weights)
	var sum int64
	for _, v := range got {
		sum += v
	}
	if sum != huge/100 {
		t.Fatalf("сумма долей = %d, want %d", sum, huge/100)
	}
	for i, v := range got {
		if v <= 0 {
			t.Errorf("доля[%d] = %d — на больших числах деление поехало", i, v)
		}
	}
}

// Пересчёт вниз на предельных суммах тоже обязан сходиться.
func TestRescaleDownSurvivesLargeAmounts(t *testing.T) {
	total := int64(100_000_000_000)
	a, b := total/2, total-total/2
	op := Operation{
		SumMinor:          &total,
		RecipientsWithSum: []RecipientWithSum{{SumMinor: &a}, {SumMinor: &b}},
	}
	RescaleOperation(&op, 2, 0)

	var shares int64
	for _, r := range op.RecipientsWithSum {
		shares += *r.SumMinor
	}
	if shares != *op.SumMinor {
		t.Errorf("сумма долей = %d, итог = %d", shares, *op.SumMinor)
	}
	if *op.SumMinor != 1_000_000_000 {
		t.Errorf("итог = %d, want 1000000000", *op.SumMinor)
	}
}

// Веса вне области определения: их сумма не помещается в int64. Раздавать
// нечего, и функция обязана СКАЗАТЬ об этом, а не вернуть правдоподобный
// вектор. Прежняя редакция делила поровну — и выдавала деньги участнику с
// нулевым весом, то есть тому, кто ни за что не платил.
func TestDistributeRefusesWeightsOutOfDomain(t *testing.T) {
	const maxI64 = int64(^uint64(0) >> 1)

	got, ok := Distribute(10, []int64{maxI64, maxI64, 3, 0})
	if ok {
		t.Fatalf("веса вне области определения приняты: %v", got)
	}
	if got != nil {
		t.Errorf("вместе с отказом вернулся вектор %v", got)
	}
}

// Нулевой вес не получает ничего и в обычном случае — правило отдельное, и
// подменять его «делением поровну» нельзя.
func TestDistributeNeverPaysZeroWeight(t *testing.T) {
	got, ok := Distribute(10, []int64{0, 5, 5, 0})
	if !ok {
		t.Fatal("обычные веса отвергнуты")
	}
	if got[0] != 0 || got[3] != 0 {
		t.Errorf("нулевые веса получили деньги: %v", got)
	}
}

// Огромная легаси-сумма без минорного поля. Прежде проверка диапазона стояла
// ПОСЛЕ пересчёта и ловила ноль вместо исходной суммы: SumMinorAt отдавал ноль,
// пересчёт клал его в документ, проекция обнуляла Sum — и «безопасный» ноль
// проходил. Деньги исчезали целиком, без ошибки.
func TestRescaleRoomRefusesHugeLegacySum(t *testing.T) {
	op := Operation{Sum: 184467440737095517}
	room := &Room{Currency: "USD", Operations: &[]Operation{op}}
	zero := 0
	room.DisplayExponent = &zero

	err := RescaleRoom(room, 2)
	if !errors.Is(err, ErrRescaleImpossible) {
		t.Fatalf("RescaleRoom: %v, want ErrRescaleImpossible", err)
	}
	if !errors.Is(err, ErrMoneyOutOfRange) {
		t.Errorf("ошибка не несёт причину: %v", err)
	}

	after := (*room.Operations)[0]
	if after.Sum != 184467440737095517 {
		t.Errorf("сумма изменилась: %d — деньги исчезли", after.Sum)
	}
	if after.SumMinor != nil {
		t.Errorf("в документ записали минорное значение %d", *after.SumMinor)
	}
	if RoomExponent(room) != 0 {
		t.Errorf("шкала = %d, want 0", RoomExponent(room))
	}
}

// «Целиком или никак» верно и для самой функции, а не только для записи в базу:
// вызывающий, не проверивший ошибку, не должен получить комнату, где часть
// операций уже в новой шкале.
func TestRescaleRoomLeavesOperationsUntouchedOnError(t *testing.T) {
	good := Operation{Sum: 100, RecipientsWithSum: []RecipientWithSum{{User: User{ID: 1}, Sum: 100}}}
	bad := Operation{
		Sum:       20,
		SplitType: SplitTypeByExactAmount,
		RecipientsWithSum: []RecipientWithSum{
			{User: User{ID: 1}, Sum: 10}, {User: User{ID: 2}, Sum: 10},
		},
		Items: []OperationItem{{Price: 15, Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}}}},
	}
	room := &Room{Currency: "USD", Operations: &[]Operation{good, bad}}
	zero := 0
	room.DisplayExponent = &zero

	if err := RescaleRoom(room, 2); !errors.Is(err, ErrRescaleImpossible) {
		t.Fatalf("RescaleRoom: %v, want ErrRescaleImpossible", err)
	}

	ops := *room.Operations
	// Первая операция переводится без проблем — и всё же обязана остаться
	// нетронутой, потому что вторая не переводится.
	if ops[0].SumMinor != nil {
		t.Errorf("хорошую операцию всё-таки пересчитали: sumMinor = %d", *ops[0].SumMinor)
	}
	if ops[0].Sum != 100 {
		t.Errorf("сумма хорошей операции изменилась: %d", ops[0].Sum)
	}
	if ops[1].Sum != 20 {
		t.Errorf("сумма плохой операции изменилась: %d", ops[1].Sum)
	}
}
