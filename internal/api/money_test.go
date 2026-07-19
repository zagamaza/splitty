package api

import "testing"

// каноническое правило деления: сумма долей всегда равна сумме расхода
func TestShareOfSumsToTotal(t *testing.T) {
	for _, tc := range []struct{ sum, n int }{
		{1000, 3}, {100, 3}, {100, 2}, {199, 100}, {5, 3}, {1, 6}, {13, 3}, {7, 7}, {1, 1},
	} {
		var total int
		for i := 0; i < tc.n; i++ {
			total += ShareOf(tc.sum, tc.n, i)
		}
		if total != tc.sum {
			t.Errorf("ShareOf(%d, %d): сумма долей = %d, want %d", tc.sum, tc.n, total, tc.sum)
		}
	}
	// 1000/3: остаток достаётся первому получателю массива
	if got := []int{ShareOf(1000, 3, 0), ShareOf(1000, 3, 1), ShareOf(1000, 3, 2)}; got[0] != 334 || got[1] != 333 || got[2] != 333 {
		t.Errorf("ShareOf(1000, 3, i) = %v, want [334 333 333]", got)
	}
}

// TestShareOf_NegativeSum — Go усекает целочисленное деление к нулю, поэтому
// для отрицательных сумм (возврат/корректировка) остаток надо раздавать вниз,
// иначе Σ долей != sum и инвариант из доккомментария ShareOf ломается.
func TestShareOf_NegativeSum(t *testing.T) {
	cases := []struct{ sum, n int }{{-10, 3}, {-1, 6}, {-100, 3}, {-5, 3}, {-7, 7}, {0, 3}, {-1, 1}}
	for _, c := range cases {
		total := 0
		for i := 0; i < c.n; i++ {
			total += ShareOf(c.sum, c.n, i)
		}
		if total != c.sum {
			t.Errorf("ShareOf(%d, %d): Σ долей = %d, ожидали %d", c.sum, c.n, total, c.sum)
		}
	}
}
