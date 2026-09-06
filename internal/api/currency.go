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
	if r != nil && r.FractionalAmounts != nil {
		return *r.FractionalAmounts
	}
	return FractionalDefaultFor(RoomCurrency(r))
}

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
