package api

import "math/bits"

// Смена шкалы комнаты — это пересчёт ВСЕХ её денег. Вверх (0 → 2) он точен:
// каждое число умножается на сто, ничего не теряется. Вниз (2 → 0) он теряет
// копейки, и терять их надо так, чтобы суммы продолжали сходиться.
//
// Наивное «округлить каждое число по отдельности» этого не даёт: расход 1.50
// на троих — это 50 + 50 + 50 копеек; округлив каждое по отдельности, получим
// 1 + 1 + 1 = 3 при итоге 2. Поэтому вниз округляется ТОЛЬКО итог, а доли
// раздаются заново пропорционально прежним — тем же способом, каким делится
// расход.

// remainderTaken помечает долю, уже получившую единицу остатка: остаток
// раздаётся по одной на участника, и второй раз одному и тому же не достаётся.
const remainderTaken = ^uint64(0)

// Distribute раздаёт total пропорционально весам weights так, что сумма
// результата в ТОЧНОСТИ равна total. Базовая доля — floor(total*w/Σw), остаток
// раздаётся по одному тем, у кого дробная часть больше (при равенстве —
// меньший индекс, чтобы результат не зависел от порядка обхода карты).
//
// Нулевая сумма весов означает, что делить не по чему: тогда total уходит
// первому — иначе деньги просто исчезли бы.
func Distribute(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	if len(weights) == 0 {
		return out
	}

	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum == 0 {
		out[0] = total
		return out
	}

	// Отрицательный итог (возврат, корректировка) раздаём тем же кодом на
	// модуле и возвращаем знак: иначе floor у отрицательных чисел уводит
	// остаток не в ту сторону и Σ долей != total.
	negative := total < 0
	if negative {
		total = -total
	}

	rem := total
	remainders := make([]uint64, len(weights))
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		// total*w не помещается в int64 на больших суммах: у комнаты в рупиях
		// это 10^10 минорных единиц, и произведение уходит за 10^20. Умножаем
		// в 128 битах и делим тем же ходом — точно и без переполнения.
		//
		// bits.Div64 паникует, когда частное не влезает в 64 бита; здесь этого
		// не бывает: w <= sum, значит total*w/sum <= total, а total — int64.
		hi, lo := bits.Mul64(uint64(total), uint64(w))
		q, r := bits.Div64(hi, lo, uint64(sum))
		out[i] = int64(q)
		remainders[i] = r
		rem -= out[i]
	}
	for ; rem > 0; rem-- {
		best := -1
		var bestRem uint64
		for i := range weights {
			if weights[i] <= 0 || remainders[i] == remainderTaken {
				continue
			}
			if best < 0 || remainders[i] > bestRem {
				best, bestRem = i, remainders[i]
			}
		}
		if best < 0 {
			break
		}
		out[best]++
		remainders[best] = remainderTaken
	}
	if negative {
		for i := range out {
			out[i] = -out[i]
		}
	}
	return out
}

// rescaleUp множитель перевода из шкалы from в БОЛЬШУЮ шкалу to.
func rescaleUp(v int64, from, to int) int64 {
	return v * int64(MinorFactor(to-from))
}

// rescaleDown округляет значение из шкалы from в МЕНЬШУЮ шкалу to по
// единственному правилу округления продукта.
func rescaleDown(v int64, from, to int) int64 {
	return int64(FromMinor(v, from-to))
}

// RescaleOperation переводит все деньги операции из шкалы from в шкалу to.
// Вверх — точно, вниз — с округлением итога и пропорциональной раздачей долей,
// так что «сумма долей равна сумме расхода» остаётся верным и после перехода.
func RescaleOperation(o *Operation, from, to int) {
	if o == nil || from == to {
		return
	}
	if to > from {
		rescaleOperationUp(o, from, to)
		return
	}
	rescaleOperationDown(o, from, to)
}

func rescaleOperationUp(o *Operation, from, to int) {
	total := rescaleUp(o.SumMinorAt(from), from, to)
	o.SumMinor = &total

	for i := range o.RecipientsWithSum {
		r := &o.RecipientsWithSum[i]
		v := rescaleUp(r.SumMinorAt(from), from, to)
		r.SumMinor = &v
	}
	for i := range o.Items {
		it := &o.Items[i]
		p := rescaleUp(it.PriceMinorAt(from), from, to)
		it.PriceMinor = &p
		for j := range it.Shares {
			if amount, ok := it.Shares[j].AmountMinorAt(from); ok {
				v := rescaleUp(amount, from, to)
				it.Shares[j].AmountMinor = &v
			}
		}
	}
	FillMoney(o, to)
}

func rescaleOperationDown(o *Operation, from, to int) {
	total := rescaleDown(o.SumMinorAt(from), from, to)
	o.SumMinor = &total

	// Доли раздаются заново от округлённого итога: округли их поодиночке — и
	// сумма долей разойдётся с суммой расхода.
	if len(o.RecipientsWithSum) > 0 {
		weights := make([]int64, len(o.RecipientsWithSum))
		for i, r := range o.RecipientsWithSum {
			weights[i] = r.SumMinorAt(from)
		}
		for i, v := range Distribute(total, weights) {
			share := v
			o.RecipientsWithSum[i].SumMinor = &share
		}
	}

	// Позиции чека — тем же способом: сначала итог, потом раздача, иначе
	// сумма позиций перестаёт сходиться с суммой расхода.
	if len(o.Items) > 0 {
		weights := make([]int64, len(o.Items))
		for i, it := range o.Items {
			weights[i] = it.PriceMinorAt(from)
		}
		prices := Distribute(total, weights)
		for i := range o.Items {
			it := &o.Items[i]
			price := prices[i]
			it.PriceMinor = &price

			// Фиксированные доли внутри позиции раздаются от её новой цены —
			// но только те, что заданы: доля без Amount так и остаётся без него,
			// иначе весовой участник превратился бы в фиксированного.
			idx := make([]int, 0, len(it.Shares))
			fixed := make([]int64, 0, len(it.Shares))
			for j := range it.Shares {
				if amount, ok := it.Shares[j].AmountMinorAt(from); ok {
					idx = append(idx, j)
					fixed = append(fixed, amount)
				}
			}
			if len(idx) == 0 {
				continue
			}
			// Фиксы делят не всю цену, а ровно ту часть, что и делили раньше:
			// остальное уходит весовым участникам, и их доля не должна
			// раздуться от смены шкалы.
			var fixedTotal int64
			for _, v := range fixed {
				fixedTotal += v
			}
			newFixedTotal := rescaleDown(fixedTotal, from, to)
			if newFixedTotal > price {
				newFixedTotal = price
			}
			for k, v := range Distribute(newFixedTotal, fixed) {
				amount := v
				it.Shares[idx[k]].AmountMinor = &amount
			}
		}
	}
	FillMoney(o, to)
}

// RescaleRoom переводит комнату в шкалу to: все операции пересчитываются,
// шкала записывается, версия шкалы растёт. Версия нужна офлайн-очереди на
// телефоне: снимка самой шкалы мало, потому что путь 0 → 2 → 0 вернул бы
// прежнее значение при дважды пересчитанных суммах.
func RescaleRoom(r *Room, to int) {
	if r == nil {
		return
	}
	from := RoomExponent(r)
	if from == to {
		return
	}
	if r.Operations != nil {
		ops := *r.Operations
		for i := range ops {
			RescaleOperation(&ops[i], from, to)
		}
	}
	exp := to
	r.DisplayExponent = &exp
	r.ScaleVersion++
}
