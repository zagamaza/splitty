package bot

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
	Center
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

	// Список индексов строк, над которыми нужно вывести горизонтальный разделитель.
	separatorRows []int

	// Если true — обрамлять таблицу рамкой сверху и снизу.
	frame bool

	// Если true — каждая строка будет начинаться с '|' и заканчиваться '|'
	lineBorders bool
}

// NewTableBuilder позволяет задать символ-разделитель для заголовка (headerSeparator)
// и строку-разделитель между колонками (columnSeparator).
func NewTableBuilder(headerSeparator rune, columnSeparator string) *TableBuilder {
	return &TableBuilder{
		headerSeparator: headerSeparator,
		columnSeparator: columnSeparator,
	}
}

// WithFrame включает возможность обрамления (ограбления) таблицы рамкой сверху и снизу.
func (tb *TableBuilder) WithFrame() *TableBuilder {
	tb.frame = true
	return tb
}

// WithLineBorders включает обрамление каждой строки символами '|' в начале и конце.
func (tb *TableBuilder) WithLineBorders() *TableBuilder {
	tb.lineBorders = true
	return tb
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

// AddColumnSimple — упрощённая версия без указания типа колонки (будет Monospaced).
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

// innerWidth вычисляет внутреннюю ширину таблицы (без учёта вертикальных границ),
// равную сумме ширин колонок плюс разделители между ними.
func (tb *TableBuilder) innerWidth(colWidths []int) int {
	totalLen := 0
	for i, w := range colWidths {
		totalLen += w
		if i < len(colWidths)-1 {
			totalLen += len(tb.columnSeparator)
		}
	}
	return totalLen
}

// padRight дополняет строку пробелами справа до нужной длины (с учётом видимой длины).
func padRight(s string, target int) string {
	current := lengthWithoutCodes(s)
	if current < target {
		return s + strings.Repeat(" ", target-current)
	}
	return s
}

// Build собирает и возвращает итоговую таблицу как строку.
func (tb *TableBuilder) Build() string {
	if len(tb.columns) == 0 {
		return "<code></code>"
	}
	// Собираем «сырые» данные.
	rawColumns := tb.getRawData()

	// Форматируем каждую колонку.
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

	// Определяем количество строк.
	rowCount := len(formatted[0])
	for _, colData := range formatted {
		if len(colData) > rowCount {
			rowCount = len(colData)
		}
	}

	// Вычисляем ширину каждой колонки на основе форматированных данных.
	colWidths := tb.calcColumnWidths(formatted)

	// Если задан заголовок, корректируем колонки: если длина заголовка больше,
	// дополняем каждую строку нужным количеством пробелов.
	var headerLine string
	if len(tb.header) > 0 {
		headerLine = tb.buildHeader(formatted, colWidths)
	}

	// Вычисляем внутреннюю ширину таблицы.
	innerWidth := tb.innerWidth(colWidths)

	var sb strings.Builder

	// Генерируем строки с данными.
	for r := 0; r < rowCount; r++ {
		var line string
		if tb.isSeparatorRow(r) {
			line = tb.buildSeparatorLine(colWidths)
		} else {
			var rowValues []string
			allEmpty := true
			for c := 0; c < len(formatted); c++ {
				val := ""
				if r < len(formatted[c]) {
					val = formatted[c][r]
				}
				rowValues = append(rowValues, val)
				if strings.TrimSpace(val) != "" {
					allEmpty = false
				}
			}
			if allEmpty {
				continue
			}
			line = strings.Join(rowValues, tb.columnSeparator)
		}
		if tb.lineBorders {
			line = padRight(line, innerWidth)
			line = "|" + line + "|"
		}
		sb.WriteString(line + "\n")
	}

	var result string
	if tb.frame {
		var framed strings.Builder
		// Верхняя рамка.
		framed.WriteString(tb.buildFrameLine(true, colWidths))
		framed.WriteString("\n")
		// Если заголовок задан, добавляем его.
		if headerLine != "" {
			if tb.lineBorders {
				headerLine = wrapLinesWithBorders(headerLine, innerWidth)
			}
			framed.WriteString(headerLine)
		}
		// Добавляем строки данных.
		framed.WriteString(sb.String())
		// Нижняя рамка.
		framed.WriteString(tb.buildFrameLine(false, colWidths))
		framed.WriteString("\n")
		result = framed.String()
	} else {
		result = headerLine + sb.String()
	}

	return "<code>" + result + "</code>"
}

// buildFrameLine строит строку рамки (верхнюю или нижнюю) на основе обновлённой ширины таблицы.
func (tb *TableBuilder) buildFrameLine(isTop bool, colWidths []int) string {
	totalLen := tb.innerWidth(colWidths)
	if tb.lineBorders {
		totalLen += 2 // по одному символу с каждой стороны
	}
	if isTop {
		return "╭" + strings.Repeat("─", totalLen) + "╮"
	}
	return "╰" + strings.Repeat("─", totalLen) + "╯"
}

// wrapLinesWithBorders добавляет '|' в начало и конец каждой непустой строки
// и дополняет строку пробелами до заданной ширины.
func wrapLinesWithBorders(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			line = padRight(line, width)
			lines[i] = "|" + line + "|"
		}
	}
	return strings.Join(lines, "\n")
}

// buildHeader формирует строку заголовка с учётом максимальной ширины каждого столбца.
// Если длина заголовка превышает ширину данных в колонке, происходит дополнение строк колонок.
func (tb *TableBuilder) buildHeader(formatted [][]string, colWidths []int) string {
	if len(tb.header) == 0 {
		return ""
	}
	var headerBuilder strings.Builder
	// Для каждой колонки проверяем, нужно ли дополнить строки.
	for i := 0; i < len(tb.header) && i < len(colWidths); i++ {
		colHeader := tb.header[i]
		headerLen := utf8.RuneCountInString(colHeader)
		if headerLen > colWidths[i] {
			diff := headerLen - colWidths[i]
			for rowIdx := 0; rowIdx < len(formatted[i]); rowIdx++ {
				if tb.columns[i].Alignment == Left {
					formatted[i][rowIdx] = formatted[i][rowIdx] + strings.Repeat(" ", diff)
				} else if tb.columns[i].Alignment == Center {
					const centerOffset = 1 // смещение вправо
					leftPad := diff/2 + centerOffset
					if leftPad > diff {
						leftPad = diff
					}
					rightPad := diff - leftPad
					formatted[i][rowIdx] = strings.Repeat(" ", leftPad) + formatted[i][rowIdx] + strings.Repeat(" ", rightPad)
				} else { // Right
					formatted[i][rowIdx] = strings.Repeat(" ", diff) + formatted[i][rowIdx]
				}
			}
			colWidths[i] = headerLen
		}
		// Выравнивание заголовка по колонке.
		spaces := colWidths[i] - utf8.RuneCountInString(colHeader)
		if tb.columns[i].Alignment == Left {
			headerBuilder.WriteString(colHeader)
			headerBuilder.WriteString(strings.Repeat(" ", spaces))
		} else if tb.columns[i].Alignment == Center {
			const centerOffset = 1 // смещение вправо
			leftPad := spaces/2 + centerOffset
			if leftPad > spaces {
				leftPad = spaces
			}
			rightPad := spaces - leftPad
			headerBuilder.WriteString(strings.Repeat(" ", leftPad))
			headerBuilder.WriteString(colHeader)
			headerBuilder.WriteString(strings.Repeat(" ", rightPad))
		} else { // Right
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
	// Формируем разделительную линию под заголовком.
	sepCount := lengthWithoutCodes(headerStr)
	sepLine := strings.Repeat(string(tb.headerSeparator), sepCount)
	return headerStr + "\n" + sepLine + "\n"
}

// buildSeparatorLine строит горизонтальную линию из символа headerSeparator.
func (tb *TableBuilder) buildSeparatorLine(colWidths []int) string {
	totalLen := 0
	for i, w := range colWidths {
		totalLen += w
		if i < len(colWidths)-1 {
			totalLen += len(tb.columnSeparator)
		}
	}
	return strings.Repeat(string(tb.headerSeparator), totalLen)
}

// isSeparatorRow проверяет, содержится ли номер строки r в слайсе separatorRows.
func (tb *TableBuilder) isSeparatorRow(r int) bool {
	for _, sep := range tb.separatorRows {
		if sep == r {
			return true
		}
	}
	return false
}

// getRawData проходит по всем колонкам, вызывая rowSource для строк, пока есть данные.
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
				columnData[i] = append(columnData[i], "")
			}
		}
		if !foundAny {
			for i := range columnData {
				columnData[i] = columnData[i][:len(columnData[i])-1]
			}
			break
		}
	}
	return columnData
}

// calcColumnWidths вычисляет, какая ширина нужна каждой колонке (с учётом форматированных строк).
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

// formatMonospaced выравнивает каждую строку по максимальной ширине с учетом выравнивания.
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
		} else if alignment == Center {
			const centerOffset = 1 // смещение вправо
			leftPad := diff/2 + centerOffset
			if leftPad > diff {
				leftPad = diff
			}
			rightPad := diff - leftPad
			out[i] = strings.Repeat(" ", leftPad) + r + strings.Repeat(" ", rightPad)
		} else { // Right
			out[i] = strings.Repeat(" ", diff) + r
		}
	}
	return out
}

// formatNotMonospaced добавляет </code> и <code> вокруг отформатированного текста.
func formatNotMonospaced(rows []string, alignment Alignment) []string {
	mono := formatMonospaced(rows, alignment)
	out := make([]string, len(rows))
	for i, r := range mono {
		out[i] = "</code>" + r + "<code>"
	}
	return out
}

// formatNumberWithTinySpaces форматирует числа с заменой запятых на "</code> <code>".
func formatNumberWithTinySpaces(rows []string, alignment Alignment) []string {
	if len(rows) == 0 {
		return rows
	}

	preFormatted := make([]string, len(rows))
	for i, row := range rows {
		numStr := strings.TrimSpace(row)
		if numStr == "" {
			preFormatted[i] = ""
			continue
		}
		val, ok := new(big.Float).SetString(numStr)
		if !ok {
			preFormatted[i] = numStr
			continue
		}
		preFormatted[i] = formatMoney(val)
	}

	maxCommas := 0
	for _, pf := range preFormatted {
		count := strings.Count(pf, ",")
		if count > maxCommas {
			maxCommas = count
		}
	}

	for i, pf := range preFormatted {
		if pf == "" {
			continue
		}
		commas := strings.Count(pf, ",")
		spacesNeeded := maxCommas - commas
		result := strings.ReplaceAll(pf, ",", "</code> <code>")
		if alignment == Left {
			result = result + strings.Repeat("</code> <code>", spacesNeeded)
		} else {
			result = strings.Repeat("</code> <code>", spacesNeeded) + result
		}
		preFormatted[i] = result
	}

	return formatMonospaced(preFormatted, alignment)
}

// formatMoney форматирует число (big.Float) в строку вида "#,##0.00".
func formatMoney(f *big.Float) string {
	floatVal, _ := f.Float64()
	sign := ""
	if floatVal < 0 {
		sign = "-"
		floatVal = -floatVal
	}
	intPart := math.Floor(floatVal)
	fracPart := floatVal - intPart
	fracPart = math.Round(fracPart*100) / 100
	if fracPart >= 1 {
		intPart += 1
		fracPart -= 1
	}
	intStr := formatIntWithCommas(int64(intPart))
	fracStr := fmt.Sprintf("%.2f", fracPart)[2:]
	return sign + intStr + "." + fracStr
}

// formatIntWithCommas разбивает int64 на группы по 3 разряда.
func formatIntWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// lengthWithoutCodes удаляет теги <code> и </code> для корректного подсчёта длины.
func lengthWithoutCodes(s string) int {
	clean := strings.ReplaceAll(s, "</code> <code>", "")
	clean = strings.ReplaceAll(clean, "<code>", "")
	clean = strings.ReplaceAll(clean, "</code>", "")
	return utf8.RuneCountInString(clean)
}
