// Package reminders собирает, кому напомнить о невозвращённом долге.
//
// Логика здесь чистая: на вход комнаты, на выход — кому и что слать. Ни базы,
// ни сети, ни пушей: сам джоб живёт в job.go.
package reminders

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
)

// CurrencyTotal — сколько человек должен в одной валюте.
type CurrencyTotal struct {
	Currency string
	Sum      int
}

// Target — один человек и всё, что нужно, чтобы собрать ему пуш.
type Target struct {
	UserId int
	// Groups — в скольких группах он должен.
	Groups int
	// Totals — суммы по валютам, по убыванию. Валюты НЕ складываются между
	// собой: курсов у нас нет, а «10 000 сум + 100 €» — бессмысленное число.
	Totals []CurrencyTotal
	// RoomId и RoomName — группа с самым крупным долгом. Пуш всегда ведёт в
	// конкретную комнату: без roomId тап по уведомлению не открывает ничего
	// (см. PushRoute на обоих клиентах).
	RoomId   string
	RoomName string
	// Key — отпечаток набора долгов. По нему серия напоминаний отличает
	// «тот же долг» от «вернул старый, взял новый».
	Key string
}

// Collector накапливает должников по мере обхода комнат порциями.
type Collector struct {
	// Now — «сейчас» для проверки свежести (в тестах фиксируется).
	Now time.Time
	// MaxIdle — сколько группа может молчать, прежде чем считаться мёртвой.
	// Дата создания комнаты для этого не годится: новый долг в старой комнате
	// напоминания не получил бы, а забытый в свежей — получил бы четыре.
	MaxIdle time.Duration

	// Skipped — сколько комнат пропущено из-за неисчислимых долгов. Джоб по
	// этому числу решает, можно ли доверять выводу «долгов не осталось».
	Skipped int

	debts map[int][]roomDebt
}

// roomDebt — долг одного человека в одной комнате.
type roomDebt struct {
	roomId   string
	roomName string
	currency string
	sum      int
}

// Add скармливает коллектору очередную порцию комнат.
func (c *Collector) Add(rooms []api.Room) {
	if c.debts == nil {
		c.debts = map[int][]roomDebt{}
	}
	for i := range rooms {
		c.addRoom(&rooms[i])
	}
}

func (c *Collector) addRoom(room *api.Room) {
	norm := api.NormalizedRoom(room)
	ops := *norm.Operations
	if len(ops) == 0 || len(*norm.Members) == 0 {
		return
	}
	// Мёртвая группа: последнее движение было слишком давно. Долг там либо
	// закрыли наличными, либо про него все забыли вместе с самой группой.
	if c.Now.Sub(lastActivity(ops)) > c.MaxIdle {
		return
	}

	debts, err := service.GetRoomDebts(norm)
	if err != nil {
		// Долги комнаты неисчислимы (легаси-данные бота). Пропускаем целиком:
		// напоминать по ним — значит называть выдуманные суммы.
		c.Skipped++
		return
	}

	currency := room.Currency
	if currency == "" {
		currency = api.DefaultCurrency
	}
	// Порог различимости: доли делятся с усечением копеек, поэтому каждый
	// расход оставляет до единицы валюты погрешности, а значит долг не крупнее
	// числа расходов от этой погрешности НЕ ОТЛИЧИМ. Напоминать по такому —
	// значит однажды прислать человеку «верните 3 ₽», и это ровно то, за что
	// приложения выключают. Порог считается в единицах комнаты и потому не
	// требует выдуманных значений для каждой валюты
	noise := len(ops)

	for _, d := range debts {
		if d.Debtor == nil || d.Sum <= noise {
			continue
		}
		// Архив у каждого свой: человек убрал группу из своего списка — значит
		// для него она закрыта, и напоминать по ней он не просил.
		if isArchivedFor(room, d.Debtor.ID) {
			continue
		}
		c.debts[d.Debtor.ID] = append(c.debts[d.Debtor.ID], roomDebt{
			roomId:   room.ID.Hex(),
			roomName: room.Name,
			currency: currency,
			sum:      d.Sum,
		})
	}
}

// Targets собирает итог: по одному Target на человека.
func (c *Collector) Targets() []Target {
	out := make([]Target, 0, len(c.debts))
	for userId, debts := range c.debts {
		out = append(out, targetOf(userId, debts))
	}
	// Стабильный порядок: одинаковый вход даёт одинаковый выход, иначе тесты
	// и логи джоба зависели бы от обхода map.
	sort.Slice(out, func(i, j int) bool { return out[i].UserId < out[j].UserId })
	return out
}

func targetOf(userId int, debts []roomDebt) Target {
	byCurrency := map[string]int{}
	rooms := map[string]bool{}
	var biggest roomDebt
	for _, d := range debts {
		byCurrency[d.currency] += d.sum
		rooms[d.roomId] = true
		if d.sum > biggest.sum {
			biggest = d
		}
	}

	totals := make([]CurrencyTotal, 0, len(byCurrency))
	for currency, sum := range byCurrency {
		totals = append(totals, CurrencyTotal{Currency: currency, Sum: sum})
	}
	sort.Slice(totals, func(i, j int) bool {
		if totals[i].Sum != totals[j].Sum {
			return totals[i].Sum > totals[j].Sum
		}
		return totals[i].Currency < totals[j].Currency
	})

	return Target{
		UserId:   userId,
		Groups:   len(rooms),
		Totals:   totals,
		RoomId:   biggest.roomId,
		RoomName: biggest.roomName,
		Key:      fingerprint(debts),
	}
}

// fingerprint — отпечаток набора долгов, не зависящий от порядка комнат.
func fingerprint(debts []roomDebt) string {
	parts := make([]string, 0, len(debts))
	for _, d := range debts {
		parts = append(parts, fmt.Sprintf("%s:%s:%d", d.roomId, d.currency, d.sum))
	}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(fmt.Sprint(parts)))
	return hex.EncodeToString(sum[:8])
}

// lastActivity — время последней операции комнаты.
func lastActivity(ops []api.Operation) time.Time {
	var last time.Time
	for i := range ops {
		if ops[i].CreateAt.After(last) {
			last = ops[i].CreateAt
		}
	}
	return last
}

func isArchivedFor(room *api.Room, userId int) bool {
	for _, id := range room.RoomStates.Archived {
		if id == userId {
			return true
		}
	}
	return false
}
