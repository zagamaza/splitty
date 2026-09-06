package api

import (
	"errors"
	"fmt"
	"math/bits"
)

// ErrRescaleImpossible — комнату нельзя перевести в другую шкалу, не соврав.
// Так бывает у испорченного документа: сумма позиций не сходится с итогом
// расхода, состав участников в позициях не тот, или веса вне области
// определения. Записывать половину пересчёта нельзя, поэтому отказ.
var ErrRescaleImpossible = errors.New("cannot rescale room without losing money")

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
// RescaleOperation переводит все деньги операции из шкалы from в шкалу to.
//
// Безопасна: проверяет диапазон исходных сумм, считает на копии и присваивает
// только после полного успеха. На ошибке аргумент остаётся нетронутым.
//
// ⚠️ Прежняя редакция правила аргумент на месте и валидировала диапазон только
// в RescaleRoom. Прямой вызов на легаси-сумме вне диапазона возвращал nil и
// обнулял деньги, а на несогласованном itemized успевал записать часть полей
// и лишь потом отказывал. База была цела только потому, что единственный
// боевой вызов шёл из RescaleRoom по его собственной копии, — то есть
// безопасность держалась на вызывающем, а не на контракте.
func RescaleOperation(o *Operation, from, to int) error {
	if o == nil || from == to {
		return nil
	}
	if err := ValidateMoneyRange(o, from); err != nil {
		return fmt.Errorf("%w: %w", ErrRescaleImpossible, err)
	}
	if err := ValidateMoneyRange(o, to); err != nil {
		return fmt.Errorf("%w: %w", ErrRescaleImpossible, err)
	}
	next := copyOperation(o)
	if err := rescaleOperationInPlace(&next, from, to); err != nil {
		return err
	}
	*o = next
	return nil
}

// rescaleOperationInPlace — тот же пересчёт, но правит аргумент на месте и
// диапазон не проверяет. Вызывать только на копии, чью пригодность уже
// проверили: на ошибке оставляет операцию наполовину переведённой.
func rescaleOperationInPlace(o *Operation, from, to int) error {
	if o == nil || from == to {
		return nil
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

	if err := rescaleItems(o, total, itemWeights, fixedWeights, from, to); err != nil {
		return err
	}

	// У itemized-расхода плоские доли ВЫВОДЯТСЯ из позиций, а не считаются
	// отдельно: иначе после смены шкалы два пути дают разные деньги.
	if len(o.Items) > 0 {
		derived, ok := sharesFromItems(o, to)
		if !ok {
			// Позиции объявлены источником правды, но вывести из них доли не
			// вышло: их сумма разошлась с итогом расхода или состав участников
			// не тот. Свалиться на плоские доли значит записать комнату, где
			// два пути расчёта дают разные деньги.
			return ErrRescaleImpossible
		}
		applySharesWithLegacy(o, derived, to)
		FillMoney(o, to)
		return nil
	}
	shares, ok := SharesMinorFrom(o, total, weights)
	if !ok {
		return ErrRescaleImpossible
	}
	applySharesWithLegacy(o, shares, to)
	FillMoney(o, to)
	return nil
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
func rescaleItems(o *Operation, total int64, itemWeights []int64, fixedWeights [][]int64, from, to int) error {
	if len(o.Items) == 0 {
		return nil
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
			return ErrRescaleImpossible
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
			return ErrRescaleImpossible
		}
		for k, v := range spread {
			amount := v
			it.Shares[idx[k]].AmountMinor = &amount
		}
	}
	return nil
}

// RescaleRoom переводит комнату в шкалу to: все операции пересчитываются,
// шкала записывается, версия шкалы растёт. Версия нужна офлайн-очереди на
// телефоне: снимка самой шкалы мало, потому что путь 0 → 2 → 0 вернул бы
// прежнее значение при дважды пересчитанных суммах.
func RescaleRoom(r *Room, to int) error {
	if r == nil {
		return nil
	}
	from := RoomExponent(r)
	if from == to {
		return nil
	}
	if r.Operations == nil {
		exp := to
		r.DisplayExponent = &exp
		r.ScaleVersion++
		return nil
	}
	ops := *r.Operations

	// ⚠️ Диапазон проверяем ДО первой правки, по ИСХОДНЫМ суммам. Проверка
	// после пересчёта не ловила ничего: у легаси-операции с суммой вне
	// диапазона минорного поля нет, SumMinorAt отдаёт ноль, пересчёт кладёт
	// этот ноль в документ, проекция делает Sum нулём — и «безопасный» ноль
	// спокойно проходит проверку. Деньги исчезали целиком.
	for i := range ops {
		if err := ValidateMoneyRange(&ops[i], from); err != nil {
			return fmt.Errorf("%w: %w", ErrRescaleImpossible, err)
		}
		if err := ValidateMoneyRange(&ops[i], to); err != nil {
			return fmt.Errorf("%w: %w", ErrRescaleImpossible, err)
		}
	}

	// ⚠️ Считаем на КОПИИ и присваиваем только после полного успеха.
	// «Целиком или никак» обязано быть верным и для самой функции, а не только
	// для записи в базу: иначе вызывающий, не проверивший ошибку, получает
	// комнату, где часть операций уже в новой шкале, а часть в старой.
	next := make([]Operation, len(ops))
	for i := range ops {
		next[i] = copyOperation(&ops[i])
		if err := rescaleOperationInPlace(&next[i], from, to); err != nil {
			return err
		}
	}

	copy(ops, next)
	exp := to
	r.DisplayExponent = &exp
	r.ScaleVersion++
	return nil
}

// copyOperation — копия операции вглубь по всем полям, которые правит пересчёт.
func copyOperation(o *Operation) Operation {
	c := *o
	if o.SumMinor != nil {
		v := *o.SumMinor
		c.SumMinor = &v
	}
	if o.RecipientsWithSum != nil {
		rws := make([]RecipientWithSum, len(o.RecipientsWithSum))
		copy(rws, o.RecipientsWithSum)
		for i := range rws {
			if rws[i].SumMinor != nil {
				v := *rws[i].SumMinor
				rws[i].SumMinor = &v
			}
		}
		c.RecipientsWithSum = rws
	}
	if o.Items != nil {
		items := make([]OperationItem, len(o.Items))
		copy(items, o.Items)
		for i := range items {
			if items[i].PriceMinor != nil {
				v := *items[i].PriceMinor
				items[i].PriceMinor = &v
			}
			if items[i].Shares != nil {
				sh := make([]ItemShare, len(items[i].Shares))
				copy(sh, items[i].Shares)
				for j := range sh {
					if sh[j].AmountMinor != nil {
						v := *sh[j].AmountMinor
						sh[j].AmountMinor = &v
					}
					if sh[j].Amount != nil {
						v := *sh[j].Amount
						sh[j].Amount = &v
					}
				}
				items[i].Shares = sh
			}
		}
		c.Items = items
	}
	return c
}
