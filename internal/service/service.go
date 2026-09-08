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
	// Шаг тусы — это и есть порог «долг или пыль». В рублёвой тусе 70 копеек
	// отдать нечем: там нет ни ввода, ни показа дробей, и такой остаток обязан
	// схлопнуться, а не превратиться в «должен 1 ₽». В тусе с копейками шаг
	// равен минорной единице, и точный долг доживает до ответа целиком.
	//
	// Прежний код сравнивал деньги с рублём ВСЮДУ и на float — отсюда и «до
	// рубля пыли», которую приходилось прощать. Здесь порог один, он явный и
	// зависит от самой тусы.
	step := api.ShareStepFor(api.RoomFractional(&room))

	// Движок считает по минорным полям, а у операций бота их в документе нет.
	// Достраиваем здесь, а не полагаемся на вызывающего: репозиторий это делает,
	// но комната приходит и другими путями (напоминания, тесты), и молча
	// посчитать долги по старым дробным полям — это разойтись с тем, что
	// показано в самом расходе. Повторный вызов ничего не меняет: значения те же.
	api.FillRoomMoney(&room)

	idUser := map[int]api.User{}
	for _, user := range *room.Members {
		idUser[user.ID] = user
	}

	// ⚠️ Погашения идут в ОДНУ кучу с расходами, а не вычитаются из готовых
	// долгов. Погашение — это тот же расход, только участники переставлены:
	// должник выступает донором и получает +sum, кредитор единственным
	// получателем и получает -sum. Вектор выходит противоположным исходному
	// расходу именно из-за перестановки, отдельная арифметика для этого не нужна.
	//
	// Прежняя схема строила долги по одним расходам, а потом вычитала погашения
	// по парам «должник-кредитор». Но пары в жадной развёртке произвольны: это
	// один из многих законных маршрутов расчёта, а не история переводов. Если
	// пара не совпадала с теми, между кем реально был перевод, погашение не
	// засчитывалось, оставалось в остатках и разворачивалось во ВСТРЕЧНЫЙ долг.
	// На проде это давало комнаты, где люди рассчитались, а долги появлялись
	// заново: 0 → 22 ₽ в «Нг 2026», 1 долг на 397 ₽ → 17 долгов на 1208 ₽
	// в «Тимбилдинге». Одна развёртка в конце от порядка пар не зависит.
	var ops []api.Operation
	for _, op := range *room.Operations {
		if op.Status != api.StatusActive {
			continue
		}
		ops = append(ops, op)
		// Участник мог выйти из комнаты, оставшись в операции. Без него в
		// справочнике долг вышел бы на пользователя с нулевым id.
		if op.Donor != nil {
			if _, ok := idUser[op.Donor.ID]; !ok {
				idUser[op.Donor.ID] = *op.Donor
			}
		}
		for _, r := range op.RecipientsWithSum {
			if _, ok := idUser[r.User.ID]; !ok {
				idUser[r.User.ID] = r.User
			}
		}
	}

	debts, err := calculateDebt(idUser, ops, step)
	if err != nil {
		return nil, err
	}
	sortDebts(debts)
	return debts, nil
}

func sortDebts(debts []api.Debt) {
	sort.Slice(debts, func(i, j int) bool {
		if debts[i].Debtor.ID == debts[j].Debtor.ID {
			return debts[i].Lender.ID < debts[j].Lender.ID
		}
		return debts[i].Debtor.ID < debts[j].Debtor.ID
	})
}

// isUserBalanceValid — предохранитель на легаси-данных. Балансы участников
// обязаны сходиться в ноль: каждая операция добавляет донору ровно столько,
// сколько снимает с получателей. Ненулевая сумма означает, что доли в документе
// не сходятся с итогом и FillMoney не смог вывести их заново, — на таких данных
// долги не считаются вовсе (debtsUnavailable), а не считаются «примерно».
//
// Прежнее `sum < 1` было допуском на float-шум; в целых минорных допуск не нужен.
func isUserBalanceValid(userBalance map[int]int64) bool {
	var sum int64
	for _, ub := range userBalance {
		var overflow bool
		if sum, overflow = addChecked(sum, ub); overflow {
			return false
		}
	}
	return sum == 0
}

// negChecked меняет знак, сообщая о невозможности: у math.MinInt64
// противоположного значения в int64 нет, и unary minus вернул бы его же.
func negChecked(v int64) (int64, bool) {
	if v == math.MinInt64 {
		return 0, false
	}
	return -v, true
}

// addChecked складывает минорные единицы, сообщая о переполнении вместо тихого
// заворота по кругу.
func addChecked(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, true
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, true
	}
	return a + b, false
}

func calculateDebt(users map[int]api.User, ops []api.Operation, step int64) ([]api.Debt, error) {

	balance, err := calculateUserBalance(ops)
	if err != nil {
		return nil, err
	}

	var usrBl []*UserBalance
	for uid, b := range balance {
		usrBl = append(usrBl, &UserBalance{user: users[uid], balance: b})
	}

	return settleBalances(usrBl, step), nil
}

// settleBalances жадно сводит балансы к нулю: самый крупный кредитор получает
// от самого крупного должника, пока есть кому платить. Балансы в usrBl
// обнуляются по ходу работы.
func settleBalances(usrBl []*UserBalance, step int64) []api.Debt {
	var debts []api.Debt
	// лимит итераций — от числа участников, а не константа 100: каждая итерация
	// обнуляет баланс хотя бы одного из двух участников, поэтому шагов не больше
	// len(usrBl); жёсткие 100 молча обрывали список долгов в больших комнатах
	for i := 0; hasDebt(usrBl, step) && i < len(usrBl); i++ {
		sort.Slice(usrBl, func(i, j int) bool {
			if usrBl[i].balance > usrBl[j].balance {
				return true
			} else if usrBl[i].balance == usrBl[j].balance {
				return usrBl[i].user.ID > usrBl[j].user.ID
			}
			return false
		})
		// платить некому: отрицательного баланса не осталось вовсе — без этой
		// проверки repayment спарил бы кредитора с самим собой. Прежний порог в
		// рубль скрывал float-шум долей; на точных минорных единицах шума нет.
		if negated, ok := negChecked(usrBl[len(usrBl)-1].balance); !ok || negated < step {
			break
		}
		debt := repayment(usrBl[0], usrBl[len(usrBl)-1], step)
		// По ТОЧНОЙ величине, а не по округлённой проекции: в тусе с копейками
		// долг в одну копейку проецируется в ноль рублей, и проверка по Sum
		// выбрасывала бы его.
		if debt.SumMinor >= step {
			debts = append(debts, debt)
		}
	}
	return debts
}

// calculateUserBalance считает балансы в минорных единицах: сколько человек
// внёс сверх того, что на него записано. Источник — минорные поля операции
// (FillMoney достраивает их на чтении), а не старые дробные.
//
// Сложения проверяемые: испорченное записанное sum_minor способно завернуть
// баланс по кругу и ложно пройти проверку схождения — тогда люди увидят долги,
// выведенные из мусора. Финансовому ядру лучше отказаться считать.
func calculateUserBalance(ops []api.Operation) (map[int]int64, error) {
	balance := map[int]int64{}
	for _, op := range ops {
		var overflow bool
		if balance[op.Donor.ID], overflow = addChecked(balance[op.Donor.ID], op.SumMinorOrLegacy()); overflow {
			return nil, errors.New("cannot calculate debts: money value out of range")
		}
		for _, recipient := range op.RecipientsWithSum {
			id := recipient.User.ID
			share, ok := negChecked(recipient.SumMinorOrLegacy())
			if !ok {
				return nil, errors.New("cannot calculate debts: money value out of range")
			}
			if balance[id], overflow = addChecked(balance[id], share); overflow {
				return nil, errors.New("cannot calculate debts: money value out of range")
			}
		}
		//на время тестов оставил
		if !isUserBalanceValid(balance) {
			return nil, errors.New("cannot calculate debts")
		}
	}
	return balance, nil
}

func repayment(lender *UserBalance, debtor *UserBalance, step int64) api.Debt {
	// Смена знака ДО сложения: у math.MinInt64 противоположного значения нет, и
	// unary minus вернул бы его же, обойдя проверку переполнения ниже. Такой
	// баланс приходит только из испорченного документа, но денежное ядро не
	// должно на нём считать.
	owed, ok := negChecked(debtor.balance)
	if !ok {
		return api.Debt{Lender: &lender.user, Debtor: &debtor.user}
	}
	sum := min(lender.balance, owed)

	// ⚠️ Долг обязан быть КРАТЕН шагу тусы. В рублёвой тусе долг 50,50 отдать
	// нечем: бот и старые сборки шлют целое 51 — больше долга, а новые шлют
	// точные 50,50 — дробную сумму, которую рублёвая туса не принимает. Долг,
	// который не может погасить ни один клиент, хуже, чем недосведённые
	// полтинники.
	//
	// Остаток остаётся в балансах и до долга не дорастает: порог в развёртке
	// отсеивает всё меньше шага. Ровно эта семантика была у develop, где
	// усечение делал moneyToInt.
	sum -= sum % step

	lender.balance -= sum
	debtor.balance += sum

	return api.NewDebt(&lender.user, &debtor.user, sum)
}

// hasDebt — есть ли кому платить. Порог — шаг тусы: в тусе с копейками долгом
// считается и одна копейка, в рублёвой — только целый рубль.
func hasDebt(balance []*UserBalance, step int64) bool {
	for _, b := range balance {
		if b.balance >= step {
			return true
		}
	}
	return false
}

type UserBalance struct {
	user api.User
	// balance — минорные единицы: положительный означает «внёс больше, чем на
	// него записано», то есть ему должны.
	balance int64
}

func (s *StatisticService) GetAllCostsSum(ctx context.Context, roomId string) (int, error) {
	room, err := s.RoomService.FindById(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var totalSpendSum int64
	for _, v := range *room.Operations {
		if v.Status == "active" && !v.IsDebtRepayment {
			totalSpendSum += v.SumMinorOrLegacy()
		}
	}
	return api.FromMinor(totalSpendSum), nil
}

func (s *StatisticService) GetUserCostsSum(ctx context.Context, userId int, roomId string) (int, error) {
	room, err := s.RoomService.FindById(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var totalUserSpendSum int64
	for _, v := range *room.Operations {
		if v.Status == "active" && !v.IsDebtRepayment && containsUserId(v.RecipientsWithSum, userId) {
			for _, r := range v.RecipientsWithSum {
				if r.User.ID == userId {
					totalUserSpendSum += r.SumMinorOrLegacy()
				}
			}
		}
	}
	return api.FromMinor(totalUserSpendSum), nil
}

func (s *StatisticService) GetAllDebtsSum(ctx context.Context, roomId string) (int, error) {
	debts, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return 0, err
	}
	var allDebtsSum int64
	for _, v := range debts {
		allDebtsSum += v.SumMinor
	}
	return api.FromMinor(allDebtsSum), nil
}

func (s *StatisticService) GetUserDebtAndLendSum(ctx context.Context, userId int, roomId string) (debt int, lent int, e error) {
	debts, err := s.GetUserInvolvedDebts(ctx, userId, roomId)
	if err != nil {
		return 0, 0, err
	}
	var debtorSum int64
	var lenderSum int64
	for _, v := range *debts {
		if v.Debtor.ID == userId {
			debtorSum += v.SumMinor
		}
		if v.Lender.ID == userId {
			lenderSum += v.SumMinor
		}
	}
	return api.FromMinor(debtorSum), api.FromMinor(lenderSum), nil
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
			if v.SumMinor != 0 {
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
