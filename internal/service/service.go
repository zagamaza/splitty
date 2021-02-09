package service

import (
	"context"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"sort"
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

func (rs *RoomService) CreateRoom(ctx context.Context, r *api.Room) (*api.Room, error) {
	rId, err := rs.RoomRepository.SaveRoom(ctx, r)
	r.ID = rId
	return r, err
}

func (css *ChatStateService) CleanChatState(ctx context.Context, state *api.ChatState) {
	if state == nil {
		return
	} else if err := (*css).DeleteByUserId(ctx, state.UserId); err != nil {
		log.Error().Err(err).Msg("")
	}
}

func (s *OperationService) GetAllOperations(ctx context.Context, roomId string) (*[]api.Operation, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id:", roomId)
		return nil, err
	}
	return room.Operations, nil
}

func (s *OperationService) GetAllUsersDebts(ctx context.Context, userId int, roomId string) (*[]api.Debt, error) {
	allDbt, err := s.GetAllDebts(ctx, roomId)
	if err != nil {
		return nil, err
	}

	var uDbts []api.Debt
	for _, debt := range *allDbt {
		if debt.Lender.ID == userId || debt.Debtor.ID == userId {
			uDbts = append(uDbts, debt)
		}
	}
	return &uDbts, nil
}

func (s *OperationService) GetAllDebts(ctx context.Context, roomId string) (*[]api.Debt, error) {
	room, err := s.RoomRepository.FindById(ctx, roomId)
	if err != nil {
		log.Err(err).Msgf("cannot find room id:", roomId)
		return nil, err
	}

	idUser := map[int]api.User{}
	for _, user := range *room.Members {
		idUser[user.ID] = user
	}

	userBalance := map[int]float64{}
	for _, op := range *room.Operations {
		userBalance[op.Donor.ID] += float64(op.Sum)
		for _, user := range *op.Recipients {
			userBalance[user.ID] -= float64(op.Sum) / float64(len(*op.Recipients))
		}
		//на время тестов оставил
		if !isUserBalanceValid(userBalance) {
			return nil, errors.New("cannot calculate debts")
		}
	}

	log.Debug().Msgf("%v", userBalance)

	return calculateDebt(idUser, userBalance), nil

}

func isUserBalanceValid(userBalance map[int]float64) bool {
	var sum float64
	for _, ub := range userBalance {
		sum += ub
	}
	return sum < 1
}

func calculateDebt(users map[int]api.User, balance map[int]float64) *[]api.Debt {
	var usrBl []*UserBalance
	for uid, b := range balance {
		usrBl = append(usrBl, &UserBalance{user: users[uid], balance: b})
	}

	var debts []api.Debt
	for i := 0; hasDebt(usrBl) && i < 100; i++ {
		log.Debug().Msg("iter")
		sort.Slice(usrBl, func(i, j int) bool {
			return usrBl[i].balance > usrBl[j].balance
		})
		debts = append(debts, repayment(usrBl[0], usrBl[len(usrBl)-1]))
	}
	return &debts
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

	return api.Debt{Lender: &lender.user, Debtor: &debtor.user, Sum: int(sum)}
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
