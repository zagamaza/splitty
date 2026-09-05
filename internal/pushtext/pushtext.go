// Package pushtext хранит тексты native-пушей на всех языках приложения.
//
// Отдельно от conf/lang намеренно. Там живут тексты БОТА, и он умеет ровно два
// языка (api.DefineLang отдаёт ru или en). Досыпать туда восемь языков только
// ради пушей значило бы завести наполовину заполненные файлы, из которых бот
// начал бы брать переводы и падать на отсутствующие ключи.
//
// Язык берётся у УСТРОЙСТВА (api.PushToken.Locale), а не у аккаунта: у
// человека может быть русский телефон и английский планшет.
package pushtext

import (
	"fmt"
	"strings"
)

// Ключи текстов. Значение — шаблон fmt с позиционными аргументами.
const (
	PayerAssigned    = "payer_assigned"     // кто, что
	PayerChanged     = "payer_changed"      // кто, что, новый плательщик
	ExpenseAdded     = "expense_added"      // кто, что, доля
	RecipientAdded   = "recipient_added"    // кто, что, доля
	ShareChanged     = "share_changed"      // кто, что, было, стало
	RecipientRemoved = "recipient_removed"  // кто, что
	RenamedWithPhoto = "renamed_with_photo" // кто, старое, новое
	Renamed          = "renamed"            // кто, старое, новое
	PhotoAdded       = "photo_added"        // кто, что
	DebtRepaid       = "debt_repaid"        // кто, сумма
	Invited          = "invited"            // кто
	InviteReturn     = "invite_return"      // кто

	// Напоминания о долге. Заголовок без подстановок; тело — в двух видах:
	// один долг с названием тусы и несколько долгов числом групп.
	DebtReminderTitle = "debt_reminder_title"
	DebtReminderOne   = "debt_reminder_one"  // сумма, туса
	DebtReminderMany  = "debt_reminder_many" // сумма, число групп
)

// Языки по умолчанию. Случая два, и путать их нельзя.
//
// legacy — устройство БЕЗ локали: старый клиент, который поля ещё не шлёт.
// До появления языков пуши собирались по-русски, и такому устройству он и
// должен уходить: сменить существующим пользователям язык уведомлений на
// английский — регрессия, а не улучшение.
//
// fallback — язык ЕСТЬ, но перевода на него у нас нет. Тут английский: его же
// приложение показывает всем, для кого локализации не существует.
const (
	legacy   = "ru"
	fallback = "en"
)

var texts = map[string]map[string]string{
	PayerAssigned: {
		"ru":      "%s назначил вас плательщиком «%s»",
		"en":      "%s made you the payer of “%s”",
		"de":      "%s hat dich als Zahler von „%s“ eingetragen",
		"fr":      "%s vous a désigné comme payeur de « %s »",
		"es":      "%s te ha puesto como pagador de «%s»",
		"ja":      "%s さんがあなたを「%s」の支払者にしました",
		"zh-Hans": "%s 把你设为「%s」的付款人",
		"ko":      "%s 님이 회원님을 「%s」의 결제자로 지정했습니다",
		"pt-BR":   "%s definiu você como pagador de “%s”",
		"it":      "%s ti ha indicato come pagante di “%s”",
	},
	PayerChanged: {
		"ru":      "%s сменил плательщика «%s» — теперь это %s",
		"en":      "%s changed the payer of “%s” — it’s %s now",
		"de":      "%s hat den Zahler von „%s“ geändert — jetzt ist es %s",
		"fr":      "%s a changé le payeur de « %s » — c’est %s maintenant",
		"es":      "%s ha cambiado el pagador de «%s»: ahora es %s",
		"ja":      "%s さんが「%s」の支払者を変更しました。今は %s さんです",
		"zh-Hans": "%s 更改了「%s」的付款人 — 现在是 %s",
		"ko":      "%s 님이 「%s」의 결제자를 변경했습니다. 이제 %s 님입니다",
		"pt-BR":   "%s trocou o pagador de “%s” — agora é %s",
		"it":      "%s ha cambiato il pagante di “%s”: ora è %s",
	},
	ExpenseAdded: {
		"ru":      "%s добавил расход «%s» — ваша доля %s",
		"en":      "%s added the expense “%s” — your share is %s",
		"de":      "%s hat die Ausgabe „%s“ hinzugefügt — dein Anteil: %s",
		"fr":      "%s a ajouté la dépense « %s » — votre part : %s",
		"es":      "%s ha añadido el gasto «%s»: tu parte es %s",
		"ja":      "%s さんが支出「%s」を追加しました。あなたの負担分は %s です",
		"zh-Hans": "%s 添加了支出「%s」— 你的分摊是 %s",
		"ko":      "%s 님이 지출 「%s」을(를) 추가했습니다. 회원님의 몫은 %s입니다",
		"pt-BR":   "%s adicionou a despesa “%s” — sua parte é %s",
		"it":      "%s ha aggiunto la spesa “%s” — la tua quota è %s",
	},
	RecipientAdded: {
		"ru":      "%s добавил вас в расход «%s» — ваша доля %s",
		"en":      "%s added you to the expense “%s” — your share is %s",
		"de":      "%s hat dich zur Ausgabe „%s“ hinzugefügt — dein Anteil: %s",
		"fr":      "%s vous a ajouté à la dépense « %s » — votre part : %s",
		"es":      "%s te ha añadido al gasto «%s»: tu parte es %s",
		"ja":      "%s さんがあなたを支出「%s」に追加しました。あなたの負担分は %s です",
		"zh-Hans": "%s 把你加入了支出「%s」— 你的分摊是 %s",
		"ko":      "%s 님이 회원님을 지출 「%s」에 추가했습니다. 회원님의 몫은 %s입니다",
		"pt-BR":   "%s adicionou você à despesa “%s” — sua parte é %s",
		"it":      "%s ti ha aggiunto alla spesa “%s” — la tua quota è %s",
	},
	ShareChanged: {
		"ru":      "%s изменил вашу долю в «%s»: %s → %s",
		"en":      "%s changed your share in “%s”: %s → %s",
		"de":      "%s hat deinen Anteil an „%s“ geändert: %s → %s",
		"fr":      "%s a modifié votre part dans « %s » : %s → %s",
		"es":      "%s ha cambiado tu parte en «%s»: %s → %s",
		"ja":      "%s さんが「%s」でのあなたの負担分を変更しました：%s → %s",
		"zh-Hans": "%s 修改了你在「%s」中的分摊：%s → %s",
		"ko":      "%s 님이 「%s」에서 회원님의 몫을 변경했습니다: %s → %s",
		"pt-BR":   "%s alterou a sua parte em “%s”: %s → %s",
		"it":      "%s ha modificato la tua quota in “%s”: %s → %s",
	},
	RecipientRemoved: {
		"ru":      "%s убрал вас из расхода «%s»",
		"en":      "%s removed you from the expense “%s”",
		"de":      "%s hat dich aus der Ausgabe „%s“ entfernt",
		"fr":      "%s vous a retiré de la dépense « %s »",
		"es":      "%s te ha quitado del gasto «%s»",
		"ja":      "%s さんがあなたを支出「%s」から外しました",
		"zh-Hans": "%s 把你从支出「%s」中移除了",
		"ko":      "%s 님이 회원님을 지출 「%s」에서 제외했습니다",
		"pt-BR":   "%s tirou você da despesa “%s”",
		"it":      "%s ti ha tolto dalla spesa “%s”",
	},
	RenamedWithPhoto: {
		"ru":      "%s переименовал «%s» → «%s» и добавил фото",
		"en":      "%s renamed “%s” → “%s” and added a photo",
		"de":      "%s hat „%s“ in „%s“ umbenannt und ein Foto hinzugefügt",
		"fr":      "%s a renommé « %s » en « %s » et ajouté une photo",
		"es":      "%s ha renombrado «%s» a «%s» y ha añadido una foto",
		"ja":      "%s さんが「%s」を「%s」に変更し、写真を追加しました",
		"zh-Hans": "%s 把「%s」改名为「%s」并添加了照片",
		"ko":      "%s 님이 「%s」을(를) 「%s」(으)로 바꾸고 사진을 추가했습니다",
		"pt-BR":   "%s renomeou “%s” para “%s” e adicionou uma foto",
		"it":      "%s ha rinominato “%s” in “%s” e aggiunto una foto",
	},
	Renamed: {
		"ru":      "%s переименовал «%s» → «%s»",
		"en":      "%s renamed “%s” → “%s”",
		"de":      "%s hat „%s“ in „%s“ umbenannt",
		"fr":      "%s a renommé « %s » en « %s »",
		"es":      "%s ha renombrado «%s» a «%s»",
		"ja":      "%s さんが「%s」を「%s」に変更しました",
		"zh-Hans": "%s 把「%s」改名为「%s」",
		"ko":      "%s 님이 「%s」을(를) 「%s」(으)로 변경했습니다",
		"pt-BR":   "%s renomeou “%s” para “%s”",
		"it":      "%s ha rinominato “%s” in “%s”",
	},
	PhotoAdded: {
		"ru":      "%s добавил фото к расходу «%s»",
		"en":      "%s added a photo to the expense “%s”",
		"de":      "%s hat der Ausgabe „%s“ ein Foto hinzugefügt",
		"fr":      "%s a ajouté une photo à la dépense « %s »",
		"es":      "%s ha añadido una foto al gasto «%s»",
		"ja":      "%s さんが支出「%s」に写真を追加しました",
		"zh-Hans": "%s 给支出「%s」添加了照片",
		"ko":      "%s 님이 지출 「%s」에 사진을 추가했습니다",
		"pt-BR":   "%s adicionou uma foto à despesa “%s”",
		"it":      "%s ha aggiunto una foto alla spesa “%s”",
	},
	DebtRepaid: {
		"ru":      "%s вернул вам долг %s",
		"en":      "%s paid you back %s",
		"de":      "%s hat dir %s zurückgezahlt",
		"fr":      "%s vous a remboursé %s",
		"es":      "%s te ha devuelto %s",
		"ja":      "%s さんが %s を返済しました",
		"zh-Hans": "%s 还了你 %s",
		"ko":      "%s 님이 %s을(를) 상환했습니다",
		"pt-BR":   "%s devolveu %s a você",
		"it":      "%s ti ha restituito %s",
	},
	Invited: {
		"ru":      "%s добавил вас в группу",
		"en":      "%s added you to a group",
		"de":      "%s hat dich zu einer Gruppe hinzugefügt",
		"fr":      "%s vous a ajouté à un groupe",
		"es":      "%s te ha añadido a un grupo",
		"ja":      "%s さんがあなたをグループに追加しました",
		"zh-Hans": "%s 把你加入了群组",
		"ko":      "%s 님이 회원님을 그룹에 추가했습니다",
		"pt-BR":   "%s adicionou você a um grupo",
		"it":      "%s ti ha aggiunto a un gruppo",
	},
	DebtReminderTitle: {
		"ru": "Splitor", "en": "Splitor", "de": "Splitor", "fr": "Splitor", "es": "Splitor",
		"ja": "Splitor", "zh-Hans": "Splitor", "ko": "Splitor", "pt-BR": "Splitor", "it": "Splitor",
	},
	DebtReminderOne: {
		"ru":      "Вы должны %s в «%s»",
		"en":      "You owe %s in “%s”",
		"de":      "Du schuldest %s in „%s“",
		"fr":      "Vous devez %s dans « %s »",
		"es":      "Debes %s en «%s»",
		"ja":      "「%[2]s」で %[1]s の借りがあります",
		"zh-Hans": "你在「%[2]s」中欠 %[1]s",
		"ko":      "「%[2]s」에서 %[1]s을(를) 빚지고 있습니다",
		"pt-BR":   "Você deve %s em “%s”",
		"it":      "Devi %s in “%s”",
	},
	DebtReminderMany: {
		"ru":      "Вы должны %s в %d группах",
		"en":      "You owe %s across %d groups",
		"de":      "Du schuldest %s in %d Gruppen",
		"fr":      "Vous devez %s dans %d groupes",
		"es":      "Debes %s en %d grupos",
		"ja":      "%[2]d 件のグループで %[1]s の借りがあります",
		"zh-Hans": "你在 %[2]d 个群组中欠 %[1]s",
		"ko":      "%[2]d개 그룹에서 %[1]s을(를) 빚지고 있습니다",
		"pt-BR":   "Você deve %s em %d grupos",
		"it":      "Devi %s in %d gruppi",
	},
	InviteReturn: {
		"ru":      "%s приглашает вас вернуться в группу",
		"en":      "%s invites you back to the group",
		"de":      "%s lädt dich zurück in die Gruppe ein",
		"fr":      "%s vous invite à revenir dans le groupe",
		"es":      "%s te invita a volver al grupo",
		"ja":      "%s さんがグループへの復帰に招待しています",
		"zh-Hans": "%s 邀请你回到群组",
		"ko":      "%s 님이 그룹으로 돌아오도록 초대했습니다",
		"pt-BR":   "%s convida você a voltar para o grupo",
		"it":      "%s ti invita a tornare nel gruppo",
	},
}

// Tr возвращает текст пуша на языке locale. Пустая локаль — старый клиент, ему
// уходит русский, как было до языков. Неизвестный язык обслуживается
// английским — тем же, что приложение показывает всем, для кого перевода нет.
func Tr(locale, key string, args ...any) string {
	byLang, ok := texts[key]
	if !ok {
		return ""
	}
	if locale == "" {
		locale = legacy
	}
	template, ok := byLang[locale]
	if !ok {
		template = byLang[fallback]
	}
	return fmt.Sprintf(template, args...)
}

// Canonical приводит язык, присланный устройством, к тегу, который понимает Tr.
//
// Пустая строка остаётся пустой: это «клиент языка не прислал», и такому
// устройству положен русский. А вот незнакомый язык пустым делать НЕЛЬЗЯ — это
// разные вещи: датский телефон показывает английские экраны и должен получать
// английские пуши, а не русские. Поэтому он схлопывается в fallback.
func Canonical(raw string) string {
	lang := strings.ToLower(strings.TrimSpace(raw))
	if lang == "" {
		return ""
	}
	switch lang {
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn":
		return "zh-Hans"
	case "pt-br", "pt_br":
		return "pt-BR"
	}
	for _, l := range Languages() {
		if lang == strings.ToLower(l) {
			return l
		}
	}
	return fallback
}

// Languages — языки, на которых есть тексты пушей. Для тестов полноты.
func Languages() []string {
	return []string{"ru", "en", "de", "fr", "es", "ja", "zh-Hans", "ko", "pt-BR", "it"}
}

// Keys — все ключи текстов. Для тестов полноты.
func Keys() []string {
	out := make([]string, 0, len(texts))
	for key := range texts {
		out = append(out, key)
	}
	return out
}
