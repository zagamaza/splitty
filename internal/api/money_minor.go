package api

import (
	"errors"
	"math"
)

// Деньги хранятся ВСЕГДА в копейках — одинаково для всех валют и всех тус.
// Хранение не зависит ни от валюты, ни от настроек: один формат на весь
// продукт.
//
// Признак тусы «считаем копейки» отвечает не за хранение, а за две другие
// вещи: что принимается на вводе и с какой точностью делится расход. Поэтому
// переключение этого признака НЕ ТРОГАЕТ НИ ОДНОЙ ЗАПИСИ — ни пересчёта, ни
// округления чужих денег, ни гонок с ним.
//
// Старое поле целых единиц остаётся округлённой проекцией для сборок, которые
// про копейки не знают. Округление одно на весь продукт: к ближайшему,
// половина — от нуля.

// MinorFactor — сколько минорных единиц в единице валюты.
const MinorFactor = 100

// MaxMoneyUnits — продуктовый потолок суммы в целых единицах валюты. Общий для
// всех входов: и REST, и бота.
const MaxMoneyUnits = 1_000_000_000

// ErrMoneyOutOfRange — сумма вне продуктового потолка.
var ErrMoneyOutOfRange = errors.New("money value out of range")

// ToMinorChecked переводит целые единицы валюты в копейки.
//
// ⚠️ Переполнение НЕ игнорируется: без проверки огромное значение сворачивалось
// в маленькое и проходило мимо лимита. ok=false означает «столько денег не
// бывает» и обязано стать отказом на входе, а не тихим числом.
func ToMinorChecked(units int) (int64, bool) {
	if units > MaxMoneyUnits || units < -MaxMoneyUnits {
		return 0, false
	}
	return int64(units) * MinorFactor, true
}

// FromMinor — проекция копеек в целые единицы валюты. ЕДИНСТВЕННОЕ место
// округления денег: к ближайшему, половина — от нуля, чтобы возврат и трата
// одного размера округлялись симметрично.
//
// Через частное и остаток, а не через minor ± половина шага: у краёв int64
// прибавление половины переполняет и меняет знак результата.
func FromMinor(minor int64) int {
	q, r := minor/MinorFactor, minor%MinorFactor
	switch {
	case r >= (MinorFactor+1)/2:
		return int(q + 1)
	case -r >= (MinorFactor+1)/2:
		return int(q - 1)
	default:
		return int(q)
	}
}

// FloatToMinor переводит доставшуюся от бота дробную сумму в копейки.
// Округление, а не усечение: «20.8» после умножения даёт 2079.9999…
func FloatToMinor(sum float64) int64 {
	return int64(math.Round(sum * MinorFactor))
}

// ValidateMoneyRange проверяет, что все деньги операции помещаются в
// продуктовый потолок.
func ValidateMoneyRange(o *Operation) error {
	if o == nil {
		return nil
	}
	if _, ok := ToMinorChecked(o.Sum); !ok {
		return ErrMoneyOutOfRange
	}
	for _, r := range o.RecipientsWithSum {
		if r.Sum > float64(MaxMoneyUnits) || r.Sum < -float64(MaxMoneyUnits) {
			return ErrMoneyOutOfRange
		}
	}
	for _, it := range o.Items {
		if _, ok := ToMinorChecked(it.Price); !ok {
			return ErrMoneyOutOfRange
		}
		for _, sh := range it.Shares {
			if sh.Amount == nil {
				continue
			}
			if _, ok := ToMinorChecked(*sh.Amount); !ok {
				return ErrMoneyOutOfRange
			}
		}
	}
	return nil
}

// SumMinorOrLegacy — сумма операции в копейках: записанная, иначе выведенная из
// старого поля. Немигрированный документ отвечает так же, как мигрированный.
func (o Operation) SumMinorOrLegacy() int64 {
	if o.SumMinor != nil {
		return *o.SumMinor
	}
	v, _ := ToMinorChecked(o.Sum)
	return v
}

// SumMinorOrLegacy — доля получателя в копейках.
func (r RecipientWithSum) SumMinorOrLegacy() int64 {
	if r.SumMinor != nil {
		return *r.SumMinor
	}
	return FloatToMinor(r.Sum)
}

// PriceMinorOrLegacy — цена позиции чека в копейках.
func (i OperationItem) PriceMinorOrLegacy() int64 {
	if i.PriceMinor != nil {
		return *i.PriceMinor
	}
	v, _ := ToMinorChecked(i.Price)
	return v
}

// AmountMinorOrLegacy — фиксированная доля позиции в копейках. Второе значение
// false означает, что фиксированной доли нет вовсе: ноль тут осмыслен («этот
// человек за позицию не платит»), и путать его с отсутствием нельзя.
func (s ItemShare) AmountMinorOrLegacy() (int64, bool) {
	if s.AmountMinor != nil {
		return *s.AmountMinor, true
	}
	if s.Amount != nil {
		v, ok := ToMinorChecked(*s.Amount)
		return v, ok
	}
	return 0, false
}

// shareStep — шаг, с которым делится расход в этой тусе. Без копеек доли обязаны
// быть кратны единице валюты: иначе человеку показывают долю, которую он не
// может ни заплатить, ни увидеть — а сумма долей на экране расходится с итогом.
func shareStep(fractional bool) int64 {
	if fractional {
		return 1
	}
	return MinorFactor
}

// SharesMinor выводит доли получателей в копейках ВЕКТОРОМ относительно итога —
// а не полем за полем.
//
// ⚠️ Поштучный перевод неверен на живых данных: старый бот хранит равное
// деление как float64(total)/n, и расход 100 на троих лежит там как три доли по
// 33.333…. Переведи каждую отдельно — сумма долей разойдётся с итогом.
//
// Правило то же, что у проекции старых полей: при равном делении доли выводятся
// канонически из итога, при точных суммах хранимые значения служат ВЕСАМИ.
func SharesMinor(o *Operation, totalMinor int64, fractional bool) ([]int64, bool) {
	if o == nil || len(o.RecipientsWithSum) == 0 {
		return nil, true
	}
	n := len(o.RecipientsWithSum)

	if !o.IsDebtRepayment && o.SplitType != SplitTypeByExactAmount {
		out := make([]int64, n)
		step := shareStep(fractional)
		for i := range out {
			out[i] = ShareOfMinorStep(totalMinor, n, i, step)
		}
		return out, true
	}

	weights := make([]int64, n)
	for i := range o.RecipientsWithSum {
		weights[i] = o.RecipientsWithSum[i].SumMinorOrLegacy()
	}
	return Distribute(totalMinor, weights)
}

// sharesAreConsistent — у каждой доли есть записанное значение в копейках, и
// вместе они в точности дают итог. Такой вектор пересобирать не за чем.
func sharesAreConsistent(o *Operation, totalMinor int64) bool {
	if len(o.RecipientsWithSum) == 0 {
		return true
	}
	var sum int64
	for _, r := range o.RecipientsWithSum {
		if r.SumMinor == nil {
			return false
		}
		sum += *r.SumMinor
	}
	return sum == totalMinor
}

// applyShares записывает выведенный вектор ТОЛЬКО в минорные поля.
//
// ⚠️ Старые дробные доли на чтении не трогаем. По ним сервер узнаёт легаси-тусу,
// в которой доли не сходятся с суммой, и честно отказывается считать долги
// (debtsUnavailable). Перезапиши их согласованными — предохранитель молча
// выключится, а люди получат долги, посчитанные из догадки.
func applyShares(o *Operation, shares []int64) {
	for i := range shares {
		v := shares[i]
		o.RecipientsWithSum[i].SumMinor = &v
	}
}

// FillMoney достраивает у операции оба представления денег: копейки по старым
// полям, если их нет, и старые поля как проекцию копеек, если копейки есть.
func FillMoney(o *Operation, fractional bool) {
	if o == nil {
		return
	}
	if o.SumMinor == nil {
		// Не помещается — поле остаётся ПУСТЫМ. Ноль соврал бы, что расход без
		// денег; отсутствие честно говорит «точного значения нет».
		if m, ok := ToMinorChecked(o.Sum); ok {
			o.SumMinor = &m
		}
	} else {
		o.Sum = FromMinor(*o.SumMinor)
	}

	// ⚠️ Уже записанный и СОГЛАСОВАННЫЙ вектор долей не трогаем. Он записан
	// человеком (или выведен при записи) и является обязательством конкретных
	// людей друг перед другом; пересобрать его по текущей настройке значило бы
	// задним числом двигать чьи-то долги — расход 100 на троих превращался из
	// 33,34 + 33,33 + 33,33 в 34 + 33 + 33 от одного тумблера. Интерфейс прямо
	// обещает обратное: «уже записанное не изменится».
	//
	// Выводим вектор только когда его нет или он не сходится с итогом — то есть
	// на легаси-данных бота, где долей в копейках не было вовсе.
	if o.SumMinor != nil && !sharesAreConsistent(o, *o.SumMinor) {
		if shares, ok := SharesMinor(o, *o.SumMinor, fractional); ok {
			applyShares(o, shares)
		}
	}

	for i := range o.Items {
		it := &o.Items[i]
		if it.PriceMinor == nil {
			if m, ok := ToMinorChecked(it.Price); ok {
				it.PriceMinor = &m
			}
		} else {
			it.Price = FromMinor(*it.PriceMinor)
		}
		for j := range it.Shares {
			sh := &it.Shares[j]
			switch {
			case sh.AmountMinor != nil:
				u := FromMinor(*sh.AmountMinor)
				sh.Amount = &u
			case sh.Amount != nil:
				if m, ok := ToMinorChecked(*sh.Amount); ok {
					sh.AmountMinor = &m
				}
			}
		}
	}
}

// FillRoomMoney достраивает деньги у всех операций тусы.
func FillRoomMoney(r *Room) {
	if r == nil || r.Operations == nil {
		return
	}
	fractional := RoomFractional(r)
	ops := *r.Operations
	for i := range ops {
		FillMoney(&ops[i], fractional)
	}
}

// ReconcileMoney сводит деньги операции ПЕРЕД записью.
//
// Отличается от FillMoney направлением, в котором разрешается конфликт. На
// ЧТЕНИИ минорное поле — источник правды: документ записывали согласованно. На
// ЗАПИСИ вызывающий мог поменять СТАРОЕ поле и не тронуть минорное — именно так
// правит бот, который про копейки ничего не знает.
//
// Правило: поля разошлись — правку авторовало СТАРОЕ поле, минорное сбрасываем
// и выводим заново.
//
// ⚠️ Никаких условий на «дробность» минорного здесь быть НЕ ДОЛЖНО. Прежняя
// редакция доверяла старому полю только при минорном, кратном ста, и ломалась
// ровно на данных бота: доля 100/3 лежит как 33.333…, её минорное 3333 не
// кратно ста, и правка на 34 молча выбрасывалась.
func ReconcileMoney(o *Operation, fractional bool) {
	if o == nil {
		return
	}
	if o.SumMinor != nil && FromMinor(*o.SumMinor) != o.Sum {
		o.SumMinor = nil
	}
	for i := range o.RecipientsWithSum {
		r := &o.RecipientsWithSum[i]
		if r.SumMinor != nil && float64(*r.SumMinor)/MinorFactor != r.Sum {
			r.SumMinor = nil
		}
	}
	for i := range o.Items {
		it := &o.Items[i]
		if it.PriceMinor != nil && FromMinor(*it.PriceMinor) != it.Price {
			it.PriceMinor = nil
		}
		for j := range it.Shares {
			sh := &it.Shares[j]
			if sh.AmountMinor == nil || sh.Amount == nil {
				continue
			}
			if FromMinor(*sh.AmountMinor) != *sh.Amount {
				sh.AmountMinor = nil
			}
		}
	}
	FillMoney(o, fractional)
}
