package api

import "testing"

// Справочник и порядок выдачи — два разных списка, и разойтись им нельзя:
// код, попавший только в CurrencyCodes, приезжает клиенту пустой записью, а
// код только в Currencies выбрать в пикере невозможно.
func TestCurrencyCodesMatchDirectory(t *testing.T) {
	if len(CurrencyCodes) != len(Currencies) {
		t.Fatalf("в порядке %d кодов, в справочнике %d", len(CurrencyCodes), len(Currencies))
	}
	seen := make(map[string]bool, len(CurrencyCodes))
	for _, code := range CurrencyCodes {
		if seen[code] {
			t.Errorf("код %q в порядке дважды", code)
		}
		seen[code] = true
		info, ok := Currencies[code]
		if !ok {
			t.Errorf("код %q есть в порядке, но не в справочнике", code)
			continue
		}
		if info.Code != code {
			t.Errorf("%q: Code = %q", code, info.Code)
		}
		if info.Symbol == "" || info.Flag == "" {
			t.Errorf("%q: пустой символ (%q) или флаг (%q)", code, info.Symbol, info.Flag)
		}
	}
}

// Валюты рынков, на языки которых переведено приложение. Без них комната в
// Токио считается в долларах: незнакомый код не отвергается, он просто
// показывается как есть.
func TestLocalizedMarketsHaveTheirCurrency(t *testing.T) {
	want := map[string]string{"JPY": "¥", "CNY": "¥", "KRW": "₩", "BRL": "R$"}
	for code, symbol := range want {
		if !IsSupportedCurrency(code) {
			t.Errorf("%s не поддерживается", code)
			continue
		}
		if got := Currencies[code].Symbol; got != symbol {
			t.Errorf("%s: символ %q, ожидался %q", code, got, symbol)
		}
	}
	// Суммы целые во всех валютах, разделитель тысяч узкий пробел.
	if got := MoneyWithSymbol(410000, "JPY"); got != "410 000 ¥" {
		t.Errorf("MoneyWithSymbol(410000, JPY) = %q", got)
	}
	if got := MoneyWithSymbol(-1200, "KRW"); got != "-1 200 ₩" {
		t.Errorf("MoneyWithSymbol(-1200, KRW) = %q", got)
	}
}

// MoneyWithSymbolMinor печатает дробь ТОЛЬКО когда она есть.
//
// Тексты пушей и напоминаний собираются здесь, и округление в них означало бы,
// что человеку обещают вернуть не ту сумму, которую он видит в приложении.
func TestMoneyWithSymbolMinor(t *testing.T) {
	cases := []struct {
		minor    int64
		currency string
		want     string
	}{
		{208000, "RUB", "2 080 ₽"},
		{2080, "RUB", "20,80 ₽"},
		{2008, "RUB", "20,08 ₽"},
		{25, "USD", "0,25 $"},
		{-2080, "RUB", "-20,80 ₽"},
		{410000, "JPY", "4 100 ¥"},
	}
	for _, c := range cases {
		if got := MoneyWithSymbolMinor(c.minor, c.currency); got != c.want {
			t.Errorf("MoneyWithSymbolMinor(%d, %s) = %q, want %q", c.minor, c.currency, got, c.want)
		}
	}
}
