package rest

import (
	"math"
	"sort"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

// Статусы и типы деления операций develop-модели.
// Константы в internal/bot приватные, поэтому REST заводит свои
const (
	statusDraft   api.OperationStatus = "draft"
	statusActive  api.OperationStatus = "active"
	statusArchive api.OperationStatus = "archive"

	splitEqually       api.SplitType = "equally"
	splitByExactAmount api.SplitType = "by_exact_amount"
)

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type userDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type meDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Lang        string `json:"lang"`
	// LinkedProviders — привязанные способы входа ("telegram", "google",
	// "apple"): по ним клиент рисует экран «Способы входа» и понимает, какой
	// отвязать нельзя (последний). Наружу отдаётся только ФАКТ привязки —
	// сами идентификаторы личности остаются в базе
	LinkedProviders []string `json:"linkedProviders"`
	NotificationOn  bool     `json:"notificationOn"`
}

type fileDto struct {
	Type   string `json:"type"`
	FileId string `json:"fileId"`
}

// operationRecipientDto доля получателя в операции: sum — целые рубли
type operationRecipientDto struct {
	User userDto `json:"user"`
	Sum  int     `json:"sum"`
}

// itemShareDto доля участника в позиции (read-path)
type itemShareDto struct {
	UserId int  `json:"userId"`
	Weight int  `json:"weight"`
	Amount *int `json:"amount,omitempty"`
}

// operationItemDto позиция чека в ответе API (read-path). Отдаётся только для
// itemized-операций (AI-распознанных); nil для обычных
type operationItemDto struct {
	Name    string         `json:"name"`
	Price   int            `json:"price"`
	Qty     int            `json:"qty"`
	Shares  []itemShareDto `json:"shares,omitempty"`
	Kind    string         `json:"kind"`
	Split   string         `json:"split,omitempty"`
	Percent *int           `json:"percent,omitempty"`
}

type operationDto struct {
	ID              string                  `json:"id"`
	Description     string                  `json:"description"`
	Sum             int                     `json:"sum"`
	IsDebtRepayment bool                    `json:"isDebtRepayment"`
	SplitType       string                  `json:"splitType,omitempty"`
	Donor           userDto                 `json:"donor"`
	Recipients      []operationRecipientDto `json:"recipients"`
	CreatedAt       time.Time               `json:"createdAt"`
	Files           []fileDto               `json:"files,omitempty"`
	// Items детализация по позициям чека (только itemized-операции); nil у обычных.
	// Плоские Recipients выше — источник для долгов; Items — «зачем так вышло»
	Items []operationItemDto `json:"items,omitempty"`
	// ClientOpId клиентский идемпотентный ключ (см. docs/API.md «Идемпотентность»):
	// по нему офлайн-клиент сопоставляет локальные операции outbox с серверными
	ClientOpId string `json:"clientOpId,omitempty"`
}

type debtDto struct {
	Debtor userDto `json:"debtor"`
	Lender userDto `json:"lender"`
	Sum    int     `json:"sum"`
}

type roomSummaryDto struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	IsArchived  bool      `json:"isArchived"`
	Currency    string    `json:"currency"`
	Members     []userDto `json:"members"`
	MemberCount int       `json:"memberCount"`
	TotalSpent  int       `json:"totalSpent"`
	MyBalance   int       `json:"myBalance"`
	// DebtsUnavailable true — долги комнаты не считаются на легаси-данных
	// (см. roomDebtsSafe): myBalance отдан нулём, клиент может показать бейдж
	DebtsUnavailable bool `json:"debtsUnavailable,omitempty"`
}

type roomDetailDto struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	IsArchived bool      `json:"isArchived"`
	Currency   string    `json:"currency"`
	Members    []userDto `json:"members"`
	TotalSpent int       `json:"totalSpent"`
	MySpent    int       `json:"mySpent"`
	MyBalance  int       `json:"myBalance"`
	Debts      []debtDto `json:"debts"`
	// DebtsUnavailable true — долги комнаты не считаются на легаси-данных
	// (см. roomDebtsSafe): debts=[] и myBalance=0, остальное поле комнаты
	// (операции, участники, траты) отдаётся как обычно
	DebtsUnavailable bool           `json:"debtsUnavailable,omitempty"`
	Operations       []operationDto `json:"operations"`
}

type friendRoomBalanceDto struct {
	RoomId   string `json:"roomId"`
	RoomName string `json:"roomName"`
	Currency string `json:"currency"`
	Balance  int    `json:"balance"`
}

// currencySumDto итог по одной валюте — суммы в разных валютах не складываются
type currencySumDto struct {
	Currency string `json:"currency"`
	Sum      int    `json:"sum"`
}

type friendBalanceDto struct {
	User             userDto                `json:"user"`
	TotalsByCurrency []currencySumDto       `json:"totalsByCurrency"`
	Rooms            []friendRoomBalanceDto `json:"rooms"`
}

type activityItemDto struct {
	RoomId       string       `json:"roomId"`
	RoomName     string       `json:"roomName"`
	RoomCurrency string       `json:"roomCurrency"`
	Operation    operationDto `json:"operation"`
}

// currencyInfoDto запись справочника валют для пикера в приложении
type currencyInfoDto struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
	Flag   string `json:"flag"`
}

// dailySumDto траты одного календарного дня (date — ISO-дата yyyy-mm-dd)
type dailySumDto struct {
	Date string `json:"date"`
	Sum  int    `json:"sum"`
}

// monthlySumDto траты одного календарного месяца (month — "yyyy-mm")
type monthlySumDto struct {
	Month string `json:"month"`
	Sum   int    `json:"sum"`
}

// memberSumDto сумма по участнику комнаты
type memberSumDto struct {
	User userDto `json:"user"`
	Sum  int     `json:"sum"`
}

// topOperationDto строка топа операций по сумме
type topOperationDto struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Sum         int       `json:"sum"`
	Donor       userDto   `json:"donor"`
	CreatedAt   time.Time `json:"createdAt"`
}

// statisticsDto расширенная статистика комнаты для дашборда:
// только active-расходы (погашения исключены), деньги — целые рубли
type statisticsDto struct {
	Currency       string            `json:"currency"`
	TotalSpent     int               `json:"totalSpent"`
	OperationCount int               `json:"operationCount"`
	MonthSpent     int               `json:"monthSpent"`
	ByDay          []dailySumDto     `json:"byDay"`
	ByMonth        []monthlySumDto   `json:"byMonth"`
	PaidByMember   []memberSumDto    `json:"paidByMember"`
	ShareByMember  []memberSumDto    `json:"shareByMember"`
	TopOperations  []topOperationDto `json:"topOperations"`
}

type authResponseDto struct {
	Token string `json:"token"`
	User  meDto  `json:"user"`
}

func toUserDto(u *api.User) userDto {
	if u == nil {
		return userDto{}
	}
	displayName := u.DisplayName
	if displayName == "" {
		displayName = u.Username
	}
	return userDto{ID: u.ID, Username: u.Username, DisplayName: displayName}
}

func toUserDtos(users []api.User) []userDto {
	dtos := make([]userDto, 0, len(users))
	for i := range users {
		dtos = append(dtos, toUserDto(&users[i]))
	}
	return dtos
}

func toMeDto(u *api.User) meDto {
	return meDto{
		ID:              u.ID,
		Username:        u.Username,
		DisplayName:     toUserDto(u).DisplayName,
		Lang:            api.DefineLang(u),
		LinkedProviders: linkedProviders(u),
		NotificationOn:  u.NotificationOn == nil || *u.NotificationOn,
	}
}

// toOperationDto маппит НОРМАЛИЗОВАННУЮ операцию (см. normalizedOperation)
func toOperationDto(o *api.Operation) operationDto {
	recipients := make([]operationRecipientDto, 0, len(o.RecipientsWithSum))
	for i := range o.RecipientsWithSum {
		recipients = append(recipients, operationRecipientDto{
			User: toUserDto(&o.RecipientsWithSum[i].User),
			Sum:  recipientShare(o, i),
		})
	}
	dto := operationDto{
		ID:              o.ID.Hex(),
		Description:     o.Description,
		Sum:             o.Sum,
		IsDebtRepayment: o.IsDebtRepayment,
		SplitType:       operationSplitType(o),
		Donor:           toUserDto(o.Donor),
		Recipients:      recipients,
		CreatedAt:       o.CreateAt,
		ClientOpId:      o.ClientOpId,
	}
	for _, f := range o.Files {
		dto.Files = append(dto.Files, fileDto{Type: string(f.Type), FileId: f.FileId})
	}
	for _, it := range o.Items {
		shares := make([]itemShareDto, 0, len(it.Shares))
		for _, s := range it.Shares {
			shares = append(shares, itemShareDto{UserId: s.UserId, Weight: s.Weight, Amount: s.Amount})
		}
		// Легаси-документы лежат в базе с пустым kind. Отдавать "" наружу
		// нельзя: клиент честно вернёт его в PUT, а валидация kind ответит
		// 400 «неизвестный тип позиции» — операция становится неправимой.
		// Нормализуем к каноническому виду на чтении.
		kind := it.Kind
		if kind == "" {
			kind = api.ItemKindItem
		}
		split := it.Split
		if kind == api.ItemKindSurcharge && split == "" {
			split = api.SplitProportional
		}
		dto.Items = append(dto.Items, operationItemDto{
			Name:    it.Name,
			Price:   it.Price,
			Qty:     it.Qty,
			Shares:  shares,
			Kind:    string(kind),
			Split:   string(split),
			Percent: it.Percent,
		})
	}
	return dto
}

// operationSplitType значение splitType для DTO: у погашений его нет,
// расходы без split_type (легаси master-2021) — equally
func operationSplitType(o *api.Operation) string {
	if o.IsDebtRepayment {
		return ""
	}
	if o.SplitType == "" {
		return string(splitEqually)
	}
	return string(o.SplitType)
}

// recipientShare доля получателя с индексом i в целых рублях (контракт API):
// для погашений и by_exact_amount — хранимое значение (всегда целочисленный float);
// для equally (и легаси) — канонический пересчёт api.ShareOf по порядку массива,
// потому что бот develop хранит дробные float-доли (100/3 = 33.33…), которые
// нельзя просто привести к int
func recipientShare(o *api.Operation, i int) int {
	if o.IsDebtRepayment || o.SplitType == splitByExactAmount {
		return int(math.Round(o.RecipientsWithSum[i].Sum))
	}
	return api.ShareOf(o.Sum, len(o.RecipientsWithSum), i)
}

// toOperationDtos маппит операции, новые первыми
func toOperationDtos(ops []api.Operation) []operationDto {
	sorted := make([]api.Operation, len(ops))
	copy(sorted, ops)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreateAt.After(sorted[j].CreateAt)
	})
	dtos := make([]operationDto, 0, len(sorted))
	for i := range sorted {
		dtos = append(dtos, toOperationDto(&sorted[i]))
	}
	return dtos
}

func toDebtDtos(debts []api.Debt) []debtDto {
	dtos := make([]debtDto, 0, len(debts))
	for _, d := range debts {
		dtos = append(dtos, debtDto{Debtor: toUserDto(d.Debtor), Lender: toUserDto(d.Lender), Sum: d.Sum})
	}
	return dtos
}

// usersOf nil-безопасно разворачивает указатель на слайс пользователей
func usersOf(users *[]api.User) []api.User {
	if users == nil {
		return nil
	}
	return *users
}

// roomMembers nil-безопасно возвращает участников комнаты
func roomMembers(r *api.Room) []api.User {
	if r == nil {
		return nil
	}
	return usersOf(r.Members)
}

// roomOperations nil-безопасно возвращает операции комнаты
func roomOperations(r *api.Room) []api.Operation {
	if r == nil || r.Operations == nil {
		return nil
	}
	return *r.Operations
}

// normalizedOperation приводит операцию к модели develop, НЕ мутируя оригинал
// (работает с копией): легаси-операции эпохи master-2021 — без status и без
// recipients_with_sum — считаются активными, а их доли синтезируются канонически
// из легаси-поля recipients (поровну, остаток первым по порядку массива).
// Активная операция без получателей (битые данные) исключается как draft —
// иначе она валила бы весь расчёт долгов комнаты
func normalizedOperation(o api.Operation) api.Operation {
	if o.Status == "" {
		o.Status = statusActive
	}
	if len(o.RecipientsWithSum) == 0 && o.Recipients != nil && len(*o.Recipients) > 0 {
		recipients := *o.Recipients
		withSum := make([]api.RecipientWithSum, 0, len(recipients))
		for i := range recipients {
			withSum = append(withSum, api.RecipientWithSum{
				User: recipients[i],
				Sum:  float64(api.ShareOf(o.Sum, len(recipients), i)),
			})
		}
		o.RecipientsWithSum = withSum
	}
	if o.Status == statusActive && len(o.RecipientsWithSum) == 0 {
		o.Status = statusDraft
	}
	return o
}

// activeOperations возвращает нормализованные АКТИВНЫЕ операции комнаты.
// REST работает только с ними: драфты бота и архивные версии отредактированных
// операций не показываются и не участвуют в долгах/статистике (как в
// GetRoomDebts develop). База при нормализации не мутируется
func activeOperations(r *api.Room) []api.Operation {
	var ops []api.Operation
	for _, o := range roomOperations(r) {
		n := normalizedOperation(o)
		if n.Status == statusActive {
			ops = append(ops, n)
		}
	}
	return ops
}

// normalizedRoom копия комнаты с нормализованными активными операциями —
// вход для расчёта долгов develop (service.GetRoomDebts)
func normalizedRoom(r *api.Room) api.Room {
	ops := activeOperations(r)
	if ops == nil {
		ops = []api.Operation{}
	}
	members := roomMembers(r)
	return api.Room{ID: r.ID, Name: r.Name, Members: &members, Operations: &ops}
}

// roomCurrencyCode валюта комнаты для API: пустая строка в базе
// (комнаты до выбора валюты) отдаётся историческим дефолтом RUB
func roomCurrencyCode(r *api.Room) string {
	if r == nil || r.Currency == "" {
		return api.DefaultCurrency
	}
	return r.Currency
}

// isRoomMember проверяет членство пользователя в комнате
func isRoomMember(r *api.Room, userId int) bool {
	for _, m := range roomMembers(r) {
		if m.ID == userId {
			return true
		}
	}
	return false
}

// isRoomArchived проверяет, заархивирована ли комната для пользователя
func isRoomArchived(r *api.Room, userId int) bool {
	for _, id := range r.RoomStates.Archived {
		if id == userId {
			return true
		}
	}
	return false
}

// roomTotalSpent сумма всех расходов (без погашений) по активным операциям
func roomTotalSpent(ops []api.Operation) int {
	var total int
	for i := range ops {
		if !ops[i].IsDebtRepayment {
			total += ops[i].Sum
		}
	}
	return total
}

// userSpentSum доля пользователя в активных расходах (без погашений), целые рубли
func userSpentSum(ops []api.Operation, userId int) int {
	var total int
	for i := range ops {
		o := &ops[i]
		if o.IsDebtRepayment {
			continue
		}
		for j := range o.RecipientsWithSum {
			if o.RecipientsWithSum[j].User.ID == userId {
				total += recipientShare(o, j)
			}
		}
	}
	return total
}

// balanceFromDebts баланс пользователя по вычисленным долгам:
// >0 — пользователю должны, <0 — пользователь должен
func balanceFromDebts(debts []api.Debt, userId int) int {
	var balance int
	for _, d := range debts {
		if d.Lender != nil && d.Lender.ID == userId {
			balance += d.Sum
		}
		if d.Debtor != nil && d.Debtor.ID == userId {
			balance -= d.Sum
		}
	}
	return balance
}
