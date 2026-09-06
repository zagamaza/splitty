package api

import (
	"strconv"
	"strings"
)

// DefaultCurrency валюта комнат, у которых валюта не выбрана
// (пустая строка в базе): исторический дефолт бота — рубль
const DefaultCurrency = "RUB"

// Currencies справочник поддерживаемых валют комнат — единый для бота и REST
var Currencies = map[string]CurrencyInfo{
	"RUB": {Code: "RUB", Symbol: "₽", Flag: "🇷🇺", DisplayExponent: 0, MaxExponent: 2},
	"USD": {Code: "USD", Symbol: "$", Flag: "🇺🇸", DisplayExponent: 2, MaxExponent: 2},
	"EUR": {Code: "EUR", Symbol: "€", Flag: "🇪🇺", DisplayExponent: 2, MaxExponent: 2},
	"IDR": {Code: "IDR", Symbol: "Rp", Flag: "🇮🇩", DisplayExponent: 0, MaxExponent: 2},
	"KZT": {Code: "KZT", Symbol: "₸", Flag: "🇰🇿", DisplayExponent: 0, MaxExponent: 2},
	"UZS": {Code: "UZS", Symbol: "сум", Flag: "🇺🇿", DisplayExponent: 0, MaxExponent: 2},
	// Валюты рынков, на языки которых приложение переведено. Без них комната
	// в Токио считалась в долларах, а «410 JPY» на витрине выглядело браком:
	// незнакомый код показывается как есть.
	//
	// У иены и воны MaxExponent нулевой: минорной единицы не существует в
	// обороте, и переключатель копеек для таких комнат не показывается вовсе.
	"JPY": {Code: "JPY", Symbol: "¥", Flag: "🇯🇵", DisplayExponent: 0, MaxExponent: 0},
	"CNY": {Code: "CNY", Symbol: "¥", Flag: "🇨🇳", DisplayExponent: 2, MaxExponent: 2},
	"KRW": {Code: "KRW", Symbol: "₩", Flag: "🇰🇷", DisplayExponent: 0, MaxExponent: 0},
	"BRL": {Code: "BRL", Symbol: "R$", Flag: "🇧🇷", DisplayExponent: 2, MaxExponent: 2},
}

// CurrencyCodes стабильный порядок выдачи справочника валют
// (map не гарантирует порядок итерации)
var CurrencyCodes = []string{"RUB", "USD", "EUR", "JPY", "CNY", "KRW", "BRL", "IDR", "KZT", "UZS"}

// IsSupportedCurrency проверяет, что код валюты есть в справочнике
func IsSupportedCurrency(code string) bool {
	_, ok := Currencies[code]
	return ok
}

// DefaultExponentFor умолчание шкалы для НОВОЙ комнаты в этой валюте.
// Шкала — свойство комнаты, а не валюты: валюта лишь подсказывает значение,
// с которого комната начинает жить, дальше человек меняет его в настройках.
func DefaultExponentFor(code string) int {
	info, ok := Currencies[code]
	if !ok {
		return Currencies[DefaultCurrency].DisplayExponent
	}
	return info.DisplayExponent
}

// MaxExponentFor наибольшая шкала, допустимая в этой валюте. Ноль означает,
// что дробной части у валюты нет в обороте (иена, вона) и переключать нечего.
func MaxExponentFor(code string) int {
	info, ok := Currencies[code]
	if !ok {
		return Currencies[DefaultCurrency].MaxExponent
	}
	return info.MaxExponent
}

// IsValidExponent проверяет, что шкала допустима для валюты: от нуля до
// MaxExponent включительно.
func IsValidExponent(code string, exp int) bool {
	return exp >= 0 && exp <= MaxExponentFor(code)
}

// RoomCurrency код валюты комнаты: пустая строка в базе означает исторический
// дефолт бота.
func RoomCurrency(r *Room) string {
	if r == nil || r.Currency == "" {
		return DefaultCurrency
	}
	return r.Currency
}

// RoomExponent шкала комнаты: явно заданная в документе, иначе умолчание её
// валюты. Единственный способ узнать шкалу — читать поле напрямую нельзя:
// у комнат, заведённых до появления поля, его в документе нет.
func RoomExponent(r *Room) int {
	if r != nil && r.DisplayExponent != nil {
		return *r.DisplayExponent
	}
	return DefaultExponentFor(RoomCurrency(r))
}

// MinorFactor множитель перевода единиц валюты в минорные для шкалы exp:
// 0 → 1, 2 → 100.
func MinorFactor(exp int) int {
	f := 1
	for i := 0; i < exp; i++ {
		f *= 10
	}
	return f
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
