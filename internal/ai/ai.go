// Package ai распознаёт расход из голоса/фото чека/текста в структурированный
// черновик. Провайдер скрыт за интерфейсом Parser — текущая реализация Gemini.
package ai

import "context"

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
}

// DraftItem позиция черновика. Unknown — имена, которые модель не смогла
// сопоставить участникам (их разрешает пользователь в UI).
type DraftItem struct {
	Name    string      `json:"name" bson:"name"`
	Price   int         `json:"price" bson:"price"`
	Qty     int         `json:"qty" bson:"qty"`
	Shares  []ItemShare `json:"shares" bson:"shares"`
	Kind    string      `json:"kind" bson:"kind"`
	Split   string      `json:"split,omitempty" bson:"split,omitempty"`
	Percent *int        `json:"percent,omitempty" bson:"percent,omitempty"`
	Unknown []string    `json:"unknown,omitempty" bson:"unknown,omitempty"`
}

// Draft черновик расхода — транспортный контракт между сервером, моделью и
// клиентом. Клиент присылает текущий Draft на правку, сервер возвращает
// обновлённый.
type Draft struct {
	Description string      `json:"description"`
	Sum         int         `json:"sum"`
	DonorId     *int        `json:"donorId,omitempty"`
	Items       []DraftItem `json:"items,omitempty"`
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
