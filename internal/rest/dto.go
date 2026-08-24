package rest

import (
	"math"
	"sort"
	"time"

	"github.com/almaznur91/splitty/internal/api"
)

// Статусы и типы деления операций develop-модели.
// Статусы — общие с ботом (api), типы деления REST заводит свои
const (
	statusDraft   = api.StatusDraft
	statusActive  = api.StatusActive
	statusArchive = api.StatusArchive

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

// quotaErrorResponse — та же ошибка плюс состояние квоты рядом с конвертом.
//
// Поле ДОБАВЛЕНО к существующей форме, а не заменяет её: сборки 1.6 разбирают
// ровно {"error":{"code","message"}} и лишнее поле просто игнорируют. Стоит
// вынести код с сообщением наверх — и разбор ошибок сломается у всех, кто ещё
// не обновился.
type quotaErrorResponse struct {
	Error errorBody `json:"error"`
	Quota quotaDto  `json:"quota"`
}

type userDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// Deleted — аккаунт удалён; проставляется только там, где клиенту нужно
	// отличить живого человека от анонимизированного снимка (/friends). Звать
	// такого в группу бессмысленно: добавление вернёт 404. По самому снимку
	// признак не виден — там остаётся лишь затёртое имя, а ловить совпадение с
	// плейсхолдером нельзя, оно бывает и у настоящего имени
	Deleted bool `json:"deleted,omitempty"`
}

type meDto struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Lang        string `json:"lang"`
	// LinkedProviders — привязанные способы входа ("telegram", "google",
	// "apple", "password"): по ним клиент рисует экран «Способы входа» и
	// понимает, какой отвязать нельзя (последний). Наружу отдаётся только ФАКТ
	// привязки — сами идентификаторы личности остаются в базе
	LinkedProviders []string `json:"linkedProviders"`
	NotificationOn  bool     `json:"notificationOn"`
	// LoginEmail — адрес входа по паролю. Остаётся за аккаунтом и после отвязки
	// пароля: по нему клиент понимает, что пароль можно задать заново. Профилю
	// без адреса задавать пароль некуда — завести адрес нечем, почту мы не шлём
	LoginEmail string `json:"loginEmail,omitempty"`
	// PurchaseBindingToken — им клиент помечает покупку в магазине
	// (appAccountToken у Apple, obfuscatedAccountId у Google), чтобы чек
	// достоверно принадлежал этому аккаунту. Нужен ДО покупки, поэтому едет
	// вместе с профилем, а не отдельным запросом с экрана оплаты
	PurchaseBindingToken string `json:"purchaseBindingToken,omitempty"`
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
	// Version версия расхода: клиент возвращает её в PUT, и правка по устаревшей
	// версии отклоняется вместо того, чтобы затереть чужую (см. docs/API.md)
	Version int `json:"version,omitempty"`
}

type debtDto struct {
	Debtor userDto `json:"debtor"`
	Lender userDto `json:"lender"`
	Sum    int     `json:"sum"`
}

// roomAvatarDto ответ на загрузку авы: клиенту нужен новый id, чтобы сразу
// показать картинку, не перечитывая список комнат.
type roomAvatarDto struct {
	AvatarFileId string `json:"avatarFileId"`
}

type roomSummaryDto struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	IsArchived  bool      `json:"isArchived"`
	Currency    string    `json:"currency"`
	Members     []userDto `json:"members"`
	MemberCount int       `json:"memberCount"`
	// AvatarFileId ссылка на фото группы; пусто — клиент рисует градиент
	AvatarFileId string `json:"avatarFileId,omitempty"`
	TotalSpent   int    `json:"totalSpent"`
	MyBalance    int    `json:"myBalance"`
	// DebtsUnavailable true — долги комнаты не считаются на легаси-данных
	// (см. roomDebtsSafe): myBalance отдан нулём, клиент может показать бейдж
	DebtsUnavailable bool `json:"debtsUnavailable,omitempty"`
	// UnreadCount непрочитанные события ЭТОЙ группы: те же события, что
	// поднимают бейдж раздела (notifiesUser), но новее отметки по комнате
	// (см. roomSeenAt). omitempty — у прочитанной группы ключа нет
	UnreadCount int `json:"unreadCount,omitempty"`
}

type roomDetailDto struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	IsArchived bool      `json:"isArchived"`
	Currency   string    `json:"currency"`
	Members    []userDto `json:"members"`
	// AvatarFileId ссылка на фото группы; пусто — клиент рисует градиент
	AvatarFileId string    `json:"avatarFileId,omitempty"`
	TotalSpent   int       `json:"totalSpent"`
	MySpent      int       `json:"mySpent"`
	MyBalance    int       `json:"myBalance"`
	Debts        []debtDto `json:"debts"`
	// DebtsUnavailable true — долги комнаты не считаются на легаси-данных
	// (см. roomDebtsSafe): debts=[] и myBalance=0, остальное поле комнаты
	// (операции, участники, траты) отдаётся как обычно
	DebtsUnavailable bool           `json:"debtsUnavailable,omitempty"`
	Operations       []operationDto `json:"operations"`
	// InviteUrl — ссылка-приглашение вида https://<домен>/join/<roomId>.
	// Отсутствует, пока не задан PUBLIC_BASE_URL: клиент тогда показывает
	// старую ссылку через telegram-бота (см. Server.inviteURL)
	InviteUrl string `json:"inviteUrl,omitempty"`
	// SeenThrough — время формирования ЭТОГО ответа, снятое до чтения комнаты.
	// Клиент возвращает ровно его в POST /rooms/{id}/notifications-seen: с
	// серверным «сейчас» расход, добавленный между ответом и отметкой, погас бы
	// в счётчике карточки, так и не показавшись человеку
	SeenThrough time.Time `json:"seenThrough"`
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
	// source — исходная (нормализованная) операция события. Не экспортируется:
	// клиенту знать чужие рассылки незачем, а счётчику непрочитанного отсюда
	// нужны notification_sent и доли — единственный точный ответ на вопрос
	// «тебе об этом сообщали» (см. notifiesUser). Правило живёт на api.Operation,
	// а не на DTO, чтобы список групп считал свои счётчики по тем же комнатам,
	// не собирая DTO на каждый расход
	source *api.Operation
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
		PurchaseBindingToken: u.PurchaseBindingToken,
		ID:                   u.ID,
		Username:             u.Username,
		DisplayName:          toUserDto(u).DisplayName,
		Lang:                 api.DefineLang(u),
		LinkedProviders:      linkedProviders(u),
		NotificationOn:       u.NotificationOn == nil || *u.NotificationOn,
		LoginEmail:           u.LoginEmail,
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
		Version:         o.Version,
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
// roomAvatarFileId разыменовывает ссылку на фото комнаты. Пустая строка и
// отсутствие поля для клиента значат одно и то же — фото нет.
func roomAvatarFileId(r *api.Room) string {
	if r == nil || r.AvatarFileId == nil {
		return ""
	}
	return *r.AvatarFileId
}

func roomOperations(r *api.Room) []api.Operation {
	if r == nil || r.Operations == nil {
		return nil
	}
	return *r.Operations
}

// normalizedOperation см. api.NormalizedOperation — правило общее с ботом
func normalizedOperation(o api.Operation) api.Operation {
	return api.NormalizedOperation(o)
}

// activeOperations см. api.ActiveOperations — правило общее с ботом
func activeOperations(r *api.Room) []api.Operation {
	return api.ActiveOperations(r)
}

// normalizedRoom см. api.NormalizedRoom — тот же вход для расчёта долгов, но
// доступный и джобу напоминаний, а не только REST
func normalizedRoom(r *api.Room) api.Room {
	return api.NormalizedRoom(r)
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
