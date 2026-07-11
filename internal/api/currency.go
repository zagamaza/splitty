package api

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
