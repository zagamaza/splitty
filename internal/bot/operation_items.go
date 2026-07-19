package bot

import (
	"fmt"
	"html"
	"strings"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/sdk"
)

// isItemized сообщает, что операция создана с позициями чека (AI-распознавание).
// Такие операции — источник правды в поле Items; бот показывает их только для
// чтения и не даёт править (правка — в приложении).
func isItemized(op api.Operation) bool {
	return len(op.Items) > 0
}

// memberName возвращает отображаемое имя участника комнаты по его id,
// либо "?" если участник не найден (рассинхрон состава комнаты и позиций).
// Имя — пользовательский ввод, а блок позиций уходит в Telegram с ParseMode=HTML:
// без экранирования "a < b" в имени даёт 400 от Telegram, и экран операции
// перестаёт открываться у всех участников комнаты.
func memberName(members *[]api.User, id int) string {
	if members != nil {
		for _, m := range *members {
			if m.ID == id {
				return html.EscapeString(m.DisplayName)
			}
		}
	}
	return "?"
}

// itemLabel формирует подпись позиции для показа: количество (×N) для обычных
// позиций и процент (10%) для надбавок — только для отображения, в расчёте не
// участвует.
// Название позиции — пользовательский ввод (клиент шлёт его напрямую), см.
// memberName про ParseMode=HTML.
func itemLabel(it api.OperationItem) string {
	name := html.EscapeString(it.Name)
	if it.Kind == api.ItemKindSurcharge {
		if it.Percent != nil {
			return fmt.Sprintf("%s %d%%", name, *it.Percent)
		}
		return name
	}
	if it.Qty > 1 {
		return fmt.Sprintf("%s ×%d", name, it.Qty)
	}
	return name
}

// itemParticipants описывает, кто участвует в позиции: список имён участников
// (с фиксом или весом, если заданы) для обычных позиций; для надбавки —
// правило деления (пропорционально/поровну).
func itemParticipants(it api.OperationItem, members *[]api.User) string {
	if it.Kind == api.ItemKindSurcharge {
		if it.Split == api.SplitEqually {
			return "поровну"
		}
		return "пропорционально"
	}
	names := make([]string, 0, len(it.Shares))
	for _, sh := range it.Shares {
		name := memberName(members, sh.UserId)
		switch {
		case sh.Amount != nil:
			name = fmt.Sprintf("%s (%d)", name, *sh.Amount)
		case sh.Weight > 1:
			name = fmt.Sprintf("%s ×%d", name, sh.Weight)
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// renderOperationItems рендерит блок позиций чека в текст (ASCII-таблица позиций
// с ценами и итогом + перечень участников по каждой позиции). Возвращает пустую
// строку для обычных операций (Items пуст) — тогда вызывающий код показывает
// операцию как раньше.
func renderOperationItems(op api.Operation, room *api.Room) string {
	if !isItemized(op) {
		return ""
	}

	items := op.Items
	total := 0
	for _, it := range items {
		total += it.Price
	}

	tb := sdk.NewTableBuilder('-', " | ")
	tb.AddHeader("Позиция")
	tb.AddHeader("Цена")
	tb.AddColumn(sdk.Left, sdk.Monospaced, func(i int) string {
		if i < len(items) {
			return itemLabel(items[i])
		}
		if i == len(items) {
			return "Итого"
		}
		return ""
	})
	tb.AddColumn(sdk.Right, sdk.NumberWithTinySpaces, func(i int) string {
		if i < len(items) {
			return moneySpace(items[i].Price, room.Currency)
		}
		if i == len(items) {
			return moneySpace(total, room.Currency)
		}
		return ""
	})
	tb.AddSeparatorRow(len(items))

	var sb strings.Builder
	sb.WriteString("\n🧾 Позиции чека:\n")
	sb.WriteString(tb.Build())
	sb.WriteString("\n👥 Кто участвует:\n")
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("• %s: %s\n", itemLabel(it), itemParticipants(it, room.Members)))
	}
	return sb.String()
}
