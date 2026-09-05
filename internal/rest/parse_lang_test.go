package rest

import "testing"

// Клиенты пишут один и тот же язык по-разному: Android отдаёт zh-CN, iOS —
// zh-Hans. В промпт должно уходить одно значение, иначе модель получает два
// разных кода на один язык.
func TestParseLangCanonicalises(t *testing.T) {
	cases := map[string]string{
		"zh-Hans": "zh-Hans", "zh-CN": "zh-Hans", "zh": "zh-Hans", "ZH-HANS-CN": "zh-Hans",
		"pt-BR": "pt-BR", "pt_BR": "pt-BR",
		"ja": "ja", "ko": "ko", "it": "it", "ru": "ru", "en": "en",
		" JA ": "ja",
	}
	for raw, want := range cases {
		if got := parseLang(raw); got != want {
			t.Errorf("parseLang(%q) = %q, ожидалось %q", raw, got, want)
		}
	}
}

// Незнакомый язык — не ошибка и не выдуманный код: пустая строка означает
// прежнее поведение, а разбор расхода обязан продолжиться.
func TestParseLangIgnoresUnknown(t *testing.T) {
	for _, raw := range []string{"", "klingon", "xx-YY", "'; DROP", "ru-RU-extra"} {
		if got := parseLang(raw); got != "" {
			t.Errorf("parseLang(%q) = %q, ожидалась пустая строка", raw, got)
		}
	}
}
