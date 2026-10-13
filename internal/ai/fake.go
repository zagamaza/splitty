package ai

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// FakeParser — предсказуемый разбор для сквозных UI-прогонов.
//
// Главная фича продукта оставалась единственной, до которой сквозные тесты не
// дотягивались вообще: распознаванию нужен Gemini, а локальный бэкенд для
// прогонов поднимается без ключа и отвечает «распознавание недоступно». Здесь
// вход — текст вида «Такси 100», выход — чек из одной позиции на всех
// участников тусы. «Ужин: хачапури 1200, вино 2400» даёт чек из двух позиций
// с описанием «Ужин» — для кадров черновика с настоящим списком. Этого достаточно, чтобы проверить путь целиком: запрос,
// ответ, режим чека, сворачивание, сохранение.
//
// Включается только вместе с dev-авторизацией (см. main.go): подставной разбор
// на проде означал бы, что люди получают выдуманные черновики.
type FakeParser struct {
	// Fallback — фраза на случай запроса без текста (голос или фото).
	Fallback string
}

var firstNumber = regexp.MustCompile(`\d+`)

func (f FakeParser) Parse(_ context.Context, in ParseInput) (ParseResult, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		text = strings.TrimSpace(f.Fallback)
	}
	description := ""
	if head, rest, ok := strings.Cut(text, ":"); ok {
		description, text = strings.TrimSpace(head), rest
	}

	shares := make([]ItemShare, 0, len(in.Participants))
	for _, p := range in.Participants {
		shares = append(shares, ItemShare{UserId: p.UserId, Weight: 1})
	}
	draft := Draft{Description: description}
	for _, segment := range strings.Split(text, ",") {
		segment = strings.TrimSpace(segment)
		match := firstNumber.FindStringIndex(segment)
		if match == nil {
			return ParseResult{Questions: []string{"не услышал сумму"}}, nil
		}
		price, err := strconv.Atoi(segment[match[0]:match[1]])
		if err != nil || price < 1 {
			return ParseResult{Questions: []string{"не услышал сумму"}}, nil
		}
		name := strings.TrimSpace(segment[:match[0]] + segment[match[1]:])
		if name == "" {
			name = "Расход"
		}
		draft.Items = append(draft.Items, DraftItem{
			Name:   name,
			Price:  price,
			Qty:    1,
			Shares: append([]ItemShare(nil), shares...),
			Kind:   "item",
		})
		draft.Sum += price
	}
	if draft.Description == "" {
		draft.Description = draft.Items[0].Name
	}
	if in.RequesterId > 0 {
		donor := in.RequesterId
		draft.DonorId = &donor
	}
	return ParseResult{Draft: draft}, nil
}
