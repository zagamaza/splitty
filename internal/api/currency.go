package api

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultCurrency валюта комнат, у которых валюта не выбрана
// (пустая строка в базе): исторический дефолт бота — рубль
const DefaultCurrency = "RUB"

// Currencies справочник поддерживаемых валют комнат — единый для бота и REST
var Currencies = map[string]CurrencyInfo{
	"RUB": {Code: "RUB", Symbol: "₽", Flag: "🇷🇺", FractionalDefault: false, SupportsFraction: true},
	"USD": {Code: "USD", Symbol: "$", Flag: "🇺🇸", FractionalDefault: true, SupportsFraction: true},
	"EUR": {Code: "EUR", Symbol: "€", Flag: "🇪🇺", FractionalDefault: true, SupportsFraction: true},
	"IDR": {Code: "IDR", Symbol: "Rp", Flag: "🇮🇩", FractionalDefault: false, SupportsFraction: true},
	"KZT": {Code: "KZT", Symbol: "₸", Flag: "🇰🇿", FractionalDefault: false, SupportsFraction: true},
	"UZS": {Code: "UZS", Symbol: "сум", Flag: "🇺🇿", FractionalDefault: false, SupportsFraction: true},
	// Валюты рынков, на языки которых приложение переведено. Без них комната
	// в Токио считалась в долларах, а «410 JPY» на витрине выглядело браком:
	// незнакомый код показывается как есть.
	//
	// У иены и воны SupportsFraction ложный: минорной единицы не существует в
	// обороте, и переключатель копеек для таких тус не показывается вовсе.
	"JPY": {Code: "JPY", Symbol: "¥", Flag: "🇯🇵", FractionalDefault: false, SupportsFraction: false},
	"CNY": {Code: "CNY", Symbol: "¥", Flag: "🇨🇳", FractionalDefault: true, SupportsFraction: true},
	"KRW": {Code: "KRW", Symbol: "₩", Flag: "🇰🇷", FractionalDefault: false, SupportsFraction: false},
	"BRL": {Code: "BRL", Symbol: "R$", Flag: "🇧🇷", FractionalDefault: true, SupportsFraction: true},
}

// CurrencyCodes стабильный порядок выдачи справочника валют
// (map не гарантирует порядок итерации)
var CurrencyCodes = []string{"RUB", "USD", "EUR", "JPY", "CNY", "KRW", "BRL", "IDR", "KZT", "UZS"}

// IsSupportedCurrency проверяет, что код валюты есть в справочнике
func IsSupportedCurrency(code string) bool {
	_, ok := Currencies[code]
	return ok
}

// MoneyWithSymbol форматирует целые единицы валюты с разделением тысяч узким
// пробелом и символом валюты: 1200, "RUB" → "1 200 ₽".
//
// Живёт здесь, а не в боте: тексты пушей собирает и джоб напоминаний, а
// одинаковые суммы обязаны выглядеть одинаково во всех каналах.
func MoneyWithSymbol(sum int, currency string) string {
	digits := strconv.Itoa(sum)
	negative := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")

	var grouped strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteRune(' ')
		}
		grouped.WriteRune(r)
	}

	info, ok := Currencies[currency]
	if !ok {
		info = Currencies[DefaultCurrency]
	}
	if negative {
		return "-" + grouped.String() + " " + info.Symbol
	}
	return grouped.String() + " " + info.Symbol
}

// MoneyWithSymbolMinor форматирует ТОЧНУЮ сумму в минорных единицах: 208000,
// "RUB" → "2 080 ₽", 2080 → "20,80 ₽". Дробная часть печатается, только когда
// она есть: у рублёвой поездки «1 200,00 ₽» — визуальный шум.
//
// Разделитель — запятая: тексты пушей собираются на сервере и уходят как есть,
// локали получателя здесь нет.
func MoneyWithSymbolMinor(minor int64, currency string) string {
	whole := MoneyWithSymbol(FromMinor(minor-minor%MinorFactor), currency)
	rest := minor % MinorFactor
	if rest == 0 {
		return whole
	}
	if rest < 0 {
		rest = -rest
	}
	info, ok := Currencies[currency]
	if !ok {
		info = Currencies[DefaultCurrency]
	}
	// Символ валюты стоит после суммы — дробную часть вставляем перед ним.
	return strings.TrimSuffix(whole, " "+info.Symbol) +
		fmt.Sprintf(",%02d", rest) + " " + info.Symbol
}

// FractionalDefaultFor — считает ли НОВАЯ туса в этой валюте копейки.
func FractionalDefaultFor(code string) bool {
	info, ok := Currencies[code]
	if !ok {
		return Currencies[DefaultCurrency].FractionalDefault
	}
	return info.FractionalDefault
}

// SupportsFraction — есть ли у валюты дробная часть в обороте. Ложь означает,
// что переключателя копеек в такой тусе нет вовсе: показывать выбор, которого
// не существует, значит врать.
func SupportsFraction(code string) bool {
	info, ok := Currencies[code]
	if !ok {
		return Currencies[DefaultCurrency].SupportsFraction
	}
	return info.SupportsFraction
}

// RoomCurrency код валюты тусы: пустая строка в базе означает исторический
// дефолт бота.
func RoomCurrency(r *Room) string {
	if r == nil || r.Currency == "" {
		return DefaultCurrency
	}
	return r.Currency
}

// RoomFractional — считает ли туса копейки. Признак хранится в документе; если
// его нет (туса заведена до появления настройки), берётся умолчание валюты.
//
// ⚠️ Это НЕ про хранение. Деньги всегда лежат в копейках; признак решает, что
// принимать на вводе и с какой точностью делить расход.
func RoomFractional(r *Room) bool {
	// ⚠️ Серверный признак — рубильник над настройкой тусы. Пока он выключен,
	// копеек нет НИГДЕ, даже у валют, где они включены умолчанием.
	//
	// Без этого выкатка бэкенда сама по себе включала бы копейки в евровых и
	// долларовых тусах: долг из записанных долей 20,50 € оставался бы дробным,
	// старая сборка показывала бы его как 21 € и не смогла бы погасить — сервер
	// ответил бы «сумма превышает текущий долг». Так рубильник и клиенты
	// перестают зависеть от порядка выкатки.
	if !fractionalInput.Load() {
		return false
	}
	if r != nil && r.FractionalAmounts != nil {
		return *r.FractionalAmounts
	}
	// ⚠️ Умолчание валюты действует ТОЛЬКО на тусы, заведённые после подъёма
	// рубильника. У существующих настройка проставляется явно один раз перед
	// включением (см. FractionalBackfilledAt): иначе подъём рубильника разом
	// сделал бы дробными все долларовые и евровые тусы, а у людей на руках
	// осталась бы старая сборка, которая точный долг ни показать, ни погасить
	// не может. Выкатка обязана быть управляемой, а не «одним щелчком на всех».
	if r != nil && r.CreateAt.Before(fractionalEnabledAt.Load().(time.Time)) {
		return false
	}
	return FractionalDefaultFor(RoomCurrency(r))
}

// fractionalEnabledAt — момент подъёма рубильника. Тусы, заведённые раньше,
// умолчание валюты не подхватывают: их настройку проставляют явно.
var fractionalEnabledAt atomic.Value

func init() { fractionalEnabledAt.Store(time.Time{}) }

// SetFractionalEnabledAt задаёт этот момент; нулевое время означает «умолчание
// валюты действует для всех», то есть поведение до появления барьера.
func SetFractionalEnabledAt(t time.Time) { fractionalEnabledAt.Store(t) }

// fractionalInput — серверный признак дробного ввода (FRACTIONAL_INPUT).
// Процесс-глобальный намеренно: это один рубильник на весь сервер, и таскать
// его параметром сквозь расчёт долгов, напоминания и бота значило бы протянуть
// конфиг в слои, которые о нём знать не должны.
var fractionalInput atomic.Bool

// SetFractionalInput выставляет серверный признак; зовётся один раз на старте.
func SetFractionalInput(on bool) { fractionalInput.Store(on) }

// FractionalInputEnabled — включён ли дробный ввод на сервере.
func FractionalInputEnabled() bool { return fractionalInput.Load() }

// FractionCurrencyCodes — коды валют, у которых есть дробная часть. Нужен
// условной записи признака копеек: включать его можно только у такой валюты, и
// проверять это надо ТЕМ ЖЕ запросом, что и пишет, иначе конкурентная смена
// валюты оставит иену с включёнными копейками.
func FractionCurrencyCodes() []string {
	out := make([]string, 0, len(CurrencyCodes))
	for _, code := range CurrencyCodes {
		if Currencies[code].SupportsFraction {
			out = append(out, code)
		}
	}
	return out
}
