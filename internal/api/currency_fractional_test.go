package api

import "testing"

// wantFractional — принятая таблица: считает ли НОВАЯ туса копейки и есть ли у
// валюты дробная часть вообще.
//
// Расхождение с ISO 4217 осознанное: у рубля по ISO две цифры, но тусы
// заводятся без копеек. Тест обязан падать, если кто-то «поправит» справочник
// обратно по ISO.
var wantFractional = map[string]struct{ def, supports bool }{
	"RUB": {false, true},
	"USD": {true, true},
	"EUR": {true, true},
	"CNY": {true, true},
	"BRL": {true, true},
	"KZT": {false, true},
	"UZS": {false, true},
	"IDR": {false, true},
	"JPY": {false, false},
	"KRW": {false, false},
}

func TestCurrencyFractionalMatchesAcceptedTable(t *testing.T) {
	if len(Currencies) != len(wantFractional) {
		t.Fatalf("валют в справочнике %d, в таблице %d — таблицу забыли обновить", len(Currencies), len(wantFractional))
	}
	for code, info := range Currencies {
		want, ok := wantFractional[code]
		if !ok {
			t.Errorf("%s: валюта есть в справочнике, но решения по копейкам для неё нет", code)
			continue
		}
		if info.FractionalDefault != want.def {
			t.Errorf("%s: FractionalDefault = %v, want %v", code, info.FractionalDefault, want.def)
		}
		if info.SupportsFraction != want.supports {
			t.Errorf("%s: SupportsFraction = %v, want %v", code, info.SupportsFraction, want.supports)
		}
		if info.FractionalDefault && !info.SupportsFraction {
			t.Errorf("%s: по умолчанию с копейками, которых у валюты нет", code)
		}
	}
}

// У иены и воны минорной единицы не существует: переключателя в такой тусе быть
// не должно вовсе.
func TestCurrenciesWithoutMinorUnit(t *testing.T) {
	for _, code := range []string{"JPY", "KRW"} {
		if SupportsFraction(code) {
			t.Errorf("%s: SupportsFraction = true", code)
		}
	}
}

func TestRoomFractional(t *testing.T) {
	yes, no := true, false

	// Явная настройка сильнее умолчания валюты — в обе стороны
	if !RoomFractional(&Room{Currency: "RUB", FractionalAmounts: &yes}) {
		t.Error("рублёвая туса с явно включёнными копейками читается как без них")
	}
	if RoomFractional(&Room{Currency: "USD", FractionalAmounts: &no}) {
		t.Error("долларовая туса с явно выключенными копейками читается как с ними")
	}
	// Настройки нет — умолчание валюты
	if RoomFractional(&Room{Currency: "RUB"}) {
		t.Error("рублёвая туса без настройки: want false")
	}
	if !RoomFractional(&Room{Currency: "USD"}) {
		t.Error("долларовая туса без настройки: want true")
	}
	// Пустая валюта — исторический дефолт бота
	if RoomFractional(&Room{}) {
		t.Error("туса без валюты: want false")
	}
	if RoomFractional(nil) {
		t.Error("nil: want false")
	}
}
