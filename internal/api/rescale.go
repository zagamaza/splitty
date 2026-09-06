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

// maxInt64 — потолок для проверки переполнения при сложении весов.
const maxInt64 = int64(^uint64(0) >> 1)

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
//
// Второе значение false — веса вне области определения (их сумма не помещается
// в int64). Раздавать в этом случае нечего, и вызывающий обязан отказать, а не
// получить правдоподобный, но неверный вектор.
func Distribute(total int64, weights []int64) ([]int64, bool) {
	out := make([]int64, len(weights))
	if len(weights) == 0 {
		return out, true
	}

	// Сумму весов считаем С ПРОВЕРКОЙ переполнения. Без неё веса
	// [MaxInt64, MaxInt64, 3] сворачиваются в единицу, и доказательство
	// «w <= sum», на котором держится безопасность bits.Div64, рушится —
	// функция уронила бы процесс паникой. Переполнение означает, что весов
	// таких быть не может: делим поровну и уходим.
	var sum int64
	for _, w := range weights {
		if w <= 0 {
			continue
		}
		if sum > maxInt64-w {
			// Веса вне области определения. Молча подменять смысл нельзя:
			// прежняя редакция делила поровну и раздавала деньги участникам с
			// НУЛЕВЫМ весом, то есть тем, кто ни за что не платил.
			return nil, false
		}
		sum += w
	}
	if sum == 0 {
		out[0] = total
		return out, true
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
	return out, true
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
// Вверх — точно, вниз — с округлением ИТОГА и повторной раздачей долей, так
// что «сумма долей равна сумме расхода» остаётся верным и после перехода.
//
// ⚠️ Доли и цены позиций НЕ переводятся поштучно. Округли их по отдельности —
// и расход 1,50 на троих даст 1+1+1 = 3 при итоге 2, то есть деньги, которых
// нет. Округляется только итог, всё остальное раздаётся по нему заново.
func RescaleOperation(o *Operation, from, to int) {
	if o == nil || from == to {
		return
	}

	// Веса снимаем ДО перевода: пропорции от шкалы не зависят.
	weights := shareWeights(o, from)
	itemWeights := make([]int64, len(o.Items))
	fixedWeights := make([][]int64, len(o.Items))
	for i, it := range o.Items {
		itemWeights[i] = it.PriceMinorAt(from)
		for _, sh := range it.Shares {
			amount, ok := sh.AmountMinorAt(from)
			if !ok {
				amount = -1
			}
			fixedWeights[i] = append(fixedWeights[i], amount)
		}
	}

	total := convertMinor(o.SumMinorAt(from), from, to)
	o.SumMinor = &total

	rescaleItems(o, total, itemWeights, fixedWeights, from, to)

	// У itemized-расхода плоские доли ВЫВОДЯТСЯ из позиций, а не считаются
	// отдельно: иначе после смены шкалы два пути дают разные деньги.
	if len(o.Items) > 0 {
		if derived, ok := sharesFromItems(o, to); ok {
			applySharesWithLegacy(o, derived, to)
			FillMoney(o, to)
			return
		}
	}
	if shares, ok := SharesMinorFrom(o, total, weights); ok {
		applySharesWithLegacy(o, shares, to)
	}
	FillMoney(o, to)
}

// convertMinor переводит значение из шкалы from в шкалу to: вверх точно, вниз
// по единственному правилу округления продукта.
func convertMinor(v int64, from, to int) int64 {
	if to > from {
		return rescaleUp(v, from, to)
	}
	return rescaleDown(v, from, to)
}

// rescaleItems переводит позиции чека. Вверх — умножением, вниз — раздачей
// нового итога по прежним ценам как по весам, чтобы сумма позиций сходилась с
// суммой расхода.
func rescaleItems(o *Operation, total int64, itemWeights []int64, fixedWeights [][]int64, from, to int) {
	if len(o.Items) == 0 {
		return
	}

	var prices []int64
	if to > from {
		prices = make([]int64, len(o.Items))
		for i, w := range itemWeights {
			prices[i] = rescaleUp(w, from, to)
		}
	} else {
		var ok bool
		prices, ok = Distribute(total, itemWeights)
		if !ok {
			// Веса позиций вне области определения — пересчитывать нечего,
			// оставляем как есть, а запись такой комнаты отвергнет проверка
			// диапазона денег.
			return
		}
	}

	for i := range o.Items {
		it := &o.Items[i]
		price := prices[i]
		it.PriceMinor = &price

		// Фиксированные доли делят не всю цену, а ровно ту часть, что делили
		// раньше: остальное принадлежит весовым участникам.
		var idx []int
		var fixed []int64
		for j, w := range fixedWeights[i] {
			if w < 0 {
				continue
			}
			idx = append(idx, j)
			fixed = append(fixed, w)
		}
		if len(idx) == 0 {
			continue
		}
		var fixedTotal int64
		for _, v := range fixed {
			fixedTotal += v
		}
		newFixedTotal := convertMinor(fixedTotal, from, to)
		if newFixedTotal > price {
			newFixedTotal = price
		}
		spread, ok := Distribute(newFixedTotal, fixed)
		if !ok {
			continue
		}
		for k, v := range spread {
			amount := v
			it.Shares[idx[k]].AmountMinor = &amount
		}
	}
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
