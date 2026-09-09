package api

import (
	"errors"
	"math"
	"sort"
)

var (
	// ErrItemOverAllocated фиксированные суммы позиции превышают её цену
	ErrItemOverAllocated = errors.New("сумма фиксированных долей превышает цену позиции")
	// ErrItemUnallocated цену позиции не на кого распределить (нет весовых
	// участников, а фиксы не покрывают цену полностью)
	ErrItemUnallocated = errors.New("цена позиции не распределена полностью")
	// ErrNegativeAmount отрицательная фиксированная сумма
	ErrNegativeAmount = errors.New("отрицательная фиксированная сумма")
	// ErrSurchargePrice надбавка без положительной цены
	ErrSurchargePrice = errors.New("надбавка требует положительной цены")
	// ErrInvariant сумма выведенных долей не сошлась с общей суммой (баг расчёта)
	ErrInvariant = errors.New("сумма долей не равна итогу")
	// ErrOverflow входные величины приводят к переполнению при взвешенном
	// делении (amount*weight выходит за пределы int64) — защита от зацикливания
	ErrOverflow = errors.New("слишком большие величины позиции")
)

// weightShare участник и его вес для целочисленного взвешенного деления.
type weightShare struct {
	id     int
	weight int
}

// splitByWeight делит amount (МИНОРНЫЕ единицы) между участниками
// пропорционально весам. Базовая доля — floor(amount*weight/totalWeight);
// остаток от округления раздаётся по одному тем, у кого базовая доля больше
// (при равенстве — меньший userId). Сумма долей всегда равна amount. Требует
// totalWeight > 0.
//
// Единица здесь — минорная, а не целая: чек в тусе с копейками делится до
// копейки, и остаток раздаётся копейками, а не рублями. Сам алгоритм от
// единицы не зависит — меняется только то, что в неё кладут.
func splitByWeight(amount int64, ws []weightShare) (map[int]int64, error) {
	out := make(map[int]int64, len(ws))
	// защита от переполнения/зацикливания: отрицательные величины недопустимы
	if amount < 0 {
		return nil, ErrOverflow
	}
	// Схлопываем дубли по id и отбрасываем нулевые веса ДО деления: один и тот же
	// участник может встретиться в shares дважды, а при proportional-надбавке вес
	// равен базовой доле и легко равен нулю (человек ничего не ел). Без этого
	// раздача остатка ниже отдавала бы копейки тому, у кого вес 0, и дважды —
	// дублю, хотя итоговая сумма сходилась и инвариант молчал.
	agg := make([]weightShare, 0, len(ws))
	idx := make(map[int]int, len(ws))
	totalW := 0
	for _, w := range ws {
		if w.weight < 0 {
			return nil, ErrOverflow
		}
		// аддитивное переполнение суммы весов
		if totalW > math.MaxInt-w.weight {
			return nil, ErrOverflow
		}
		totalW += w.weight
		if w.weight == 0 {
			continue
		}
		if i, ok := idx[w.id]; ok {
			if agg[i].weight > math.MaxInt-w.weight {
				return nil, ErrOverflow
			}
			agg[i].weight += w.weight
			continue
		}
		idx[w.id] = len(agg)
		agg = append(agg, w)
	}
	ws = agg
	if totalW <= 0 {
		return out, nil
	}
	var given int64
	for _, w := range ws {
		// amount*w.weight не должно переполнить int64
		if w.weight != 0 && amount > math.MaxInt64/int64(w.weight) {
			return nil, ErrOverflow
		}
		v := amount * int64(w.weight) / int64(totalW)
		// += , а не = : один и тот же участник может встретиться в shares дважды
		out[w.id] += v
		given += v
	}
	rem := amount - given
	// при корректных входах остаток дробей строго меньше числа участников;
	// иначе это признак переполнения/бага — не раздаём его в цикле, а сигналим
	if rem < 0 || rem > int64(len(ws)) {
		return nil, ErrOverflow
	}
	if rem == 0 {
		return out, nil
	}
	order := make([]weightShare, len(ws))
	copy(order, ws)
	sort.Slice(order, func(i, j int) bool {
		if out[order[i].id] != out[order[j].id] {
			return out[order[i].id] > out[order[j].id]
		}
		return order[i].id < order[j].id
	})
	for i := int64(0); i < rem; i++ {
		out[order[int(i)%len(order)].id]++
	}
	return out, nil
}

// SplitItem делит цену позиции (МИНОРНЫЕ единицы) между её участниками:
// сначала снимаются фиксированные доли, остаток делится по Weight
// (splitByWeight). Возвращает карту userId→сумма; сумма всегда равна price.
func SplitItem(price int64, shares []ItemShare) (map[int]int64, error) {
	if price < 0 {
		return nil, ErrOverflow
	}
	out := make(map[int]int64, len(shares))
	var fixed int64
	var weighted []weightShare
	for _, s := range shares {
		if amount, has := s.AmountMinorOrLegacy(); has {
			if amount < 0 {
				return nil, ErrNegativeAmount
			}
			// аддитивное переполнение суммы фиксов
			if fixed > math.MaxInt64-amount {
				return nil, ErrOverflow
			}
			out[s.UserId] += amount
			fixed += amount
			continue
		}
		if s.Weight > 0 {
			weighted = append(weighted, weightShare{id: s.UserId, weight: s.Weight})
		}
	}
	if fixed > price {
		return nil, ErrItemOverAllocated
	}
	rem := price - fixed
	if len(weighted) == 0 {
		if rem != 0 {
			return nil, ErrItemUnallocated
		}
		return out, nil
	}
	d, err := splitByWeight(rem, weighted)
	if err != nil {
		return nil, err
	}
	for id, v := range d {
		out[id] += v
	}
	// страховка контракта: сумма долей обязана равняться цене (ловит любой
	// незамеченный дефект деления до того, как суммы уйдут в операцию)
	var got int64
	for _, v := range out {
		got += v
	}
	if got != price {
		return nil, ErrInvariant
	}
	return out, nil
}

// SplitSurcharge делит надбавку (сбор/чаевые/доставку) по базовым долям людей.
// proportional → вес участника равен его базовой доле; equally → всем поровну.
// base — суммы, выведенные из обычных позиций (кто сколько съел).
func SplitSurcharge(price int64, rule SplitRule, base map[int]int64) map[int]int64 {
	ids := make([]int, 0, len(base))
	for id := range base {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	ws := make([]weightShare, 0, len(ids))
	var totalBase int64
	for _, id := range ids {
		totalBase += base[id]
	}
	for _, id := range ids {
		w := 1
		// пропорционально работает только если у базы есть положительный вес;
		// иначе (все нули) откатываемся к делению поровну, чтобы сбор не потерялся
		if rule == SplitProportional && totalBase > 0 {
			// Вес — базовая доля в минорных: она же и ограничена валидацией,
			// поэтому в int влезает.
			w = int(base[id])
		}
		ws = append(ws, weightShare{id: id, weight: w})
	}
	// величины надбавки/базы уже ограничены валидацией; при переполнении
	// вернётся nil, и инвариант в DeriveShares поймает несходящуюся сумму
	res, _ := splitByWeight(price, ws)
	return res
}

// DeriveShares сворачивает позиции чека в плоскую карту userId→сумма и общий
// итог. Обычные позиции считаются первыми (образуют базу), затем на эту базу
// накладываются надбавки. Возвращает ошибку, если любая позиция невалидна или
// нарушен инвариант «сумма долей == итог».
func DeriveShares(items []OperationItem) (map[int]int64, int64, error) {
	base := make(map[int]int64)
	var total int64
	for _, it := range items {
		if it.Kind == ItemKindSurcharge {
			continue
		}
		d, err := SplitItem(it.PriceMinorOrLegacy(), it.Shares)
		if err != nil {
			return nil, 0, err
		}
		for id, v := range d {
			base[id] += v
		}
		total += it.PriceMinorOrLegacy()
	}

	out := make(map[int]int64, len(base))
	for id, v := range base {
		out[id] = v
	}
	for _, it := range items {
		if it.Kind != ItemKindSurcharge {
			continue
		}
		if it.PriceMinorOrLegacy() <= 0 {
			return nil, 0, ErrSurchargePrice
		}
		for id, v := range SplitSurcharge(it.PriceMinorOrLegacy(), it.Split, base) {
			out[id] += v
		}
		total += it.PriceMinorOrLegacy()
	}

	var sum int64
	for _, v := range out {
		sum += v
	}
	if sum != total {
		return nil, 0, ErrInvariant
	}
	return out, total, nil
}
