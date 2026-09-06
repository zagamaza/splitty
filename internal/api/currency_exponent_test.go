package api

import "testing"

// wantExponents — принятая таблица шкал. Расхождение с ISO 4217 осознанное:
// у рубля по ISO двойка, но комнаты заводятся без копеек, а у рупии, сума и
// тенге минорной единицы нет в обороте. Тест обязан падать, если кто-то
// «поправит» справочник обратно по ISO.
var wantExponents = map[string]struct{ display, max int }{
	"RUB": {0, 2},
	"USD": {2, 2},
	"EUR": {2, 2},
	"CNY": {2, 2},
	"BRL": {2, 2},
	"KZT": {0, 2},
	"UZS": {0, 2},
	"IDR": {0, 2},
	"JPY": {0, 0},
	"KRW": {0, 0},
}

func TestCurrencyExponentsMatchAcceptedTable(t *testing.T) {
	if len(Currencies) != len(wantExponents) {
		t.Fatalf("валют в справочнике %d, в таблице шкал %d — таблицу забыли обновить", len(Currencies), len(wantExponents))
	}
	for code, info := range Currencies {
		want, ok := wantExponents[code]
		if !ok {
			t.Errorf("%s: валюта есть в справочнике, но шкала для неё не решена", code)
			continue
		}
		if info.DisplayExponent != want.display {
			t.Errorf("%s: DisplayExponent = %d, want %d", code, info.DisplayExponent, want.display)
		}
		if info.MaxExponent != want.max {
			t.Errorf("%s: MaxExponent = %d, want %d", code, info.MaxExponent, want.max)
		}
		if info.DisplayExponent > info.MaxExponent {
			t.Errorf("%s: умолчание %d выше предела %d", code, info.DisplayExponent, info.MaxExponent)
		}
	}
}

// У иены и воны минорной единицы не существует: переключателя копеек в такой
// комнате быть не должно вовсе.
func TestCurrenciesWithoutMinorUnit(t *testing.T) {
	for _, code := range []string{"JPY", "KRW"} {
		if MaxExponentFor(code) != 0 {
			t.Errorf("%s: MaxExponentFor = %d, want 0", code, MaxExponentFor(code))
		}
		if IsValidExponent(code, 2) {
			t.Errorf("%s: шкала 2 признана допустимой", code)
		}
	}
}

func TestIsValidExponent(t *testing.T) {
	for _, tc := range []struct {
		code string
		exp  int
		want bool
	}{
		{"RUB", 0, true}, {"RUB", 2, true}, {"RUB", 3, false}, {"RUB", -1, false},
		{"JPY", 0, true}, {"JPY", 1, false},
		// незнакомый код ведёт себя как валюта по умолчанию, а не разрешает всё
		{"GBP", 2, true}, {"GBP", 3, false},
	} {
		if got := IsValidExponent(tc.code, tc.exp); got != tc.want {
			t.Errorf("IsValidExponent(%q, %d) = %v, want %v", tc.code, tc.exp, got, tc.want)
		}
	}
}

func TestMinorFactor(t *testing.T) {
	for _, tc := range []struct{ exp, want int }{{0, 1}, {1, 10}, {2, 100}, {3, 1000}} {
		if got := MinorFactor(tc.exp); got != tc.want {
			t.Errorf("MinorFactor(%d) = %d, want %d", tc.exp, got, tc.want)
		}
	}
}

func TestRoomExponent(t *testing.T) {
	two, zero := 2, 0

	// явно записанная шкала сильнее умолчания валюты
	if got := RoomExponent(&Room{Currency: "RUB", DisplayExponent: &two}); got != 2 {
		t.Errorf("комната с явной шкалой 2: got %d, want 2", got)
	}
	// осознанный ноль в долларовой комнате не должен подменяться умолчанием 2
	if got := RoomExponent(&Room{Currency: "USD", DisplayExponent: &zero}); got != 0 {
		t.Errorf("комната с явной шкалой 0: got %d, want 0", got)
	}
	// поля нет — берём умолчание валюты
	if got := RoomExponent(&Room{Currency: "USD"}); got != 2 {
		t.Errorf("долларовая комната без поля: got %d, want 2", got)
	}
	if got := RoomExponent(&Room{Currency: "RUB"}); got != 0 {
		t.Errorf("рублёвая комната без поля: got %d, want 0", got)
	}
	// пустая валюта — исторический дефолт бота
	if got := RoomExponent(&Room{}); got != 0 {
		t.Errorf("комната без валюты: got %d, want 0", got)
	}
	if got := RoomExponent(nil); got != 0 {
		t.Errorf("nil: got %d, want 0", got)
	}
}

func TestDefaultExponentForUnknownCurrency(t *testing.T) {
	if got := DefaultExponentFor("GBP"); got != DefaultExponentFor(DefaultCurrency) {
		t.Errorf("незнакомый код: got %d, want как у %s", got, DefaultCurrency)
	}
}

// Смена валюты не пересчитывает суммы — она безопасна ровно пока новая валюта
// допускает шкалу комнаты.
func TestScaleAfterCurrencyChange(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exp       int
		hasOps    bool
		currency  string
		wantExp   int
		wantAllow bool
	}{
		{"рубль без копеек в иены — можно, число то же", 0, true, "JPY", 0, true},
		{"доллар с копейками в евро — можно, шкала общая", 2, true, "EUR", 2, true},
		{"доллар с копейками в иены — нельзя: 2080 стало бы 2080 иен", 2, true, "JPY", 2, false},
		{"та же пара, но комната пустая — можно, терять нечего", 2, false, "JPY", 0, true},
		{"рубль без копеек в доллар — шкала не поднимается сама", 0, true, "USD", 0, true},
		{"незнакомый код ведёт себя как валюта по умолчанию", 2, true, "GBP", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotExp, gotAllow := ScaleAfterCurrencyChange(tc.exp, tc.hasOps, tc.currency)
			if gotAllow != tc.wantAllow {
				t.Fatalf("разрешено = %v, want %v", gotAllow, tc.wantAllow)
			}
			if gotExp != tc.wantExp {
				t.Errorf("шкала = %d, want %d", gotExp, tc.wantExp)
			}
		})
	}
}
