package service

import (
	"context"
	"math"
	"sort"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func NewUserService(r repository.UserRepository) *UserService {
	return &UserService{r}
}

func NewRoomService(r repository.RoomRepository) *RoomService {
	return &RoomService{r}
}

func NewChatStateService(r repository.ChatStateRepository) *ChatStateService {
	return &ChatStateService{r}
}

func NewButtonService(r repository.ButtonRepository) *ButtonService {
	return &ButtonService{r}
}

func NewOperationService(r repository.RoomRepository) *OperationService {
	return &OperationService{r}
}

func NewStatisticService(r *RoomService, s *OperationService) *StatisticService {
	return &StatisticService{*r, *s}
}

func NewRoomStateService(s *OperationService, rr repository.RoomRepository) *RoomStateService {
	return &RoomStateService{rr, *s}
}

func NewLoginCodeService(r repository.LoginCodeRepository) *LoginCodeService {
	return &LoginCodeService{r}
}

func NewBugReportService(r repository.BugReportRepository) *BugReportService {
	return &BugReportService{r}
}

type UserService struct {
	repository.UserRepository
}

type RoomService struct {
	repository.RoomRepository
}

type ChatStateService struct {
	repository.ChatStateRepository
}

type ButtonService struct {
	repository.ButtonRepository
}

type OperationService struct {
	repository.RoomRepository
}

type StatisticService struct {
	RoomService
	OperationService
}

type RoomStateService struct {
	repository.RoomRepository
	OperationService
}

type LoginCodeService struct {
	repository.LoginCodeRepository
}

type BugReportService struct {
	repository.BugReportRepository
}

func (rs *RoomService) CreateRoom(ctx context.Context, r *api.Room) (*api.Room, error) {
	rId, err := rs.RoomRepository.SaveRoom(ctx, r)
	r.ID = rId
	return r, err
}

func (css *ChatStateService) CleanChatState(ctx context.Context, state *api.ChatState) {
	if state == nil {
		return
	} else if err := (*css).DeleteByUserId(ctx, state.UserId); err != nil {
		log.Error().Err(err).Msg("CleanChatState failed")
	}
}

// GetAllOperations отдаёт операции комнаты без архивных: удалённый расход не
// должен возвращаться наружу (в том числе в интеграцию с внешним хостом) — до
// мягкого удаления его в документе просто не было
func (s *OperationService) GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id:%s", roomId)
		return nil, err
	}
	if room.Operations == nil {
		return room.Operations, nil
	}
	ops := make([]api.Operation, 0, len(*room.Operations))
	for _, o := range *room.Operations {
		if o.Status != api.StatusArchive {
			ops = append(ops, o)
		}
	}
	return &ops, nil
}

func (s *OperationService) GetAllDebtOperations(ctx context.Context, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, err
	}
	var debtOperations []api.Operation
	for _, o := range *room.Operations {
		// archive — удалённое погашение: до мягкого удаления его здесь не было,
		// потому что запись вырезали из документа
		if o.IsDebtRepayment && o.Status != api.StatusArchive {
			debtOperations = append(debtOperations, o)
		}
	}
	return &debtOperations, nil
}

func (s *OperationService) GetAllSpendOperations(ctx context.Context, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, err
	}
	var spendOperations []api.Operation
	for _, o := range *room.Operations {
		if !o.IsDebtRepayment && o.Status != "archive" {
			spendOperations = append(spendOperations, o)
		}
	}
	return &spendOperations, nil
}

func (s *OperationService) GetUserSpendOperations(ctx context.Context, userId int, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, err
	}
	var spendUserOperations []api.Operation
	for _, o := range *room.Operations {
		if !o.IsDebtRepayment && o.Donor.ID == userId && o.Status != "archive" {
			spendUserOperations = append(spendUserOperations, o)
		}
	}
	return &spendUserOperations, nil
}

func (s *OperationService) GetUserParticipateInOperations(ctx context.Context, userId int, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, err
	}
	var participateInOperations []api.Operation
	for _, o := range *room.Operations {
		if !o.IsDebtRepayment && containsUserId(o.RecipientsWithSum, userId) && o.Status == "active" {
			participateInOperations = append(participateInOperations, o)
		}
	}
	return &participateInOperations, nil
}

func (s *OperationService) GetUserInvolvedDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error) {
	allDbt, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return nil, err
	}

	var uDbts []api.Debt
	for _, debt := range allDbt {
		if debt.Lender.ID == userId || debt.Debtor.ID == userId {
			uDbts = append(uDbts, debt)
		}
	}
	return &uDbts, nil
}

func (s *OperationService) GetUserDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error) {
	allDbt, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return nil, err
	}

	var uDbts []api.Debt
	for _, debt := range allDbt {
		if debt.Debtor.ID == userId {
			uDbts = append(uDbts, debt)
		}
	}
	return &uDbts, nil
}
func (s *OperationService) GetUserDebt(ctx context.Context, debtorId int, lenderId int, roomId string) (*api.Debt, error) {
	allDbt, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return nil, err
	}

	for _, debt := range allDbt {
		if debt.Debtor.ID == debtorId && debt.Lender.ID == lenderId {
			return &debt, nil
		}
	}
	return nil, nil
}

func (s *OperationService) GetAllDebts(ctx context.Context, roomId string) ([]api.Debt, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil || room == nil {
		log.Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, err
	}

	return GetRoomDebts(*room)
}

func GetRoomDebts(room api.Room) ([]api.Debt, error) {
	idUser := map[int]api.User{}
	for _, user := range *room.Members {
		idUser[user.ID] = user
	}

	var notDebt []api.Operation
	var debtReturn []api.Operation
	for _, op := range *room.Operations {
		if op.Status != "active" {
			continue
		}
		if op.IsDebtRepayment {
			debtReturn = append(debtReturn, op)
		} else {
			notDebt = append(notDebt, op)
		}
	}

	debts, err := calculateDebt(idUser, notDebt)
	if err != nil {
		return nil, err
	}
	sortDebts(debts)

	debts, err = AddReturnToDebts(debts, debtReturn)
	sortDebts(debts)
	return debts, err

}

func sortDebts(debts []api.Debt) {
	sort.Slice(debts, func(i, j int) bool {
		if debts[i].Debtor.ID == debts[j].Debtor.ID {
			return debts[i].Lender.ID < debts[j].Lender.ID
		}
		return debts[i].Debtor.ID < debts[j].Debtor.ID
	})
}

func AddReturnToDebts(debts []api.Debt, debtReturn []api.Operation) ([]api.Debt, error) {
	// Создаем карту для хранения всех возвратов долгов
	// Ключ: ID пользователя, значение: общий баланс возвратов
	returned, err := calculateUserBalance(debtReturn)
	if err != nil {
		return nil, err
	}

	// Создаем карту для хранения возвратов между конкретными пользователями
	// Ключ внешней карты: ID должника, ключ внутренней карты: ID кредитора, значение: сумма возврата
	specificReturns := make(map[int]map[int]float64)

	// Заполняем карту прямых возвратов между должниками и кредиторами
	for _, op := range debtReturn {
		donorID := op.Donor.ID
		for _, recipient := range op.RecipientsWithSum {
			recipientID := recipient.User.ID

			// Проверяем существование внешней карты
			if _, exists := specificReturns[donorID]; !exists {
				specificReturns[donorID] = make(map[int]float64)
			}

			// Донор возвращает деньги получателю
			specificReturns[donorID][recipientID] += recipient.Sum
		}
	}

	var result []api.Debt
	for _, debt := range debts {
		debtorID := debt.Debtor.ID
		lenderID := debt.Lender.ID

		// Проверяем, существует ли прямой возврат от должника к кредитору
		directReturn := float64(0)
		if returns, exists := specificReturns[debtorID]; exists {
			if amount, exists := returns[lenderID]; exists {
				directReturn = amount
			}
		}

		// Если есть прямой возврат, уменьшаем долг.
		// Применённую сумму списываем из общих балансов returned — иначе шаг
		// по балансам ниже вычтет тот же возврат второй раз (долг 50, возврат 20
		// давал 10 вместо 30).
		if directReturn > 0 {
			applied := getMin(directReturn, float64(debt.Sum))
			returned[debtorID] -= applied
			returned[lenderID] += applied
			if directReturn >= float64(debt.Sum) {
				// Долг полностью погашен
				continue
			} else {
				// Уменьшаем долг на сумму возврата
				debt.Sum -= int(directReturn)
			}
		}

		// Также учитываем общий баланс, как в оригинальном алгоритме
		if returned[debtorID] >= 1 && returned[lenderID] < 1 {
			min := getMin(returned[debtorID], -returned[lenderID], float64(debt.Sum))
			returned[debtorID] -= min
			returned[lenderID] += min
			debt.Sum -= int(min)
		}

		// Если после всех расчетов долг все еще существует, добавляем его в результат
		if debt.Sum >= 1 {
			result = append(result, debt)
		}
	}

	// Возвраты могут превышать расчётные долги пары: должник по тратам вернул
	// больше, чем жадная развёртка ему насчитала (например, потом снова платил
	// за всех). Раньше такой излишек просто выбрасывался с записью в лог, и
	// переплатившему «никто ничего не должен». Теперь остатки балансов
	// превращаются в долги в обратную сторону: положительный остаток — этому
	// пользователю должны, отрицательный — он должен вернуть.
	users := map[int]api.User{}
	for _, op := range debtReturn {
		users[op.Donor.ID] = *op.Donor
		for _, r := range op.RecipientsWithSum {
			users[r.User.ID] = r.User
		}
	}
	// Остатки в пределах рубля не превращаем в долги: усечение копеек при
	// делении долей (100/3 и т.п.) накапливает у участника до рубля пыли,
	// неотличимой по значению от честной переплаты в 1 ₽. Осознанно жертвуем
	// максимум рублём на участника, чтобы не показывать людям долги «верни 1 ₽»
	// из округлений — develop в этой ситуации выбрасывал переплату целиком
	var leftover []*UserBalance
	for uid, b := range returned {
		if b > 1 || b < -1 {
			leftover = append(leftover, &UserBalance{user: users[uid], balance: b})
		}
	}
	result = append(result, settleBalances(leftover)...)

	for _, ub := range leftover {
		if ub.balance > 5 {
			log.Printf("cannot calculate debts, sum is %f", ub.balance)
		}
	}
	return mergeDebtPairs(result), nil
}

// mergeDebtPairs суммирует долги с одинаковой парой должник-кредитор: развёртка
// остатков возвратов может выдать пару, которая уже есть в списке из трат
// (например, кредитор перевёл деньги своему же должнику операцией возврата)
func mergeDebtPairs(debts []api.Debt) []api.Debt {
	type pair struct{ debtorID, lenderID int }
	seen := map[pair]int{}
	var merged []api.Debt
	for _, d := range debts {
		p := pair{d.Debtor.ID, d.Lender.ID}
		if i, ok := seen[p]; ok {
			merged[i].Sum += d.Sum
			continue
		}
		seen[p] = len(merged)
		merged = append(merged, d)
	}
	return merged
}

func getMin(f ...float64) float64 {
	min := f[0]
	for _, v := range f {
		if v < min {
			min = v
		}
	}
	return min
}

func isUserBalanceValid(userBalance map[int]float64) bool {
	var sum float64
	for _, ub := range userBalance {
		sum += ub
	}
	return sum < 1
}

func calculateDebt(users map[int]api.User, ops []api.Operation) ([]api.Debt, error) {

	balance, err := calculateUserBalance(ops)
	if err != nil {
		return nil, err
	}

	var usrBl []*UserBalance
	for uid, b := range balance {
		usrBl = append(usrBl, &UserBalance{user: users[uid], balance: b})
	}

	return settleBalances(usrBl), nil
}

// settleBalances жадно сводит балансы к нулю: самый крупный кредитор получает
// от самого крупного должника, пока есть кому платить. Балансы в usrBl
// обнуляются по ходу работы.
func settleBalances(usrBl []*UserBalance) []api.Debt {
	var debts []api.Debt
	// лимит итераций — от числа участников, а не константа 100: каждая итерация
	// обнуляет баланс хотя бы одного из двух участников, поэтому шагов не больше
	// len(usrBl); жёсткие 100 молча обрывали список долгов в больших комнатах
	for i := 0; hasDebt(usrBl) && i < len(usrBl); i++ {
		sort.Slice(usrBl, func(i, j int) bool {
			if usrBl[i].balance > usrBl[j].balance {
				return true
			} else if usrBl[i].balance == usrBl[j].balance {
				return usrBl[i].user.ID > usrBl[j].user.ID
			}
			return false
		})
		// платить некому: не осталось отрицательного баланса хотя бы на рубль
		// (с учётом float-шума долей) — без этой проверки repayment спарил бы
		// кредитора с самим собой
		if moneyToInt(-usrBl[len(usrBl)-1].balance) < 1 {
			break
		}
		debt := repayment(usrBl[0], usrBl[len(usrBl)-1])
		if debt.Sum != 0 {
			debts = append(debts, debt)
		}
	}
	return debts
}

func calculateUserBalance(ops []api.Operation) (map[int]float64, error) {
	balance := map[int]float64{}
	for _, op := range ops {
		balance[op.Donor.ID] += float64(op.Sum)
		for _, recipient := range op.RecipientsWithSum {
			balance[recipient.User.ID] -= recipient.Sum
		}
		//на время тестов оставил
		if !isUserBalanceValid(balance) {
			return nil, errors.New("cannot calculate debts")
		}
	}
	return balance, nil
}

func repayment(lender *UserBalance, debtor *UserBalance) api.Debt {
	var sum float64
	if lender.balance < -debtor.balance {
		sum = lender.balance
	} else {
		sum = -debtor.balance
	}

	lender.balance -= sum
	debtor.balance += sum

	return api.Debt{Lender: &lender.user, Debtor: &debtor.user, Sum: moneyToInt(sum)}
}

func hasDebt(balance []*UserBalance) bool {
	for _, b := range balance {
		if b.balance >= 1 {
			return true
		}
	}
	return false
}

type UserBalance struct {
	user    api.User
	balance float64
}

func (s *StatisticService) GetAllCostsSum(ctx context.Context, roomId string) (int, error) {
	room, err := s.RoomService.FindById(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var totalSpendSum int
	for _, v := range *room.Operations {
		if v.Status == "active" && !v.IsDebtRepayment {
			totalSpendSum += v.Sum
		}
	}
	return totalSpendSum, nil
}

func (s *StatisticService) GetUserCostsSum(ctx context.Context, userId int, roomId string) (int, error) {
	room, err := s.RoomService.FindById(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var totalUserSpendSum float64
	for _, v := range *room.Operations {
		if v.Status == "active" && !v.IsDebtRepayment && containsUserId(v.RecipientsWithSum, userId) {
			for _, r := range v.RecipientsWithSum {
				if r.User.ID == userId {
					totalUserSpendSum += r.Sum
				}
			}
		}
	}
	return moneyToInt(totalUserSpendSum), nil
}

// moneyToInt переводит float-сумму в целые рубли, поглощая накопленную
// float-погрешность: у операций бота в recipients_with_sum лежат дробные доли
// (100/3 = 33.33…), и шесть операций по 13 ₽ на троих давали баланс 25.999999…,
// который int() усекал до 25 при точном долге 26. Честные дробные остатки
// (0.6 ₽) по-прежнему усекаются вниз — семантика develop сохранена
func moneyToInt(f float64) int {
	r := math.Round(f)
	if math.Abs(f-r) < 1e-6 {
		return int(r)
	}
	return int(f)
}

func (s *StatisticService) GetAllDebtsSum(ctx context.Context, roomId string) (int, error) {
	debts, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var allDebtsSum int
	for _, v := range debts {
		allDebtsSum += v.Sum
	}
	return allDebtsSum, nil
}

func (s *StatisticService) GetUserDebtAndLendSum(ctx context.Context, userId int, roomId string) (debt int, lent int, e error) {
	debts, err := s.GetUserInvolvedDebts(ctx, userId, roomId)
	if err != nil {
		return 0, 0, err
	}
	var debtorSum int
	var lenderSum int
	for _, v := range *debts {
		if v.Debtor.ID == userId {
			debtorSum += v.Sum
		}
		if v.Lender.ID == userId {
			lenderSum += v.Sum
		}
	}
	return debtorSum, lenderSum, nil
}

func containsUserId(users []api.RecipientWithSum, id int) bool {
	for _, u := range users {
		if u.User.ID == id {
			return true
		}
	}
	return false
}

func (s RoomStateService) DefinePaidOfDebtsUserIdsAndSave(ctx context.Context, room *api.Room) error {
	if len(*room.Members) == len(room.RoomStates.FinishedAddOperation) {
		debts, err := s.OperationService.GetAllDebts(ctx, room.ID.Hex())
		if err != nil {
			log.Error().Err(err).Msgf("cannot get debts")
			return err
		}
		for _, v := range debts {
			if v.Sum != 0 {
				*room.Members = deleteUser(*room.Members, v.Debtor.ID)
			}
		}
		var paidOfDebtsUserIds []int
		for _, user := range *room.Members {
			paidOfDebtsUserIds = append(paidOfDebtsUserIds, user.ID)
		}
		err = s.RoomRepository.PaidOfDebts(ctx, paidOfDebtsUserIds, room.ID.Hex())
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteUser(users []api.User, userId int) []api.User {
	index := -1
	for i, v := range users {
		if v.ID == userId {
			index = i
			break
		}
	}
	if index == -1 {
		return users
	}
	copy(users[index:], users[index+1:])
	return users[:len(users)-1]
}
