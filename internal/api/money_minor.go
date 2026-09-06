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
func ToMinor(units int, exp int) int64 {
	return int64(units) * int64(MinorFactor(exp))
}

// FromMinor — проекция минорных единиц в целые единицы валюты. ЕДИНСТВЕННОЕ
// место округления денег: к ближайшему, половина — от нуля, чтобы возврат
// и трата одного размера округлялись симметрично.
func FromMinor(minor int64, exp int) int {
	f := int64(MinorFactor(exp))
	if f == 1 {
		return int(minor)
	}
	if minor >= 0 {
		return int((minor + f/2) / f)
	}
	return int((minor - f/2) / f)
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

	for i := range o.RecipientsWithSum {
		r := &o.RecipientsWithSum[i]
		if r.SumMinor == nil {
			m := FloatToMinor(r.Sum, exp)
			r.SumMinor = &m
		} else {
			r.Sum = float64(*r.SumMinor) / float64(MinorFactor(exp))
		}
	}

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
// так правит бот, который про минорные единицы ничего не знает. Взять там
// минорное значило бы молча вернуть прежнюю сумму и потерять правку человека.
//
// Правило: поля разошлись — верим СТАРОМУ и пересобираем минорное из него.
// Исключение одно: минорное ДРОБНОЕ, и старым полем его не выразить —
// тогда правку принимать нельзя, и минорное остаётся как есть.
//
// ⚠️ Второй случай сегодня недостижим: дробные значения появляются только с
// включённым признаком дробного ввода, а до него старым клиентам правку
// дробной операции запрещает сам сервер. Когда бот узнает про дроби
// (Задача 14), этот случай обязан стать явным отказом, а не тихим выбором.
func ReconcileMoney(o *Operation, exp int) {
	if o == nil {
		return
	}
	factor := int64(MinorFactor(exp))

	if o.SumMinor != nil && *o.SumMinor%factor == 0 && FromMinor(*o.SumMinor, exp) != o.Sum {
		o.SumMinor = nil
	}
	for i := range o.RecipientsWithSum {
		r := &o.RecipientsWithSum[i]
		if r.SumMinor != nil && *r.SumMinor%factor == 0 && float64(*r.SumMinor)/float64(factor) != r.Sum {
			r.SumMinor = nil
		}
	}
	for i := range o.Items {
		it := &o.Items[i]
		if it.PriceMinor != nil && *it.PriceMinor%factor == 0 && FromMinor(*it.PriceMinor, exp) != it.Price {
			it.PriceMinor = nil
		}
		for j := range it.Shares {
			sh := &it.Shares[j]
			if sh.AmountMinor == nil || sh.Amount == nil {
				continue
			}
			if *sh.AmountMinor%factor == 0 && FromMinor(*sh.AmountMinor, exp) != *sh.Amount {
				sh.AmountMinor = nil
			}
		}
	}
	FillMoney(o, exp)
}
