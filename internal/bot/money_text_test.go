package bot

import "testing"

// Тексты бота печатают ТОЧНУЮ сумму.
//
// Прежняя версия собиралась правкой готовой строки и искала в ней пробел перед
// символом валюты — а он узкий неразрывный. Копейки молча пропадали, а у суммы
// с тысячами дробь вставлялась в середину числа. Проверка сравнивает строку
// целиком: именно её видит человек в чате.
func TestMoneySpaceMinor(t *testing.T) {
	const nbsp = " "

	cases := []struct {
		minor    int64
		currency string
		want     string
	}{
		{2080, "RUB", "20,80" + nbsp + "₽"},
		{2100, "RUB", "21" + nbsp + "₽"},
		{2008, "RUB", "20,08" + nbsp + "₽"},
		{25, "USD", "0,25" + nbsp + "$"},
		{123450, "RUB", "1 234,50" + nbsp + "₽"},
		{123400, "RUB", "1 234" + nbsp + "₽"},
		{-2080, "RUB", "-20,80" + nbsp + "₽"},
	}
	for _, c := range cases {
		if got := moneySpaceMinor(c.minor, c.currency); got != c.want {
			t.Errorf("moneySpaceMinor(%d, %s) = %q, want %q", c.minor, c.currency, got, c.want)
		}
	}

	// Целые суммы печатаются ровно как раньше: вид текстов бота не меняется.
	for _, sum := range []int{0, 21, 1234, 1234567} {
		if got, want := moneySpaceMinor(int64(sum)*100, "RUB"), moneySpace(sum, "RUB"); got != want {
			t.Errorf("moneySpaceMinor(%d00) = %q, moneySpace(%d) = %q — вид изменился", sum, got, sum, want)
		}
	}
}

// Кнопка «вернуть всё» кладёт сумму В ТОМ ЖЕ виде, в каком её набрал бы
// человек: строка из кнопки уходит обратно в разбор ввода. Сырые копейки
// («2050») читались как 2050 рублей, и погашение отбивалось как переплата —
// ломался даже целый долг.
func TestMinorToInputRoundTrip(t *testing.T) {
	for _, minor := range []int64{2050, 2100, 25, 1, 123456} {
		got, err := defineSumMinor(minorToInput(minor), true)
		if err != nil {
			t.Fatalf("minorToInput(%d) = %q не разбирается: %v", minor, minorToInput(minor), err)
		}
		if got != minor {
			t.Errorf("%d → %q → %d", minor, minorToInput(minor), got)
		}
	}
}

// Запрет дробей накрывает и бота: REST-слой проверяет признак сам, а бот пишет
// в базу напрямую — при выключенном рубильнике Telegram принимал «20,80» и
// заводил дробный расход в тусе, которая копеек не считает.
func TestDefineSumMinorRejectsFractionWhenDisabled(t *testing.T) {
	if _, err := defineSumMinor("20,80", false); err == nil {
		t.Error("дробная сумма принята при выключенном признаке")
	}
	got, err := defineSumMinor("21", false)
	if err != nil || got != 2100 {
		t.Errorf("целая сумма = %d, %v — должна приниматься всегда", got, err)
	}
	if got, err := defineSumMinor("20,80", true); err != nil || got != 2080 {
		t.Errorf("дробная сумма при включённом признаке = %d, %v", got, err)
	}
}

// Отрицательная сумма не сумма — включая «минус ноль».
//
// «-0,50» разбиралось как +0,50: Atoi("-0") возвращает ноль, знак терялся
// вместе с проверкой на отрицательность.
func TestDefineSumMinorRejectsNegative(t *testing.T) {
	for _, in := range []string{"-0.50", "-0,50", "-1.50", "-0", "-21"} {
		if got, err := defineSumMinor(in, true); err == nil {
			t.Errorf("%q принято как %d", in, got)
		}
	}
}
