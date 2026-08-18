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
	"RUB": {Code: "RUB", Symbol: "₽", Flag: "🇷🇺"},
	"USD": {Code: "USD", Symbol: "$", Flag: "🇺🇸"},
	"EUR": {Code: "EUR", Symbol: "€", Flag: "🇪🇺"},
	"IDR": {Code: "IDR", Symbol: "Rp", Flag: "🇮🇩"},
	"KZT": {Code: "KZT", Symbol: "₸", Flag: "🇰🇿"},
	"UZS": {Code: "UZS", Symbol: "сум", Flag: "🇺🇿"},
}

// CurrencyCodes стабильный порядок выдачи справочника валют
// (map не гарантирует порядок итерации)
var CurrencyCodes = []string{"RUB", "USD", "EUR", "IDR", "KZT", "UZS"}

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
