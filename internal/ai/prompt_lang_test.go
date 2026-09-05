package ai

import (
	"strings"
	"testing"
)

// Поле questions читает ЧЕЛОВЕК: модель задаёт в нём уточняющие вопросы вроде
// «Сколько стоила пицца?». Промпт целиком русский, поэтому без явного указания
// языка японец получал вопрос по-русски. Языка в запросе не было вовсе.

func TestPromptAsksQuestionsInClientLanguage(t *testing.T) {
	prompt := buildPrompt(ParseInput{Text: "ужин 600", Lang: "ja"})
	if !strings.Contains(prompt, "questions") {
		t.Fatal("в промпте нет правила про questions")
	}
	if !strings.Contains(prompt, "ja") {
		t.Errorf("язык клиента не доехал до промпта:\n%s", prompt[len(prompt)-300:])
	}
}

// Обратная совместимость: старый клиент языка не шлёт, и промпт обязан
// остаться прежним — иначе выкат сломал бы разбор тем, кто не обновился.
func TestPromptWithoutLanguageIsUnchanged(t *testing.T) {
	base := buildPrompt(ParseInput{Text: "ужин 600"})
	if strings.Contains(base, "пиши на языке") {
		t.Error("без языка правило добавляться не должно")
	}
	withLang := buildPrompt(ParseInput{Text: "ужин 600", Lang: "it"})
	if len(withLang) <= len(base) {
		t.Error("с языком промпт обязан быть длиннее базового")
	}
	if !strings.HasPrefix(withLang, base) {
		t.Error("правило про язык дописывается в конец, база не меняется")
	}
}
