package pushtext

import (
	"strings"
	"testing"
)

// Полнота таблицы: пропущенный язык виден только на живом устройстве, где
// человек получает пуш на чужом языке. Пусть его ловит тест.
func TestEveryKeyHasEveryLanguage(t *testing.T) {
	var problems []string
	for _, key := range Keys() {
		for _, lang := range Languages() {
			if _, ok := texts[key][lang]; !ok {
				problems = append(problems, key+"/"+lang)
			}
		}
	}
	if len(problems) > 0 {
		t.Fatalf("нет перевода: %s", strings.Join(problems, ", "))
	}
}

// Число подстановок обязано совпадать во всех языках: лишний %s в переводе
// даёт «%!s(MISSING)» прямо в баннере, недостающий — потерянную сумму.
func TestPlaceholderCountMatches(t *testing.T) {
	// Считаем ВСЕ подстановки, включая позиционные «%[2]s»: в японском и
	// корейском порядок аргументов другой, и без позиционной формы сумма с
	// названием тусы менялись бы местами.
	count := func(s string) int {
		return strings.Count(s, "%") - 2*strings.Count(s, "%%")
	}
	for _, key := range Keys() {
		want := count(texts[key]["ru"])
		for _, lang := range Languages() {
			if got := count(texts[key][lang]); got != want {
				t.Errorf("%s/%s: подстановок %d, у русского %d", key, lang, got, want)
			}
		}
	}
}

// Позиционные подстановки обязаны давать тот же результат, что и обычные:
// перепутанный порядок аргументов выдал бы «в «300 ₽» у Коворк».
func TestPositionalArgumentsRenderCorrectly(t *testing.T) {
	ja := Tr("ja", DebtReminderOne, "¥300", "コワーク")
	if !strings.Contains(ja, "¥300") || !strings.Contains(ja, "コワーク") {
		t.Errorf("аргументы потерялись: %q", ja)
	}
	if strings.Contains(ja, "%!") {
		t.Errorf("битая подстановка: %q", ja)
	}
	ko := Tr("ko", DebtReminderMany, "₩3,000", 4)
	if strings.Contains(ko, "%!") {
		t.Errorf("битая подстановка: %q", ko)
	}
}

// Устройство БЕЗ локали — это старый клиент, у которого пуши до сих пор были
// русскими. Отдать ему английский значит сменить язык уведомлений живым
// пользователям на выкате: регрессия, а не улучшение.
func TestEmptyLocaleKeepsPreviousBehaviour(t *testing.T) {
	russian := Tr("ru", DebtRepaid, "Аня", "300 ₽")
	if got := Tr("", DebtRepaid, "Аня", "300 ₽"); got != russian {
		t.Errorf("Tr(\"\") = %q, ожидался прежний русский %q", got, russian)
	}
}

// А вот язык, который есть, но не переведён, — другой случай: тут английский,
// его же приложение показывает всем, для кого локализации нет.
func TestUnsupportedLanguageFallsBackToEnglish(t *testing.T) {
	english := Tr("en", DebtRepaid, "Аня", "300 ₽")
	for _, locale := range []string{"klingon", "sv", "pl"} {
		if got := Tr(locale, DebtRepaid, "Аня", "300 ₽"); got != english {
			t.Errorf("Tr(%q) = %q, ожидался английский %q", locale, got, english)
		}
	}
}

func TestTranslatesToRequestedLanguage(t *testing.T) {
	if got := Tr("ja", DebtRepaid, "アンナ", "¥300"); !strings.Contains(got, "返済") {
		t.Errorf("японский текст не подставился: %q", got)
	}
	if got := Tr("ru", ShareChanged, "Аня", "Чай", "124", "83"); !strings.Contains(got, "долю") {
		t.Errorf("русский текст не подставился: %q", got)
	}
}

// Незнакомый ключ — пустая строка, а не паника: вызывающий проверит и не
// станет слать пустой пуш.
func TestUnknownKeyIsEmpty(t *testing.T) {
	if got := Tr("ru", "нет такого ключа"); got != "" {
		t.Errorf("ожидалась пустая строка, получено %q", got)
	}
}

// TestCanonicalKeepsLegacyApart — пустая локаль и незнакомый язык это разные
// случаи. Схлопни их в одно, и датский телефон, показывающий английские экраны,
// начнёт получать русские пуши.
func TestCanonicalKeepsLegacyApart(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"ru", "ru"},
		{"RU", "ru"},
		{" en ", "en"},
		{"zh-CN", "zh-Hans"},
		{"zh-Hans", "zh-Hans"},
		{"pt_BR", "pt-BR"},
		{"pt-BR", "pt-BR"},
		{"da", "en"},
		{"tlh", "en"},
	}
	for _, tt := range tests {
		if got := Canonical(tt.in); got != tt.want {
			t.Errorf("Canonical(%q) = %q, ожидалось %q", tt.in, got, tt.want)
		}
	}
	if Tr(Canonical("da"), Invited, "Автор") == Tr("", Invited, "Автор") {
		t.Error("датское устройство получило тот же текст, что и клиент без локали (русский)")
	}
}
