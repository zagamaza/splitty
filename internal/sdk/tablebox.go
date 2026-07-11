package sdk

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"
)

type Alignment int

const (
	Left Alignment = iota
	Right
)

type ColumnType int

const (
	Monospaced ColumnType = iota
	NumberWithTinySpaces
	NotMonospaced
)

// RowSource — функция, которая по индексу строки возвращает текст.
// Если строк больше нет — верните "" (или nil-string), и она не будет выводиться.
type RowSource func(rowIndex int) string

// Column — описание одной колонки (её тип, выравнивание, функция для получения данных).
type Column struct {
	Alignment Alignment
	Type      ColumnType
	RowSource RowSource
}

// TableBuilder — аналогичный класс/структура из Kotlin, здесь на Go.
type TableBuilder struct {
	columns         []Column
	columnSeparator string
	headerSeparator rune
	header          []string

	// Новое поле: список индексов строк, над которыми нужно вывести горизонтальный разделитель
	separatorRows []int
}

// NewTableBuilder позволяет задать символ-разделитель для заголовка (headerSeparator)
// и строку-разделитель между колонками (columnSeparator).
func NewTableBuilder(headerSeparator rune, columnSeparator string) *TableBuilder {
	return &TableBuilder{
		headerSeparator: headerSeparator,
		columnSeparator: columnSeparator,
	}
}

// AddHeader добавляет текст заголовка колонки.
func (tb *TableBuilder) AddHeader(h string) *TableBuilder {
	tb.header = append(tb.header, h)
	return tb
}

// AddColumn с указанием выравнивания, типа колонки и функции-источника.
func (tb *TableBuilder) AddColumn(al Alignment, ct ColumnType, src RowSource) *TableBuilder {
	tb.columns = append(tb.columns, Column{
		Alignment: al,
		Type:      ct,
		RowSource: src,
	})
	return tb
}

// AddColumnSimple упрощённая версия без указания типа колонки (будет Monospaced).
func (tb *TableBuilder) AddColumnSimple(al Alignment, src RowSource) *TableBuilder {
	tb.columns = append(tb.columns, Column{
		Alignment: al,
		Type:      Monospaced,
		RowSource: src,
	})
	return tb
}

// AddSeparatorRow добавляет индекс строки, над (или перед) которой печатается разделитель.
func (tb *TableBuilder) AddSeparatorRow(rowIndex int) *TableBuilder {
	tb.separatorRows = append(tb.separatorRows, rowIndex)
	return tb
}

func (tb *TableBuilder) Build() string {
	if len(tb.columns) == 0 {
		return "<code></code>"
	}
	// Собираем «сырые» данные
	rawColumns := tb.getRawData()

	// Форматируем каждую колонку
	formatted := make([][]string, len(tb.columns))
	for i, col := range tb.columns {
		switch col.Type {
		case Monospaced:
			formatted[i] = formatMonospaced(rawColumns[i], col.Alignment)
		case NumberWithTinySpaces:
			formatted[i] = formatNumberWithTinySpaces(rawColumns[i], col.Alignment)
		case NotMonospaced:
			formatted[i] = formatNotMonospaced(rawColumns[i], col.Alignment)
		}
	}

	// Посчитаем количество строк (rowCount)
	rowCount := len(formatted[0])
	for _, colData := range formatted {
		if len(colData) > rowCount {
			rowCount = len(colData)
		}
	}

	// Считаем ширины колонок (для заголовка и разделителей)
	colWidths := tb.calcColumnWidths(formatted)

	var sb strings.Builder

	// Генерируем строки таблицы
	for r := 0; r < rowCount; r++ {
		// Если нужна линия-разделитель перед этой строкой:
		if tb.isSeparatorRow(r) {
			sb.WriteString(tb.buildSeparatorLine(colWidths))
			sb.WriteString("\n")
		}

		// Собираем значения всех колонок в текущей строке
		var rowValues []string
		allEmpty := true
		for c := 0; c < len(formatted); c++ {
			val := ""
			if r < len(formatted[c]) {
				val = formatted[c][r]
			}
			rowValues = append(rowValues, val)

			// Если хотя бы в одной колонке не пусто (с учётом пробелов), значит строка "не пустая"
			if strings.TrimSpace(val) != "" {
				allEmpty = false
			}
		}

		// Если все колонки пустые, выводим просто перевод строки (без columnSeparator)
		if allEmpty {
			sb.WriteString("\n")
			continue
		}

		// Иначе печатаем все колонки с разделителем
		for c := 0; c < len(rowValues); c++ {
			sb.WriteString(rowValues[c])
			if c < len(rowValues)-1 {
				sb.WriteString(tb.columnSeparator)
			}
		}
		sb.WriteString("\n")
	}

	// Формируем заголовок (если есть)
	headerLine := tb.buildHeader(formatted, colWidths)
	if headerLine != "" {
		// Вставляем заголовок в самое начало
		return "<code>" + headerLine + sb.String() + "</code>"
	}
	return "<code>" + sb.String() + "</code>"
}

// buildHeader формирует строку заголовка с учётом максимальной ширины каждого столбца,
// выравнивания и т.д. Аналог логики из Kotlin-кода.
func (tb *TableBuilder) buildHeader(formatted [][]string, colWidths []int) string {
	if len(tb.header) == 0 {
		return ""
	}
	var headerBuilder strings.Builder

	// Формируем одну строку заголовка
	for i := 0; i < len(tb.header) && i < len(colWidths); i++ {
		colHeader := tb.header[i]
		spaces := colWidths[i] - utf8.RuneCountInString(colHeader)
		align := tb.columns[i].Alignment

		if spaces < 0 {
			// заголовок длиннее, чем текущая ширина колонки
			// нужно «расширить» форматированную колонку
			diff := -spaces
			for rowIdx := 0; rowIdx < len(formatted[i]); rowIdx++ {
				if align == Left {
					formatted[i][rowIdx] = formatted[i][rowIdx] + strings.Repeat(" ", diff)
				} else {
					formatted[i][rowIdx] = strings.Repeat(" ", diff) + formatted[i][rowIdx]
				}
			}
			colWidths[i] = utf8.RuneCountInString(colHeader)
			spaces = 0
		}

		if align == Left {
			headerBuilder.WriteString(colHeader)
			headerBuilder.WriteString(strings.Repeat(" ", spaces))
		} else {
			headerBuilder.WriteString(strings.Repeat(" ", spaces))
			headerBuilder.WriteString(colHeader)
		}
		if i < len(tb.header)-1 {
			headerBuilder.WriteString(tb.columnSeparator)
		}
	}
	headerStr := headerBuilder.String()
	if headerStr == "" {
		return ""
	}

	// Добавляем перевод строки и «черту» под заголовком:
	sepCount := lengthWithoutCodes(headerStr)
	sepLine := strings.Repeat(string(tb.headerSeparator), sepCount)
	return headerStr + "\n" + sepLine + "\n"
}

// buildSeparatorLine строит горизонтальную линию из символа tb.headerSeparator
// такой же ширины, что и у текущей строки (учитывая columnSeparator).
func (tb *TableBuilder) buildSeparatorLine(colWidths []int) string {
	totalLen := 0
	for i, w := range colWidths {
		totalLen += w
		// Добавим длину разделителя (кроме последней колонки)
		if i < len(colWidths)-1 {
			totalLen += len(tb.columnSeparator)
		}
	}
	return strings.Repeat(string(tb.headerSeparator), totalLen)
}

// isSeparatorRow проверяет, содержится ли номер строки r в слайсе separatorRows
func (tb *TableBuilder) isSeparatorRow(r int) bool {
	for _, sep := range tb.separatorRows {
		if sep == r {
			return true
		}
	}
	return false
}

// getRawData пробегается по всем колонкам, вызывая rowSource для строк
// до тех пор, пока хоть в одной колонке есть данные (не nil/пустой).
func (tb *TableBuilder) getRawData() [][]string {
	columnData := make([][]string, len(tb.columns))
	for i := range columnData {
		columnData[i] = []string{}
	}

	for {
		foundAny := false
		for i, col := range tb.columns {
			rowIdx := len(columnData[i])
			val := col.RowSource(rowIdx)
			if val != "" {
				foundAny = true
				columnData[i] = append(columnData[i], val)
			} else {
				// записываем пустую строку, чтобы все столбцы были одинаковой длины на этом шаге
				columnData[i] = append(columnData[i], "")
			}
		}
		// Если во всех колонках вернулся пустой результат — завершаем
		if !foundAny {
			// Удалим добавленную «пустую» строку
			for i := range columnData {
				columnData[i] = columnData[i][:len(columnData[i])-1]
			}
			break
		}
	}
	return columnData
}

// calcColumnWidths вычисляет, какая ширина нужна каждой колонке
// (учитывая все её уже отформатированные строки).
func (tb *TableBuilder) calcColumnWidths(formatted [][]string) []int {
	colWidths := make([]int, len(tb.columns))
	for i := 0; i < len(tb.columns); i++ {
		maxWidth := 0
		for _, rowVal := range formatted[i] {
			width := lengthWithoutCodes(rowVal)
			if width > maxWidth {
				maxWidth = width
			}
		}
		colWidths[i] = maxWidth
	}
	return colWidths
}

// formatMonospaced выравнивает каждую строку по максимальной ширине.
func formatMonospaced(rows []string, alignment Alignment) []string {
	if len(rows) == 0 {
		return rows
	}
	maxLen := 0
	for _, r := range rows {
		if utf8.RuneCountInString(r) > maxLen {
			maxLen = utf8.RuneCountInString(r)
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		diff := maxLen - utf8.RuneCountInString(r)
		if diff < 0 {
			diff = 0
		}
		if alignment == Left {
			out[i] = r + strings.Repeat(" ", diff)
		} else {
			out[i] = strings.Repeat(" ", diff) + r
		}
	}
	return out
}

// formatNotMonospaced похож на формат моно, но вставляет </code> и <code>
// по аналогии с Kotlin-кодом.
func formatNotMonospaced(rows []string, alignment Alignment) []string {
	mono := formatMonospaced(rows, alignment)
	out := make([]string, len(rows))
	for i, r := range mono {
		out[i] = "</code>" + r + "<code>"
	}
	return out
}

// formatNumberWithTinySpaces имитирует Kotlin-логику с заменой запятых на "</code> <code>"
// и вставкой дополнительных кусочков для выравнивания.
func formatNumberWithTinySpaces(rows []string, alignment Alignment) []string {
	if len(rows) == 0 {
		return rows
	}

	// Сначала формируем число в виде "#,##0.00"
	preFormatted := make([]string, len(rows))
	for i, row := range rows {
		numStr := strings.TrimSpace(row)
		if numStr == "" {
			preFormatted[i] = ""
			continue
		}
		// Пробуем распарсить в big.Float
		val, ok := new(big.Float).SetString(numStr)
		if !ok {
			// Если не парсится — оставляем как есть
			preFormatted[i] = numStr
			continue
		}
		preFormatted[i] = formatMoney(val)
	}

	// Считаем максимально возможное количество запятых (групп 3 разрядов)
	maxCommas := 0
	for _, pf := range preFormatted {
		count := strings.Count(pf, ",")
		if count > maxCommas {
			maxCommas = count
		}
	}

	// Заменяем запятые и учитываем выравнивание
	for i, pf := range preFormatted {
		if pf == "" {
			continue
		}
		commas := strings.Count(pf, ",")
		spacesNeeded := maxCommas - commas

		// Заменяем все "," на "</code> <code>"
		result := strings.ReplaceAll(pf, ",", "</code> <code>")
		if alignment == Left {
			result = result + strings.Repeat("</code> <code>", spacesNeeded)
		} else {
			result = strings.Repeat("</code> <code>", spacesNeeded) + result
		}
		preFormatted[i] = result
	}

	// Теперь выравниваем с учётом новых вставок:
	return formatMonospaced(preFormatted, alignment)
}

// formatMoney приводит число (big.Float) к строке вида "#,##0.00".
func formatMoney(f *big.Float) string {
	floatVal, _ := f.Float64()
	sign := ""
	if floatVal < 0 {
		sign = "-"
		floatVal = -floatVal
	}
	// Целая часть
	intPart := math.Floor(floatVal)
	// Дробная часть
	fracPart := floatVal - intPart

	// Округлим дробную часть до двух знаков
	fracPart = math.Round(fracPart*100) / 100
	// Если при округлении получилось 1.00 (например 0.9999 -> 1.00),
	// то увеличим intPart на 1, а fracPart вернём к 0
	if fracPart >= 1 {
		intPart += 1
		fracPart -= 1
	}

	// Форматируем целую часть
	intStr := formatIntWithCommas(int64(intPart))

	// Формируем дробную: 2 знака
	fracStr := fmt.Sprintf("%.2f", fracPart)[2:] // обрежем "0."
	return sign + intStr + "." + fracStr
}

// formatIntWithCommas разбивает int64 на группы по 3 разряда, вставляя запятые.
func formatIntWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	// Разбиваем на блоки по 3 символа (справа налево).
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// lengthWithoutCodes — убираем "</code> <code>" и все <code>/<code>, считаем длину без них.
func lengthWithoutCodes(s string) int {
	clean := strings.ReplaceAll(s, "</code> <code>", "")
	clean = strings.ReplaceAll(clean, "<code>", "")
	clean = strings.ReplaceAll(clean, "</code>", "")
	return utf8.RuneCountInString(clean)
}
