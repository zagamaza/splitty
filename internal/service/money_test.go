package service

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Тесты денежной арифметики расчёта долгов develop, адаптированные с ветки
// wip/ios-app-master-base: там расчёт был целочисленным, здесь — float-модель
// develop (recipients_with_sum) с фиксами moneyToInt и лимита итераций.
// Сценарии контракта v2 уровня API (канонические доли, by_exact_amount,
// легаси-синтез, погашения через REST) покрыты в internal/rest/handlers_test.go.

// fakeMoneyRoomRepo отдаёт одну комнату; остальные методы RoomRepository тестами не используются
type fakeMoneyRoomRepo struct {
	repository.RoomRepository
	room *api.Room
}

func (f fakeMoneyRoomRepo) FindById(_ context.Context, _ string) (*api.Room, error) {
	return f.room, nil
}

var (
	moneyUserA = api.User{ID: 1, DisplayName: "A"}
	moneyUserB = api.User{ID: 2, DisplayName: "B"}
	moneyUserC = api.User{ID: 3, DisplayName: "C"}
)

// botEquallySpend расход «как из бота develop»: equally с ДРОБНЫМИ float-долями
// Sum/n в recipients_with_sum (100/3 = 33.33…), Status=active
func botEquallySpend(sum int, donor api.User, recipients ...api.User) api.Operation {
	withSum := make([]api.RecipientWithSum, 0, len(recipients))
	for _, r := range recipients {
		withSum = append(withSum, api.RecipientWithSum{User: r, Sum: float64(sum) / float64(len(recipients))})
	}
	return api.Operation{
		ID:                primitive.NewObjectID(),
		Sum:               sum,
		Donor:             &donor,
		RecipientsWithSum: withSum,
		Status:            "active",
		SplitType:         "equally",
	}
}

// repayOp погашение долга «как из бота develop»: active, единственный получатель-кредитор
func repayOp(sum int, debtor, lender api.User) api.Operation {
	return api.Operation{
		ID:                primitive.NewObjectID(),
		Sum:               sum,
		Donor:             &debtor,
		RecipientsWithSum: []api.RecipientWithSum{{User: lender, Sum: float64(sum)}},
		IsDebtRepayment:   true,
		Status:            "active",
	}
}

func moneyRoom(members []api.User, ops []api.Operation) api.Room {
	return api.Room{ID: primitive.NewObjectID(), Members: &members, Operations: &ops}
}

func findMoneyDebt(debts []api.Debt, debtorId, lenderId int) int {
	for _, d := range debts {
		if d.Debtor.ID == debtorId && d.Lender.ID == lenderId {
			return d.Sum
		}
	}
	return 0
}

// moneyToInt поглощает float-погрешность (round при отклонении < 1e-6),
// но сохраняет усечение честных дробных остатков — семантику develop
func TestMoneyToInt(t *testing.T) {
	assert.Equal(t, 26, moneyToInt(25.999999999999996), "float-погрешность округляется вверх")
	assert.Equal(t, 26, moneyToInt(26.000000000000004), "float-погрешность округляется вниз")
	assert.Equal(t, 1, moneyToInt(0.9999999999999999), "почти-рубль не должен пропадать")
	assert.Equal(t, 33, moneyToInt(33.333333333333336), "честная дробная доля усекается")
	assert.Equal(t, 33, moneyToInt(33.9), "усечение develop сохранено")
	assert.Equal(t, 34, moneyToInt(34.0))
}

// накопленная float-погрешность долей бота: 6 операций по 13 ₽ на троих дают
// баланс 25.999999999999996, который старый int() усекал до 25 при точном долге 26
func TestDebtsFloatNoiseRounded(t *testing.T) {
	var ops []api.Operation
	for i := 0; i < 6; i++ {
		ops = append(ops, botEquallySpend(13, moneyUserA, moneyUserA, moneyUserB, moneyUserC))
	}
	debts, err := GetRoomDebts(moneyRoom([]api.User{moneyUserA, moneyUserB, moneyUserC}, ops))
	assert.NoError(t, err)

	assert.Equal(t, 26, findMoneyDebt(debts, moneyUserB.ID, moneyUserA.ID), "долг B→A")
	assert.Equal(t, 26, findMoneyDebt(debts, moneyUserC.ID, moneyUserA.ID), "долг C→A")
}

// 6 операций по 1 ₽ на шестерых: баланс должника 0.9999999999999999 —
// старый int() давал долг 0 и долги молча пропадали из ответа целиком
func TestDebtsSmallFloatSharesNotLost(t *testing.T) {
	members := []api.User{moneyUserA}
	for i := 2; i <= 6; i++ {
		members = append(members, api.User{ID: i})
	}
	var ops []api.Operation
	for i := 0; i < 6; i++ {
		ops = append(ops, botEquallySpend(1, moneyUserA, members...))
	}
	debts, err := GetRoomDebts(moneyRoom(members, ops))
	assert.NoError(t, err)

	assert.Len(t, debts, 5, "по одному долгу от каждого из пяти должников")
	for _, d := range debts {
		assert.Equal(t, 1, d.Sum, "долг %d→%d", d.Debtor.ID, d.Lender.ID)
		assert.Equal(t, moneyUserA.ID, d.Lender.ID)
	}
}

// больше 100 должников: старый жёсткий лимит в 100 итераций молча обрывал список долгов
func TestDebtsManyParticipants(t *testing.T) {
	donor := api.User{ID: 1000, DisplayName: "донор"}
	members := []api.User{donor}
	var recipients []api.User
	for i := 1; i <= 150; i++ {
		u := api.User{ID: i}
		members = append(members, u)
		recipients = append(recipients, u)
	}
	// 1500/150 = ровно 10.0 на каждого
	debts, err := GetRoomDebts(moneyRoom(members, []api.Operation{botEquallySpend(1500, donor, recipients...)}))
	assert.NoError(t, err)

	assert.Len(t, debts, 150, "старый код обрывался на 100 итерациях")
	var total int
	for _, d := range debts {
		total += d.Sum
	}
	assert.Equal(t, 1500, total)
}

// погашения: полное погашение обнуляет долг; частичное уменьшает его ровно
// на сумму возврата (двойной учёт «прямой возврат + общий баланс» из
// develop-алгоритма AddReturnToDebts исправлен — применённый возврат
// списывается из общих балансов)
func TestRepaymentSettlesDebt(t *testing.T) {
	members := []api.User{moneyUserA, moneyUserB}
	spend := botEquallySpend(100, moneyUserA, moneyUserA, moneyUserB)

	debts, err := GetRoomDebts(moneyRoom(members, []api.Operation{spend}))
	assert.NoError(t, err)
	assert.Equal(t, 50, findMoneyDebt(debts, moneyUserB.ID, moneyUserA.ID), "долг до погашения")

	// полное погашение: долга нет
	debts, err = GetRoomDebts(moneyRoom(members, []api.Operation{spend, repayOp(50, moneyUserB, moneyUserA)}))
	assert.NoError(t, err)
	assert.Empty(t, debts, "после полного погашения")

	// частичное погашение 20: долг уменьшается ровно на 20 → 30
	debts, err = GetRoomDebts(moneyRoom(members, []api.Operation{spend, repayOp(20, moneyUserB, moneyUserA)}))
	assert.NoError(t, err)
	assert.Equal(t, 30, findMoneyDebt(debts, moneyUserB.ID, moneyUserA.ID), "частичный возврат учитывается ровно один раз")
}

// GetUserCostsSum: float-доли бота суммируются без потери рубля на погрешности,
// погашения в расходы не входят
func TestGetUserCostsSumFloatNoise(t *testing.T) {
	var ops []api.Operation
	for i := 0; i < 6; i++ {
		ops = append(ops, botEquallySpend(13, moneyUserA, moneyUserA, moneyUserB, moneyUserC))
	}
	ops = append(ops, repayOp(5, moneyUserB, moneyUserA))
	room := moneyRoom([]api.User{moneyUserA, moneyUserB, moneyUserC}, ops)

	repo := fakeMoneyRoomRepo{room: &room}
	ss := NewStatisticService(NewRoomService(repo), NewOperationService(repo))

	// доля B: 6 × 13/3 = 25.999999999999996 → 26 (старый int() давал 25)
	got, err := ss.GetUserCostsSum(context.Background(), moneyUserB.ID, room.ID.Hex())
	assert.NoError(t, err)
	assert.Equal(t, 26, got)
}
