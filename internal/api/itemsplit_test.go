package api

import (
	"math"
	"reflect"
	"testing"
	"time"
)

const maxInt = math.MaxInt

func amt(v int) *int { return &v }

func sumMap(m map[int]int) int {
	s := 0
	for _, v := range m {
		s += v
	}
	return s
}

func TestSplitItem(t *testing.T) {
	tests := []struct {
		name    string
		price   int
		shares  []ItemShare
		want    map[int]int
		wantErr bool
	}{
		{
			name:   "поровну на троих",
			price:  300,
			shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}, {UserId: 3, Weight: 1}},
			want:   map[int]int{1: 100, 2: 100, 3: 100},
		},
		{
			name:   "неравные веса 5/3/2 (баурсаки)",
			price:  500,
			shares: []ItemShare{{UserId: 2, Weight: 5}, {UserId: 1, Weight: 3}, {UserId: 4, Weight: 2}},
			want:   map[int]int{2: 250, 1: 150, 4: 100},
		},
		{
			name:   "микс: фикс 500 + остаток поровну (вино)",
			price:  3000,
			shares: []ItemShare{{UserId: 4, Amount: amt(500)}, {UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
			want:   map[int]int{4: 500, 1: 1250, 2: 1250},
		},
		{
			name:   "полностью ручные суммы",
			price:  2500,
			shares: []ItemShare{{UserId: 1, Amount: amt(1500)}, {UserId: 2, Amount: amt(700)}, {UserId: 3, Amount: amt(300)}},
			want:   map[int]int{1: 1500, 2: 700, 3: 300},
		},
		{
			name:   "неровный остаток поровну — тому, у кого доля больше (при равенстве меньший userId)",
			price:  100,
			shares: []ItemShare{{UserId: 3, Weight: 1}, {UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
			want:   map[int]int{1: 34, 2: 33, 3: 33},
		},
		{
			name:   "неровный остаток по весам — остаток крупнейшей доле",
			price:  10,
			shares: []ItemShare{{UserId: 1, Weight: 2}, {UserId: 2, Weight: 1}, {UserId: 3, Weight: 1}},
			want:   map[int]int{1: 6, 2: 2, 3: 2},
		},
		{
			name:   "одиночный участник получает всё",
			price:  400,
			shares: []ItemShare{{UserId: 4, Weight: 1}},
			want:   map[int]int{4: 400},
		},
		{
			name:   "фикс ровно равен цене — весов нет",
			price:  800,
			shares: []ItemShare{{UserId: 1, Amount: amt(800)}},
			want:   map[int]int{1: 800},
		},
		{
			name:    "перебор фиксов над ценой позиции",
			price:   100,
			shares:  []ItemShare{{UserId: 1, Amount: amt(150)}},
			wantErr: true,
		},
		{
			name:    "все фиксы, но сумма не сходится с ценой",
			price:   1000,
			shares:  []ItemShare{{UserId: 1, Amount: amt(400)}, {UserId: 2, Amount: amt(400)}},
			wantErr: true,
		},
		{
			name:    "пустые shares при ненулевой цене",
			price:   400,
			shares:  nil,
			wantErr: true,
		},
		{
			name:    "отрицательный фикс",
			price:   400,
			shares:  []ItemShare{{UserId: 1, Amount: amt(-50)}, {UserId: 2, Weight: 1}},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitItem(tc.price, tc.shares)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, получено %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SplitItem(%d) = %v, want %v", tc.price, got, tc.want)
			}
			if s := sumMap(got); s != tc.price {
				t.Fatalf("сумма долей = %d, want %d (инвариант)", s, tc.price)
			}
		})
	}
}

func TestSplitItem_WeightSumOverflow(t *testing.T) {
	// аддитивное переполнение суммы весов не должно давать пустой сплит с nil-ошибкой
	_, err := SplitItem(100, []ItemShare{{UserId: 1, Weight: maxInt}, {UserId: 2, Weight: 1}})
	if err == nil {
		t.Fatal("ожидалась ошибка на переполнении суммы весов")
	}
}

func TestSplitItem_FixedSumOverflow(t *testing.T) {
	big := maxInt
	_, err := SplitItem(maxInt, []ItemShare{{UserId: 1, Amount: &big}, {UserId: 2, Amount: &big}})
	if err == nil {
		t.Fatal("ожидалась ошибка на переполнении суммы фиксов")
	}
}

func TestSplitItem_OverflowReturnsErrorFast(t *testing.T) {
	// Огромные price/weight переполняют amount*weight в int64: наивная раздача
	// остатка ушла бы в почти бесконечный цикл (DoS). Ждём быструю ошибку.
	done := make(chan error, 1)
	go func() {
		_, err := SplitItem(6917529027641081856, []ItemShare{
			{UserId: 1, Weight: 1_000_000},
			{UserId: 2, Weight: 1_000_000},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ожидалась ошибка на overflow-входе, получен успех")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SplitItem завис на overflow-входе (нет защиты от переполнения)")
	}
}

func TestDeriveShares_OverflowReturnsErrorFast(t *testing.T) {
	items := []OperationItem{{
		Name: "overflow", Price: 6917529027641081856, Qty: 1, Kind: ItemKindItem,
		Shares: []ItemShare{{UserId: 1, Weight: 1_000_000}, {UserId: 2, Weight: 1_000_000}},
	}}
	done := make(chan error, 1)
	go func() {
		_, _, err := DeriveShares(items)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ожидалась ошибка на overflow-входе DeriveShares")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DeriveShares завис на overflow-входе")
	}
}

func TestSplitSurcharge(t *testing.T) {
	// база: кто сколько съел
	base := map[int]int{1: 1800, 2: 1900, 3: 400, 4: 600}

	t.Run("пропорционально съеденному", func(t *testing.T) {
		got := SplitSurcharge(470, SplitProportional, base)
		want := map[int]int{1: 180, 2: 190, 3: 40, 4: 60}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("proportional = %v, want %v", got, want)
		}
		if s := sumMap(got); s != 470 {
			t.Fatalf("сумма = %d, want 470", s)
		}
	})

	t.Run("поровну между участниками базы", func(t *testing.T) {
		got := SplitSurcharge(400, SplitEqually, base)
		// 400/4 = 100 каждому
		want := map[int]int{1: 100, 2: 100, 3: 100, 4: 100}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("equally = %v, want %v", got, want)
		}
	})

	t.Run("поровну с остатком — крупнейшей доле, затем меньший userId", func(t *testing.T) {
		got := SplitSurcharge(10, SplitEqually, map[int]int{1: 500, 2: 500, 3: 500})
		// 10/3 = 3 каждому, остаток 1 → при равных долях меньший userId (1)
		want := map[int]int{1: 4, 2: 3, 3: 3}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("equally+remainder = %v, want %v", got, want)
		}
	})
}

func TestDeriveShares_FullReceipt(t *testing.T) {
	// Полный чек из Overview:
	// Пицца 1200 — я(1), Лёха(2), Саня(3)
	// Баурсаки 500 — Лёха(2) w5, я(1) w3, Маша(4) w2
	// Вино 3000 — Маша(4) фикс 500, остальное поровну я(1)/Лёха(2)
	// Сервисный сбор 10% = 470, proportional
	items := []OperationItem{
		{
			Name: "Пицца", Price: 1200, Qty: 1, Kind: ItemKindItem,
			Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}, {UserId: 3, Weight: 1}},
		},
		{
			Name: "Баурсаки", Price: 500, Qty: 10, Kind: ItemKindItem,
			Shares: []ItemShare{{UserId: 2, Weight: 5}, {UserId: 1, Weight: 3}, {UserId: 4, Weight: 2}},
		},
		{
			Name: "Вино", Price: 3000, Qty: 1, Kind: ItemKindItem,
			Shares: []ItemShare{{UserId: 4, Amount: amt(500)}, {UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
		},
		{
			Name: "Сервисный сбор", Price: 470, Qty: 1, Kind: ItemKindSurcharge,
			Split: SplitProportional, Percent: amt(10),
		},
	}
	got, total, err := DeriveShares(items)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	want := map[int]int{1: 1980, 2: 2090, 3: 440, 4: 660}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeriveShares = %v, want %v", got, want)
	}
	if total != 5170 {
		t.Fatalf("total = %d, want 5170", total)
	}
	if s := sumMap(got); s != total {
		t.Fatalf("инвариант: сумма долей %d != total %d", s, total)
	}
}

func TestDeriveShares_QtyIgnoredInMath(t *testing.T) {
	// Qty>1 не влияет на деление: Price — total строки
	a := []OperationItem{{Price: 100, Qty: 1, Kind: ItemKindItem, Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}}}}
	b := []OperationItem{{Price: 100, Qty: 7, Kind: ItemKindItem, Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}}}}
	ga, _, _ := DeriveShares(a)
	gb, _, _ := DeriveShares(b)
	if !reflect.DeepEqual(ga, gb) {
		t.Fatalf("Qty повлиял на расчёт: %v != %v", ga, gb)
	}
}

func TestDeriveShares_SurchargePercentUsesPriceNotPercent(t *testing.T) {
	// сумма сбора берётся из Price, Percent игнорируется в расчёте
	base := []OperationItem{{Price: 1000, Qty: 1, Kind: ItemKindItem, Shares: []ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}}}}
	withSurcharge := append([]OperationItem{}, base...)
	withSurcharge = append(withSurcharge, OperationItem{
		Price: 200, Kind: ItemKindSurcharge, Split: SplitEqually, Percent: amt(999), // абсурдный процент — не должен влиять
	})
	got, total, err := DeriveShares(withSurcharge)
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	// база 500/500, сбор 200 поровну 100/100 → 600/600
	want := map[int]int{1: 600, 2: 600}
	if !reflect.DeepEqual(got, want) || total != 1200 {
		t.Fatalf("got %v total %d, want %v total 1200", got, total, want)
	}
}

func TestDeriveShares_SurchargeZeroPrice(t *testing.T) {
	items := []OperationItem{
		{Price: 100, Kind: ItemKindItem, Shares: []ItemShare{{UserId: 1, Weight: 1}}},
		{Price: 0, Kind: ItemKindSurcharge, Split: SplitEqually},
	}
	if _, _, err := DeriveShares(items); err == nil {
		t.Fatal("ожидалась ошибка на surcharge с нулевой ценой")
	}
}
