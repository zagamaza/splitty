package api

// ShareOf — каноническое правило деления расхода поровну (единое для сервера и клиентов):
// для суммы sum (целые рубли) и n получателей в порядке массива получателей
// base = sum/n (целочисленно), r = sum%n; получатель с индексом i платит base+1
// при i < r, иначе base. Сумма долей всегда равна sum.
func ShareOf(sum, n, i int) int {
	share := sum / n
	if i < sum%n {
		share++
	}
	return share
}
