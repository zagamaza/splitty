package sdk

import (
	"strings"
	"testing"
)

// Готовая денежная строка в колонке чисел не должна калечиться.
//
// Форматтер заменял ЛЮБУЮ запятую на разметку — так он расставляет разрядные
// пробелы у чисел, которые сам же и отформатировал. Но у готовой суммы
// «20,80 ₽» запятая десятичная, и замена превращала её в «20 80 ₽»: в тусе с
// копейками так ломались все таблицы бота — чек, доли расхода, список долгов.
func TestNumberColumnKeepsFormattedMoney(t *testing.T) {
	tb := NewTableBuilder('-', " | ")
	rows := []string{"20,80 ₽", "1 234,50 ₽"}
	tb.AddColumn(Right, NumberWithTinySpaces, func(i int) string {
		if i < len(rows) {
			return rows[i]
		}
		return ""
	})
	out := tb.Build()

	for _, want := range rows {
		if !strings.Contains(out, want) {
			t.Errorf("сумма %q искажена:\n%s", want, out)
		}
	}
	if strings.Contains(out, "20</code> <code>80") {
		t.Errorf("десятичная запятая заменена разметкой:\n%s", out)
	}
}

// Числа, которые форматтер разбирает сам, по-прежнему получают разрядные
// пробелы: починка не должна отменить то, ради чего колонка заведена.
func TestNumberColumnStillGroupsPlainNumbers(t *testing.T) {
	tb := NewTableBuilder('-', " | ")
	tb.AddColumn(Right, NumberWithTinySpaces, func(i int) string {
		if i == 0 {
			return "1234567"
		}
		return ""
	})
	if out := tb.Build(); !strings.Contains(out, "</code> <code>") {
		t.Errorf("разрядные пробелы пропали:\n%s", out)
	}
}
