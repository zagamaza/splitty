package api

import "math"

// Деньги живут в двух представлениях, пока живы прежние сборки клиентов:
// МИНОРНЫЕ единицы (копейки, центы) целым числом — источник правды, и старые
// поля целых единиц — округлённая проекция для тех, кто про минорные не знает.
//
// Округление здесь ровно одно на весь продукт: к ближайшему, половина — от
// нуля. Второго места округления быть не должно, иначе одна и та же сумма
// показывается по-разному в разных каналах.

// ToMinor переводит целые единицы валюты в минорные при шкале exp.
//
// Переполнение НЕ игнорируется: без проверки sum = 184467440737095517 при
// шкале 2 сворачивался в 84, то есть проходил и лимит суммы, и запрет дробей,
// сохраняя 0,84. ok=false означает «столько денег не бывает» и обязан стать
// отказом на входе, а не тихим числом.
func ToMinorChecked(units int, exp int) (int64, bool) {
	factor := int64(MinorFactor(exp))
	v := int64(units)
	if v > maxInt64/factor || v < -maxInt64/factor {
		return 0, false
	}
	return v * factor, true
}

// ToMinor — то же без проверки, для значений, чья величина уже проверена.
func ToMinor(units int, exp int) int64 {
	v, ok := ToMinorChecked(units, exp)
	if !ok {
		return 0
	}
	return v
}

// FromMinor — проекция минорных единиц в целые единицы валюты. ЕДИНСТВЕННОЕ
// место округления денег: к ближайшему, половина — от нуля, чтобы возврат
// и трата одного размера округлялись симметрично.
func FromMinor(minor int64, exp int) int {
	f := int64(MinorFactor(exp))
	if f == 1 {
		return int(minor)
	}
	// Через частное и остаток, а не через minor ± f/2: у краёв int64
	// прибавление половины шага переполняет и меняет знак результата.
	q, r := minor/f, minor%f
	if r >= (f+1)/2 {
		return int(q + 1)
	}
	if -r >= (f+1)/2 {
		return int(q - 1)
	}
	return int(q)
}

// FloatToMinor переводит доставшуюся от бота дробную сумму в минорные единицы.
// Нужен только на чтении старых документов: доли получателей лежат там во
// float64, и «20.8» после умножения даёт 2079.9999… — отсюда округление, а не
// усечение.
func FloatToMinor(sum float64, exp int) int64 {
	return int64(math.Round(sum * float64(MinorFactor(exp))))
}

// SumMinorAt — сумма операции в минорных единицах: записанная, иначе
// выведенная из старого поля. Немигрированный документ отвечает так же, как
// мигрированный, и вызывающему не нужно знать, какой ему достался.
func (o Operation) SumMinorAt(exp int) int64 {
	if o.SumMinor != nil {
		return *o.SumMinor
	}
	return ToMinor(o.Sum, exp)
}

// SumMinorAt — доля получателя в минорных единицах.
func (r RecipientWithSum) SumMinorAt(exp int) int64 {
	if r.SumMinor != nil {
		return *r.SumMinor
	}
	return FloatToMinor(r.Sum, exp)
}

// PriceMinorAt — цена позиции чека в минорных единицах.
func (i OperationItem) PriceMinorAt(exp int) int64 {
	if i.PriceMinor != nil {
		return *i.PriceMinor
	}
	return ToMinor(i.Price, exp)
}

// AmountMinorAt — фиксированная доля позиции в минорных единицах. Второе
// значение false означает, что фиксированной доли нет вовсе: ноль тут
// осмыслен («этот человек за позицию не платит»), и путать его с отсутствием
// нельзя.
func (s ItemShare) AmountMinorAt(exp int) (int64, bool) {
	if s.AmountMinor != nil {
		return *s.AmountMinor, true
	}
	if s.Amount != nil {
		return ToMinor(*s.Amount, exp), true
	}
	return 0, false
}

// FillMoney достраивает у операции оба представления денег: минорные поля по
// старым, если их нет, и старые как проекцию минорных, если минорные есть.
// После вызова любое из полей можно читать напрямую, и они согласованы.
//
// Арифметика на этом этапе прежняя, целыми единицами: задача этой функции —
// только не потерять представление, которого в документе не было.
func FillMoney(o *Operation, exp int) {
	if o == nil {
		return
	}
	if o.SumMinor == nil {
		m := ToMinor(o.Sum, exp)
		o.SumMinor = &m
	} else {
		o.Sum = FromMinor(*o.SumMinor, exp)
	}

	applyShares(o, SharesMinor(o, *o.SumMinor, exp))

	for i := range o.Items {
		it := &o.Items[i]
		if it.PriceMinor == nil {
			m := ToMinor(it.Price, exp)
			it.PriceMinor = &m
		} else {
			it.Price = FromMinor(*it.PriceMinor, exp)
		}
		for j := range it.Shares {
			sh := &it.Shares[j]
			switch {
			case sh.AmountMinor != nil:
				u := FromMinor(*sh.AmountMinor, exp)
				sh.Amount = &u
			case sh.Amount != nil:
				m := ToMinor(*sh.Amount, exp)
				sh.AmountMinor = &m
			}
		}
	}
}

// SharesMinor выводит доли получателей в минорных единицах ВЕКТОРОМ
// относительно итога — а не полем за полем.
//
// ⚠️ Поштучный перевод неверен на живых данных. Старый бот хранит равное
// деление как `float64(total)/n` (`internal/bot/operation_screen.go`), и расход
// 100 на троих лежит там как три доли по 33.333…. Переведи каждую отдельно — и
// сумма долей станет 99 при итоге 100, а после включения копеек разойдётся уже
// на целый рубль.
//
// Правило то же, что у проекции старых полей (`recipientShare` в rest/dto.go),
// только в минорных единицах: при равном делении доли выводятся канонически из
// итога, а хранимые значения игнорируются; при точных суммах хранимые значения
// служат ВЕСАМИ, по которым итог раздаётся целиком. Уже сходящийся набор
// Distribute возвращает без изменений.
func SharesMinor(o *Operation, totalMinor int64, exp int) []int64 {
	if o == nil || len(o.RecipientsWithSum) == 0 {
		return nil
	}
	n := len(o.RecipientsWithSum)

	if !o.IsDebtRepayment && o.SplitType != SplitTypeByExactAmount {
		out := make([]int64, n)
		for i := range out {
			out[i] = ShareOfMinor(totalMinor, n, i)
		}
		return out
	}

	weights := make([]int64, n)
	for i := range o.RecipientsWithSum {
		weights[i] = o.RecipientsWithSum[i].SumMinorAt(exp)
	}
	return Distribute(totalMinor, weights)
}

// applyShares записывает выведенный вектор ТОЛЬКО в минорные поля.
//
// ⚠️ Старые дробные доли на чтении не трогаем. По ним сервер узнаёт легаси-
// комнату, в которой доли не сходятся с суммой, и честно отказывается считать
// долги (`debtsUnavailable`). Перезапиши их согласованными — и предохранитель
// молча выключится, а люди получат долги, посчитанные из догадки.
func applyShares(o *Operation, shares []int64) {
	for i := range shares {
		v := shares[i]
		o.RecipientsWithSum[i].SumMinor = &v
	}
}

// applySharesWithLegacy записывает вектор в ОБА представления. Годится только
// там, где деньги пересчитываются осознанно — при смене шкалы комнаты.
func applySharesWithLegacy(o *Operation, shares []int64, exp int) {
	factor := float64(MinorFactor(exp))
	for i := range shares {
		v := shares[i]
		o.RecipientsWithSum[i].SumMinor = &v
		o.RecipientsWithSum[i].Sum = float64(v) / factor
	}
}

// FillRoomMoney достраивает деньги у всех операций комнаты по её шкале.
func FillRoomMoney(r *Room) {
	if r == nil || r.Operations == nil {
		return
	}
	exp := RoomExponent(r)
	ops := *r.Operations
	for i := range ops {
		FillMoney(&ops[i], exp)
	}
}

// ReconcileMoney сводит деньги операции ПЕРЕД записью.
//
// Отличается от FillMoney направлением, в котором разрешается конфликт. На
// ЧТЕНИИ минорное поле — источник правды: документ записывали согласованно.
// На ЗАПИСИ вызывающий мог поменять СТАРОЕ поле и не тронуть минорное — именно
// так правит бот, который про минорные единицы ничего не знает.
//
// Правило: поля разошлись — правку авторовало СТАРОЕ поле, минорное сбрасываем
// и выводим заново.
//
// ⚠️ Никаких условий на «дробность» минорного здесь быть НЕ ДОЛЖНО. Прежняя
// редакция доверяла старому полю только при минорном, кратном шкале, — и это
// ломалось на настоящих данных: доля 100/3 от бота лежит как 33.333…, её
// минорное 3333 не кратно ста, правка на 34 молча выбрасывалась и возвращалось
// 33.33. Расхождение пары не может означать ничего, кроме правки старого поля:
// REST пишет оба поля согласованными по построению.
func ReconcileMoney(o *Operation, exp int) {
	if o == nil {
		return
	}
	factor := float64(MinorFactor(exp))

	if o.SumMinor != nil && FromMinor(*o.SumMinor, exp) != o.Sum {
		o.SumMinor = nil
	}
	for i := range o.RecipientsWithSum {
		r := &o.RecipientsWithSum[i]
		if r.SumMinor != nil && float64(*r.SumMinor)/factor != r.Sum {
			r.SumMinor = nil
		}
	}
	for i := range o.Items {
		it := &o.Items[i]
		if it.PriceMinor != nil && FromMinor(*it.PriceMinor, exp) != it.Price {
			it.PriceMinor = nil
		}
		for j := range it.Shares {
			sh := &it.Shares[j]
			if sh.AmountMinor == nil || sh.Amount == nil {
				continue
			}
			if FromMinor(*sh.AmountMinor, exp) != *sh.Amount {
				sh.AmountMinor = nil
			}
		}
	}
	FillMoney(o, exp)
}

// shareWeights снимает веса долей в минорных единицах шкалы exp. Пропорции от
// шкалы не зависят, поэтому веса можно снять ДО перевода и раздать по ним уже
// переведённый итог.
func shareWeights(o *Operation, exp int) []int64 {
	out := make([]int64, len(o.RecipientsWithSum))
	for i := range o.RecipientsWithSum {
		out[i] = o.RecipientsWithSum[i].SumMinorAt(exp)
	}
	return out
}

// SharesMinorFrom — то же, что SharesMinor, но с явными весами: при пересчёте
// шкалы веса снимаются в ПРЕЖНЕЙ шкале, а итог уже в новой.
func SharesMinorFrom(o *Operation, totalMinor int64, weights []int64) []int64 {
	n := len(o.RecipientsWithSum)
	if n == 0 {
		return nil
	}
	if !o.IsDebtRepayment && o.SplitType != SplitTypeByExactAmount {
		out := make([]int64, n)
		for i := range out {
			out[i] = ShareOfMinor(totalMinor, n, i)
		}
		return out
	}
	return Distribute(totalMinor, weights)
}

// DeriveSharesMinor сворачивает позиции чека в плоские доли в МИНОРНЫХ
// единицах. Алгоритм деления по позициям целочисленный и от шкалы не зависит,
// поэтому переиспользуется как есть — на минорных значениях.
func DeriveSharesMinor(items []OperationItem, exp int) (map[int]int64, int64, error) {
	minorItems := make([]OperationItem, len(items))
	for i, it := range items {
		mi := it
		mi.Price = int(it.PriceMinorAt(exp))
		shares := make([]ItemShare, len(it.Shares))
		for j, sh := range it.Shares {
			s := sh
			s.Amount = nil
			if amount, ok := sh.AmountMinorAt(exp); ok {
				v := int(amount)
				s.Amount = &v
			}
			shares[j] = s
		}
		mi.Shares = shares
		minorItems[i] = mi
	}
	byUser, total, err := DeriveShares(minorItems)
	if err != nil {
		return nil, 0, err
	}
	out := make(map[int]int64, len(byUser))
	for id, v := range byUser {
		out[id] = int64(v)
	}
	return out, int64(total), nil
}

// sharesFromItems выводит плоские доли ИЗ позиций чека, сохраняя порядок
// получателей операции.
//
// ⚠️ Позиции объявлены источником правды, но раньше пересчитывались отдельно
// от плоских долей — и после смены шкалы два пути давали разные деньги: итог
// 1,50 из трёх позиций по 0,50 давал по позициям A=2, B=0, а по плоским долям
// A=1, B=1. Долг зависел от того, каким путём считать.
func sharesFromItems(o *Operation, exp int) ([]int64, bool) {
	byUser, _, err := DeriveSharesMinor(o.Items, exp)
	if err != nil {
		return nil, false
	}
	out := make([]int64, len(o.RecipientsWithSum))
	for i, r := range o.RecipientsWithSum {
		v, ok := byUser[r.User.ID]
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}
