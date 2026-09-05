package reminders

import (
	"strings"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/pushtext"
	"github.com/gookit/i18n"
)

// maxCurrenciesInBody — сколько валют показываем в теле пуша. Больше двух не
// помещается в строку уведомления, а курсов, чтобы свести их в одну сумму, у
// нас нет.
const maxCurrenciesInBody = 2

// Body собирает тело пуша на языке человека.
//
// Формулировка говорит ровно о невозвращённом долге и не утверждает, что
// человек «в минусе» вообще: ему самому может быть должны больше, и «вы должны»
// про общий баланс было бы неправдой.
func Body(t Target, lang string) string {
	amount := amountText(t.Totals)
	if t.Groups == 1 {
		return i18n.Tr(lang, "push_debt_reminder_one", amount, t.RoomName)
	}
	return i18n.Tr(lang, "push_debt_reminder_many", amount, t.Groups)
}

// Title — заголовок пуша.
func Title(lang string) string {
	return i18n.Tr(lang, "push_debt_reminder_title")
}

// PushBody и PushTitle — тексты PUSH на языке УСТРОЙСТВА. Отдельно от Body и
// Title: те идут через i18n вместе с текстами бота и знают только ru/en, а у
// пуша языков десять — см. пакет pushtext.
func PushBody(t Target, locale string) string {
	amount := amountText(t.Totals)
	if t.Groups == 1 {
		return pushtext.Tr(locale, pushtext.DebtReminderOne, amount, t.RoomName)
	}
	return pushtext.Tr(locale, pushtext.DebtReminderMany, amount, t.Groups)
}

func PushTitle(locale string) string {
	return pushtext.Tr(locale, pushtext.DebtReminderTitle)
}

// amountText — суммы по валютам: «1 200 ₽» либо «1 200 ₽ и 30 €».
func amountText(totals []CurrencyTotal) string {
	shown := totals
	if len(shown) > maxCurrenciesInBody {
		shown = shown[:maxCurrenciesInBody]
	}
	parts := make([]string, 0, len(shown))
	for _, t := range shown {
		parts = append(parts, api.MoneyWithSymbol(t.Sum, t.Currency))
	}
	return strings.Join(parts, " + ")
}

// PushData — полезная нагрузка для клиента. roomId обязателен: без него тап по
// уведомлению не открывает ничего (PushRoute на обоих клиентах возвращает nil).
func PushData(t Target) map[string]string {
	return map[string]string{
		"channel": "debts",
		"type":    "debt_reminder",
		"roomId":  t.RoomId,
	}
}
