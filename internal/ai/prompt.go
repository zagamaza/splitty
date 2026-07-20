package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildPrompt собирает текстовую инструкцию для модели: правила распознавания,
// список участников (с алиасами) и текущий черновик при правке.
func buildPrompt(in ParseInput) string {
	var b strings.Builder
	b.WriteString(`Ты помощник в приложении для деления расходов. Разбери расход в JSON-черновик по заданной схеме.

Правила:
- ВСЕГДА раскладывай расход на позиции (items), если названо хоть одно блюдо/товар — даже если позиция одна. Если цена позиции НЕ названа и её нет на фото — всё равно создай позицию с участниками, но поставь price=0 (цена не определена) и задай уточняющий вопрос в questions («Сколько стоила пицца?»). Цены НЕ выдумывай. Пустые items — только если не названо ни одной позиции (тогда sum — общая сумма, если прозвучала) или вообще ничего не понял (объясни в questions, что переспросить).
- price каждой позиции — ИТОГОВАЯ стоимость строки в целых единицах валюты (уже с учётом количества). qty — только для отображения, в делении не участвует.
- Доли участников в shares: weight — относительная доля (1 у всех = поровну; «съел вдвое больше» = weight 2). amount — фиксированная сумма участника (если названа явно, например «с Маши 500»); при заданном amount weight игнорируется.
- Сервисный сбор, чаевые, комиссию, доставку помечай kind="surcharge" (обычные позиции — kind="item"). surcharge — это надбавка ПОВЕРХ заказа; сама услуга (такси, билеты, прокат, отель) — обычная позиция item с участниками. У surcharge не заполняй shares. Сумму сбора всегда клади в price (если назван процент — посчитай сумму сам). split="proportional" для процентных сборов (делится по съеденному), split="equally" для фиксированных вроде доставки. percent заполняй только для показа.
- Матчинг имён: сопоставляй произнесённые имена участникам по displayName, username и aliases. Если имя не удаётся однозначно сопоставить (или подходит несколько участников) — НЕ угадывай, положи это имя строкой в unknown соответствующей позиции.
- donorId — id того, кто заплатил (если понятно). sum — общая сумма расхода.
- Если приложены и фото чека, и голос: позиции и ЦЕНЫ бери с ФОТО чека (они точные), а КТО что ел и как делить — из ГОЛОСА. Голос уточняет распределение по позициям с чека.
- Отвечай ТОЛЬКО валидным JSON по схеме, без пояснений.`)

	if len(in.Participants) > 0 {
		b.WriteString("\n\nУчастники комнаты:")
		for _, p := range in.Participants {
			b.WriteString(fmt.Sprintf("\n- id=%d, имя=%q", p.UserId, p.DisplayName))
			if p.Username != "" {
				b.WriteString(fmt.Sprintf(", username=@%s", p.Username))
			}
			if len(p.Aliases) > 0 {
				// прозвища пишет пользователь (в том числе ЧУЖОЙ — любой сосед по
				// комнате), поэтому в промпт они идут только в кавычках %q, как и
				// displayName: сырая подстановка позволяла бы вписать в профиль
				// участника строку с переводом строки и инструкцией для модели
				quoted := make([]string, 0, len(p.Aliases))
				for _, a := range p.Aliases {
					quoted = append(quoted, fmt.Sprintf("%q", a))
				}
				b.WriteString(fmt.Sprintf(", прозвища=[%s]", strings.Join(quoted, ", ")))
			}
		}
	}
	if in.RequesterId != 0 {
		b.WriteString(fmt.Sprintf(
			"\n\nЗапрос отправил участник id=%d: «я», «меня», «мне», «моё» в надиктовке — это ОН, а не кто-то из списка по созвучию.",
			in.RequesterId,
		))
	}
	if in.Currency != "" {
		b.WriteString("\n\nВалюта: " + in.Currency)
	}

	if in.Draft != nil {
		raw, _ := json.Marshal(in.Draft)
		b.WriteString("\n\nТЕКУЩИЙ ЧЕРНОВИК (это ИСТИНА — примени только правку из нового ввода, не пересобирай с нуля, не меняй уже проставленные доли и разрешённые имена, если правка их не касается):\n")
		b.Write(raw)
	}

	return b.String()
}

// draftSchema — responseSchema для Gemini (подмножество OpenAPI). Держим в
// синхроне с типами Draft/DraftItem/ItemShare.
func draftSchema() map[string]any {
	share := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"userId": map[string]any{"type": "integer"},
			"weight": map[string]any{"type": "integer"},
			"amount": map[string]any{"type": "integer", "nullable": true},
		},
		"required": []string{"userId", "weight"},
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"price":   map[string]any{"type": "integer"},
			"qty":     map[string]any{"type": "integer"},
			"shares":  map[string]any{"type": "array", "items": share},
			"kind":    map[string]any{"type": "string", "enum": []string{"item", "surcharge"}},
			"split":   map[string]any{"type": "string", "enum": []string{"proportional", "equally"}, "nullable": true},
			"percent": map[string]any{"type": "integer", "nullable": true},
			"unknown": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"name", "price", "kind", "shares"},
	}
	draft := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{"type": "string"},
			"sum":         map[string]any{"type": "integer"},
			"donorId":     map[string]any{"type": "integer", "nullable": true},
			"items":       map[string]any{"type": "array", "items": item},
		},
		"required": []string{"description", "sum", "items"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"draft":     draft,
			"questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"draft"},
	}
}
