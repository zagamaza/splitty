package api

import "math/bits"

// remainderTaken помечает долю, уже получившую единицу остатка: остаток
// раздаётся по одной на участника, и второй раз одному и тому же не достаётся.
const remainderTaken = ^uint64(0)

// maxInt64 — потолок для проверки переполнения при сложении весов.
const maxInt64 = int64(^uint64(0) >> 1)

// Distribute раздаёт total пропорционально весам так, что сумма результата в
// ТОЧНОСТИ равна total. Базовая доля — floor(total*w/Σw), остаток раздаётся по
// одному тем, у кого дробная часть больше (при равенстве — меньший индекс,
// чтобы результат не зависел от порядка обхода карты).
//
// Нулевая сумма весов означает, что делить не по чему: тогда total уходит
// первому — иначе деньги просто исчезли бы. Нулевой вес не получает ничего.
//
// Второе значение false — веса вне области определения (их сумма не помещается
// в int64). Раздавать в этом случае нечего, и вызывающий обязан отказать, а не
// получить правдоподобный, но неверный вектор.
func Distribute(total int64, weights []int64) ([]int64, bool) {
	out := make([]int64, len(weights))
	if len(weights) == 0 {
		return out, true
	}

	// Сумму весов считаем С ПРОВЕРКОЙ переполнения: без неё веса
	// [MaxInt64, MaxInt64, 3] сворачиваются в единицу, доказательство «w <= sum»
	// рушится, и bits.Div64 уронил бы процесс паникой.
	var sum int64
	for _, w := range weights {
		if w <= 0 {
			continue
		}
		if sum > maxInt64-w {
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
		// total*w не помещается в int64 на больших суммах. Умножаем в 128 битах
		// и делим тем же ходом — точно и без переполнения. bits.Div64 паникует,
		// когда частное не влезает в 64 бита; здесь этого не бывает: w <= sum,
		// значит total*w/sum <= total, а total — int64.
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
