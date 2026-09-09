// Package ai распознаёт расход из голоса/фото чека/текста в структурированный
// черновик. Провайдер скрыт за интерфейсом Parser — текущая реализация Gemini.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/almaznur91/splitty/internal/api"
)

// Participant участник комнаты в виде, пригодном для матчинга имён моделью.
type Participant struct {
	UserId      int      `json:"userId"`
	DisplayName string   `json:"displayName"`
	Username    string   `json:"username,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

// ItemShare доля участника в позиции черновика (транспортный вид).
type ItemShare struct {
	UserId int  `json:"userId" bson:"user_id"`
	Weight int  `json:"weight" bson:"weight"`
	Amount *int `json:"amount,omitempty" bson:"amount,omitempty"`
	// AmountMinor — фиксированная доля в минорных единицах, см. DraftItem.PriceMinor
	AmountMinor *int64 `json:"amountMinor,omitempty" bson:"amount_minor,omitempty"`
}

// DraftItem позиция черновика. Unknown — имена, которые модель не смогла
// сопоставить участникам (их разрешает пользователь в UI).
type DraftItem struct {
	Name  string `json:"name" bson:"name"`
	Price int    `json:"price" bson:"price"`
	// PriceMinor — цена строки в минорных единицах шкалы комнаты. Пока признак
	// дробного ввода выключен, дробное значение здесь отвергается на входе.
	PriceMinor *int64      `json:"priceMinor,omitempty" bson:"price_minor,omitempty"`
	Qty        int         `json:"qty" bson:"qty"`
	Shares     []ItemShare `json:"shares" bson:"shares"`
	Kind       string      `json:"kind" bson:"kind"`
	Split      string      `json:"split,omitempty" bson:"split,omitempty"`
	Percent    *int        `json:"percent,omitempty" bson:"percent,omitempty"`
	Unknown    []string    `json:"unknown,omitempty" bson:"unknown,omitempty"`
}

// Draft черновик расхода — транспортный контракт между сервером, моделью и
// клиентом. Клиент присылает текущий Draft на правку, сервер возвращает
// обновлённый.
type Draft struct {
	Description string `json:"description"`
	// Sum — округлённая проекция; SumMinor — точная величина. Пара нужна и
	// здесь: плоская диктовка «ужин 20,80» идёт мимо позиций, и без точного
	// поля черновик не мог бы её представить вовсе.
	Sum         int         `json:"sum"`
	SumMinor    *int64      `json:"sumMinor,omitempty"`
	DonorId     *int        `json:"donorId,omitempty"`
	Items       []DraftItem `json:"items,omitempty"`
}

// SumMinorOrLegacy — точная сумма черновика: записанная, иначе выведенная из
// целой.
func (d Draft) SumMinorOrLegacy() int64 {
	if d.SumMinor != nil {
		return *d.SumMinor
	}
	return int64(d.Sum) * api.MinorFactor
}

// SetSumMinor ставит сумму парой: точную и её округлённую проекцию. Точное поле
// живёт, только когда копейки есть — иначе оно не несёт ничего сверх целого.
func (d *Draft) SetSumMinor(minor int64) {
	d.Sum = api.FromMinor(minor)
	if minor%api.MinorFactor != 0 {
		value := minor
		d.SumMinor = &value
		return
	}
	d.SumMinor = nil
}

// UnmarshalJSON разбирает черновик, читая сумму ТОЧНО — по той же причине и тем
// же правилом, что цену позиции.
func (d *Draft) UnmarshalJSON(data []byte) error {
	type plain Draft
	var shadow struct {
		plain
		Sum json.RawMessage `json:"sum"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	*d = Draft(shadow.plain)

	raw := strings.Trim(strings.TrimSpace(string(shadow.Sum)), `"`)
	if raw == "" || raw == "null" {
		return nil
	}
	minor, ok := api.MinorFromDecimalString(raw)
	if !ok {
		return fmt.Errorf("сумма черновика %q не число", raw)
	}
	d.SetSumMinor(minor)
	return nil
}

// ParseInput вход распознавания: любая комбинация медиа (фото чека + голос +
// текст) в одном запросе + контекст комнаты + текущий черновик. Мульти-модально:
// с фото берутся позиции и цены, из голоса — кто что ел.
type ParseInput struct {
	Audio     []byte // голос (опционально)
	AudioMime string
	Image     []byte // фото чека (опционально)
	ImageMime string
	Text      string // текстовый ввод (опционально)

	Participants []Participant
	Currency     string
	// Fractional — туса считает копейки. Тогда модель называет цены как в
	// жизни («20.80»), и они разбираются точно, без float64. Иначе схема
	// требует целых, как раньше.
	Fractional bool
	// Lang — язык интерфейса клиента (BCP-47 в написании App Store: ja,
	// zh-Hans, ko, pt-BR, it, ru, en, de, fr, es). Пусто — прежнее поведение.
	//
	// Нужен не для распознавания (модель понимает речь сама), а для ОТВЕТА:
	// поле questions показывается пользователю, и без языка модель писала
	// уточняющие вопросы по-русски кому угодно.
	Lang string
	// Кто отправил запрос: «я/меня/мне» в надиктовке — это он.
	// 0 — неизвестно (правило в промпт не добавляется).
	RequesterId int
	Draft       *Draft // текущий черновик при правке; nil при первом распознавании
}

// HasMedia сообщает, есть ли хоть один вид ввода.
func (in ParseInput) HasMedia() bool {
	return len(in.Audio) > 0 || len(in.Image) > 0 || in.Text != ""
}

// ParseResult результат: обновлённый черновик и опциональные уточняющие вопросы.
type ParseResult struct {
	Draft     Draft    `json:"draft"`
	Questions []string `json:"questions,omitempty"`
}

// Parser распознаватель расхода. Реализация обязана быть stateless: весь
// контекст берётся из ParseInput.
type Parser interface {
	Parse(ctx context.Context, in ParseInput) (ParseResult, error)
}

// UnmarshalJSON разбирает долю позиции, читая фиксированную сумму ТОЧНО.
//
// Схема ответа в тусе с копейками разрешает дробное amount, а поле целое:
// обычный разбор падал на «10.5» — и весь /parse отвечал 502, теряя всё, что
// человек надиктовал. Правило то же, что у цены: величина читается по строке,
// целое поле рядом остаётся округлённой проекцией.
func (s *ItemShare) UnmarshalJSON(data []byte) error {
	type plain ItemShare
	var shadow struct {
		plain
		Amount json.RawMessage `json:"amount"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	*s = ItemShare(shadow.plain)

	raw := strings.Trim(strings.TrimSpace(string(shadow.Amount)), `"`)
	if raw == "" || raw == "null" {
		return nil
	}
	minor, ok := api.MinorFromDecimalString(raw)
	if !ok {
		return fmt.Errorf("фиксированная доля %q не число", raw)
	}
	rounded := api.FromMinor(minor)
	s.Amount = &rounded
	// Точное поле ставится, только когда копейки есть: у целой доли оно не
	// несёт ничего сверх целого, а лишнее поле пришлось бы сверять на входе.
	if minor%api.MinorFactor != 0 {
		s.AmountMinor = &minor
	}
	return nil
}

// UnmarshalJSON разбирает позицию черновика, читая цену ТОЧНО.
//
// Модель в тусе с копейками возвращает «20.8», а поле Price целое: обычный
// разбор на нём падает, а разбор через float64 даёт 2079.9999… и теряет
// копейку. Поэтому цена читается сырым числом-строкой и переводится в
// минорные единицы по строке (api.MinorFromDecimalString), а целое поле рядом
// остаётся округлённой проекцией — тем же правилом, что и у остальных сумм.
func (i *DraftItem) UnmarshalJSON(data []byte) error {
	// Псевдоним разрывает рекурсию: у него нет этого метода.
	type plain DraftItem
	var shadow struct {
		plain
		Price json.RawMessage `json:"price"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	*i = DraftItem(shadow.plain)

	raw := strings.Trim(strings.TrimSpace(string(shadow.Price)), `"`)
	if raw == "" || raw == "null" {
		return nil
	}
	minor, ok := api.MinorFromDecimalString(raw)
	if !ok {
		return fmt.Errorf("цена позиции %q не число", raw)
	}
	i.Price = api.FromMinor(minor)
	// Точное поле ставим, только когда копейки есть: у целой цены оно не несёт
	// ничего сверх целого, а лишнее поле пришлось бы сверять на каждом входе.
	if minor%api.MinorFactor != 0 {
		i.PriceMinor = &minor
	}
	return nil
}
