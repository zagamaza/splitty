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
// участников тусы. Этого достаточно, чтобы проверить путь целиком: запрос,
// ответ, режим чека, сворачивание, сохранение.
//
// Включается только вместе с dev-авторизацией (см. main.go): подставной разбор
// на проде означал бы, что люди получают выдуманные черновики.
type FakeParser struct{}

var firstNumber = regexp.MustCompile(`\d+`)

func (FakeParser) Parse(_ context.Context, in ParseInput) (ParseResult, error) {
	text := strings.TrimSpace(in.Text)
	match := firstNumber.FindStringIndex(text)
	if match == nil {
		return ParseResult{Questions: []string{"не услышал сумму"}}, nil
	}
	price, err := strconv.Atoi(text[match[0]:match[1]])
	if err != nil || price < 1 {
		return ParseResult{Questions: []string{"не услышал сумму"}}, nil
	}
	name := strings.TrimSpace(text[:match[0]] + text[match[1]:])
	if name == "" {
		name = "Расход"
	}

	shares := make([]ItemShare, 0, len(in.Participants))
	for _, p := range in.Participants {
		shares = append(shares, ItemShare{UserId: p.UserId, Weight: 1})
	}
	draft := Draft{
		Description: name,
		Sum:         price,
		Items: []DraftItem{{
			Name:   name,
			Price:  price,
			Qty:    1,
			Shares: shares,
			Kind:   "item",
		}},
	}
	if in.RequesterId > 0 {
		donor := in.RequesterId
		draft.DonorId = &donor
	}
	return ParseResult{Draft: draft}, nil
}
