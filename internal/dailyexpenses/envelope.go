package dailyexpenses

import (
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

// Конверт выгрузки расходов наружу.
//
// Раньше на сторонний адрес уходила доменная модель как есть: массив операций
// из mongo, в котором смешаны комнаты с РАЗНЫМИ валютами, а у самой операции
// валюты нет — она известна только родительской комнате. Получатель не мог
// истолковать число: он не знал, в чём оно.
//
// Поэтому здесь свой тип со своей версией: он развязывает выгрузку и
// внутреннюю модель. Правка модели больше не меняет молча смысл того, что
// уходит наружу, а получателю есть на что смотреть, чтобы заметить смену
// контракта.

// exportVersion — версия конверта. Растёт, когда меняется СМЫСЛ полей.
const exportVersion = 1

// exportExponent — во сколько десятичных знаков укладывается минорная единица
// относительно единицы валюты: sumMinor = sum × 10^exponent. Шкала хранения
// одна для всех валют, включая иену и вону, у которых сотых в обороте нет.
const exportExponent = 2

type exportEnvelope struct {
	Version     int             `json:"version"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Expenses    []exportExpense `json:"expenses"`
}

// exportExpense — одна операция вместе с контекстом, без которого число не
// прочитать: комнатой, валютой и шкалой.
type exportExpense struct {
	ID       string `json:"id"`
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
	Currency string `json:"currency"`
	Exponent int    `json:"exponent"`

	Description string `json:"description"`
	// Sum — округлённая проекция, SumMinor — точная величина. Пара, как в API:
	// получатель, который про копейки не знает, читает первое и не ломается.
	Sum      int   `json:"sum"`
	SumMinor int64 `json:"sumMinor"`

	DonorID         int       `json:"donorId"`
	IsDebtRepayment bool      `json:"isDebtRepayment"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`

	Recipients []exportShare `json:"recipients"`
}

// exportShare — доля участника в той же паре величин.
type exportShare struct {
	UserID   int   `json:"userId"`
	Sum      int   `json:"sum"`
	SumMinor int64 `json:"sumMinor"`
}

// exportExpenses раскладывает операции комнаты в конверт.
func exportExpenses(room api.Room, operations []api.Operation) []exportExpense {
	out := make([]exportExpense, 0, len(operations))
	for i := range operations {
		op := operations[i]
		expense := exportExpense{
			ID:              op.ID.Hex(),
			RoomID:          room.ID.Hex(),
			RoomName:        room.Name,
			Currency:        api.RoomCurrency(&room),
			Exponent:        exportExponent,
			Description:     op.Description,
			Sum:             api.FromMinor(op.SumMinorOrLegacy()),
			SumMinor:        op.SumMinorOrLegacy(),
			IsDebtRepayment: op.IsDebtRepayment,
			Status:          string(op.Status),
			CreatedAt:       op.CreateAt.UTC(),
			Recipients:      make([]exportShare, 0, len(op.RecipientsWithSum)),
		}
		if op.Donor != nil {
			expense.DonorID = op.Donor.ID
		}
		for j := range op.RecipientsWithSum {
			share := op.RecipientsWithSum[j]
			expense.Recipients = append(expense.Recipients, exportShare{
				UserID:   share.User.ID,
				Sum:      api.FromMinor(share.SumMinorOrLegacy()),
				SumMinor: share.SumMinorOrLegacy(),
			})
		}
		out = append(out, expense)
	}
	return out
}
