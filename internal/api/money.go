package api

// ShareOf — каноническое правило деления расхода поровну (единое для сервера и клиентов):
// для суммы sum (целые рубли) и n получателей в порядке массива получателей
// base = sum/n (целочисленно), r = sum%n; получатель с индексом i платит base+1
// при i < r, иначе base. Сумма долей всегда равна sum.
func ShareOf(sum, n, i int) int {
	share := sum / n
	rem := sum % n
	if rem < 0 {
		// отрицательная сумма (возврат/корректировка): Go усекает деление к нулю,
		// поэтому остаток раздаём вниз, иначе Σ долей != sum
		if i < -rem {
			share--
		}
		return share
	}
	if i < rem {
		share++
	}
	return share
}

// ShareOfMinor — то же каноническое правило деления, но в минорных единицах.
// Отдельная функция, а не приведение к int: единица денег в модели одна, и
// смешивать типы на границе значит однажды перепутать их.
func ShareOfMinor(sum int64, n, i int) int64 {
	share := sum / int64(n)
	rem := sum % int64(n)
	if rem < 0 {
		if int64(i) < -rem {
			share--
		}
		return share
	}
	if int64(i) < rem {
		share++
	}
	return share
}
