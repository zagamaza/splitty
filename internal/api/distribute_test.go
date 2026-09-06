package api

import "testing"

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
		got, ok := Distribute(tc.total, tc.weights)
		if !ok {
			t.Fatalf("Distribute(%d, %v) отвергнут", tc.total, tc.weights)
		}
		var sum int64
		for _, v := range got {
			sum += v
		}
		if sum != tc.total {
			t.Errorf("Distribute(%d, %v) = %v, сумма %d", tc.total, tc.weights, got, sum)
		}
	}
}

// Нулевой вес не получает ничего: человек, который ни за что не платил, не
// должен получить копейку.
func TestDistributeNeverPaysZeroWeight(t *testing.T) {
	got, ok := Distribute(10, []int64{0, 5, 5, 0})
	if !ok {
		t.Fatal("обычные веса отвергнуты")
	}
	if got[0] != 0 || got[3] != 0 {
		t.Errorf("нулевые веса получили деньги: %v", got)
	}
}

// Делить не по чему — деньги всё равно не исчезают.
func TestDistributeAllZeroWeights(t *testing.T) {
	got, ok := Distribute(10, []int64{0, 0})
	if !ok || got[0]+got[1] != 10 {
		t.Errorf("Distribute(10, [0 0]) = %v, деньги потерялись", got)
	}
}

// Веса вне области определения: их сумма не помещается в int64. Функция обязана
// СКАЗАТЬ об этом, а не вернуть правдоподобный вектор. Прежняя редакция делила
// поровну и выдавала деньги участнику с нулевым весом.
func TestDistributeRefusesWeightsOutOfDomain(t *testing.T) {
	got, ok := Distribute(10, []int64{maxInt64, maxInt64, 3, 0})
	if ok {
		t.Fatalf("веса вне области определения приняты: %v", got)
	}
	if got != nil {
		t.Errorf("вместе с отказом вернулся вектор %v", got)
	}
}

// Большие суммы: наивное total*w уходит за int64 и даёт отрицательную долю.
func TestDistributeSurvivesLargeAmounts(t *testing.T) {
	const huge = int64(100_000_000_000)
	weights := []int64{huge / 3, huge / 3, huge - 2*(huge/3)}

	got, ok := Distribute(huge/100, weights)
	if !ok {
		t.Fatal("отвергнуто на продуктовых величинах")
	}
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

// Шаг деления: без копеек доли кратны единице валюты, с копейками — копейке.
func TestShareOfMinorStep(t *testing.T) {
	// 1000 ¥ на троих без копеек: 334 + 333 + 333, и ни одной сотой йены
	var sum int64
	for i := 0; i < 3; i++ {
		v := ShareOfMinorStep(100000, 3, i, MinorFactor)
		if v%MinorFactor != 0 {
			t.Errorf("доля[%d] = %d не кратна единице валюты", i, v)
		}
		sum += v
	}
	if sum != 100000 {
		t.Errorf("сумма долей = %d, want 100000", sum)
	}

	// Итог не кратен шагу — шаг игнорируется, иначе с итогом не сойтись
	sum = 0
	for i := 0; i < 3; i++ {
		sum += ShareOfMinorStep(2080, 3, i, MinorFactor)
	}
	if sum != 2080 {
		t.Errorf("сумма долей = %d, want 2080", sum)
	}
}
