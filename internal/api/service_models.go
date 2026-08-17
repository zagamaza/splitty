package api

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
	"time"
)

type Room struct {
	ID         primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Name       string             `json:"name" bson:"name"`
	Chat       Chat               `json:"chat" bson:"chat"`
	Members    *[]User            `json:"users" bson:"users"`
	Operations *[]Operation       `json:"operations" bson:"operations"`
	RoomStates RoomStatesUsers    `json:"roomStates" bson:"room_states"`
	CreateAt   time.Time          `json:"createAt" bson:"create_at"`
	Currency   string             `json:"currency" bson:"currency"`
	// AvatarFileId — ссылка на документ в коллекции files. Сами байты внутри
	// комнаты не лежат: тут уже все её операции и потолок mongo 16 МБ.
	// nil — фото не загружали, клиент рисует градиент по хэшу id.
	AvatarFileId *string `json:"avatarFileId,omitempty" bson:"avatar_file_id,omitempty"`
}

type CurrencyInfo struct {
	Code   string // ISO-код, например, "RUB"
	Symbol string // Символ валюты, например, "₽"
	Flag   string // Флаг страны, например, "🇷🇺"
}

type RoomStatesUsers struct {
	Archived             []int `json:"archived" bson:"archived,omitempty"`
	PaidOffDebt          []int `json:"paidOffDebts" bson:"paid_off_debts,omitempty"`
	FinishedAddOperation []int `json:"finishedAddOperation" bson:"finished_add_operation,omitempty"`
}

type Operation struct {
	ID                primitive.ObjectID  `json:"id" bson:"_id,omitempty"`
	OldOperationId    *primitive.ObjectID `json:"old_operation_id" bson:"old_operation_id,omitempty"`
	Description       string              `json:"description" bson:"description"`
	Donor             *User               `json:"donor" bson:"donor"`
	Recipients        *[]User             `json:"recipients" bson:"recipients"`
	RecipientsWithSum []RecipientWithSum  `json:"recipientsWithSum" bson:"recipients_with_sum"`
	IsDebtRepayment   bool                `json:"IsDebtRepayment" bson:"is_debt_repayment"`
	Sum               int                 `json:"sum" bson:"sum"`
	NotificationSent  []int               `json:"notificationSent" bson:"notification_sent"`
	CreateAt          time.Time           `json:"createAt" bson:"create_at"`
	Files             []File              `json:"files" bson:"files,omitempty"`
	Status            OperationStatus     `json:"status" bson:"status"`
	SplitType         SplitType           `json:"splitType" bson:"split_type"`
	// ClientOpId клиентский идемпотентный ключ операции (uuid из outbox
	// офлайн-клиента): заполняется только REST, бот его не пишет
	ClientOpId string `bson:"client_op_id,omitempty" json:"clientOpId,omitempty"`
	// Items детализация расхода по позициям чека (AI-распознавание): источник
	// правды, из которого сервер выводит RecipientsWithSum. nil для обычных
	// операций — старые клиенты (бот, Android) про Items не знают и работают
	// на плоских RecipientsWithSum
	Items []OperationItem `json:"items,omitempty" bson:"items,omitempty"`
	// Version растёт на каждую запись операции. По ней правка отличает «с
	// момента чтения никто не писал» от «писали»: без этого две одновременные
	// правки одного расхода затирали друг друга по last-write-wins, и человек
	// узнавал об этом, только увидев чужую сумму вместо своей.
	// omitempty ради старых документов — там поля нет вовсе, и это значит 0
	Version int `json:"version,omitempty" bson:"version,omitempty"`
}

type RecipientWithSum struct {
	User User    `json:"user" bson:"user"`
	Sum  float64 `json:"sum" bson:"sum"`
}

// ItemKind различает обычную позицию и надбавку (сервисный сбор, чаевые, доставка)
type ItemKind string

// SplitRule правило деления надбавки: пропорционально съеденному или поровну
type SplitRule string

const (
	ItemKindItem      ItemKind = "item"
	ItemKindSurcharge ItemKind = "surcharge"

	SplitProportional SplitRule = "proportional"
	SplitEqually      SplitRule = "equally"
)

// ItemShare доля одного участника в позиции. Amount задан → фиксированная сумма
// (Weight игнорируется); иначе доля определяется относительным Weight
// (1 у всех = поровну).
type ItemShare struct {
	UserId int  `json:"userId" bson:"user_id"`
	Weight int  `json:"weight" bson:"weight"`
	Amount *int `json:"amount,omitempty" bson:"amount,omitempty"`
}

// OperationItem одна строка чека. Price — всегда суммарная стоимость строки
// (не цена единицы); Qty и Percent — только для отображения, в расчёте не
// участвуют. У Kind==surcharge поле Shares не используется (сбор делится по
// долям людей от обычных позиций согласно Split).
type OperationItem struct {
	Name    string      `json:"name" bson:"name"`
	Price   int         `json:"price" bson:"price"`
	Qty     int         `json:"qty" bson:"qty"`
	Shares  []ItemShare `json:"shares" bson:"shares"`
	Kind    ItemKind    `json:"kind" bson:"kind"`
	Split   SplitRule   `json:"split,omitempty" bson:"split,omitempty"`
	Percent *int        `json:"percent,omitempty" bson:"percent,omitempty"`
}

type File struct {
	Type   FileType `json:"type" bson:"type"`
	FileId string   `json:"fileId" bson:"file_id"`
}
type FileType string

// StoredFileKind — зачем файл лежит в базе. Вид пока один, но поле есть с
// самого начала: чек расхода ляжет рядом без миграции.
type StoredFileKind string

const StoredFileRoomAvatar StoredFileKind = "room_avatar"

// StoredFile — картинка, загруженная из приложения. Байты лежат ОТДЕЛЬНОЙ
// коллекцией, а не внутри комнаты: в документе комнаты уже все её операции,
// потолок mongo 16 МБ, и ава вычитывалась бы при каждом открытии списка групп.
//
// RoomId — и владелец, и проверка доступа: файл видит тот, кто состоит в этой
// комнате.
type StoredFile struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	RoomId    primitive.ObjectID `bson:"room_id"`
	OwnerId   int                `bson:"owner_id"`
	Kind      StoredFileKind     `bson:"kind"`
	Mime      string             `bson:"mime"`
	Size      int                `bson:"size"`
	Data      []byte             `bson:"data"`
	CreatedAt time.Time          `bson:"created_at"`
}

type OperationStatus string

type SplitType string

type Debt struct {
	Lender *User `json:"lender" bson:"lender"`
	Debtor *User `json:"debtor" bson:"debtor"`
	Sum    int   `json:"sum" bson:"sum"`
}

// ChatState stores user state
type ChatState struct {
	ID primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	// UserId — НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY (u.User.ID), не telegram id и не chat
	// id. Исторически сюда писали getChatID(u), и состояние находилось лишь
	// потому, что _id == telegram id == chat id приватного чата. У аккаунта,
	// пришедшего через Google и привязавшего telegram, эти числа разные, и
	// смешивать их — значит терять многошаговые сценарии между шагами
	UserId       int           `json:"userId" bson:"user_id"`
	Action       Action        `json:"action" bson:"action"`
	CallbackData *CallbackData `json:"callbackData" bson:"callback_data"`
}

// Button which is sent to the user as ReplyMarkup
type Button struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	CallbackData *CallbackData      `json:"callbackData" bson:"callback_data"`
	Text         string             `json:"text" bson:"text"`
	Action       Action             `json:"action" bson:"action"`
	CreateAt     time.Time          `json:"createAt" bson:"create_at"`
}

type Action string

type CallbackData struct {
	RoomId       string             `json:"roomId" bson:"room_id,omitempty"`
	UserId       int                `json:"userId" bson:"user_id,omitempty"`
	ExternalId   string             `json:"externalId" bson:"external_id,omitempty"`
	ExternalData string             `json:"externalData" bson:"external_data,omitempty"`
	OperationId  primitive.ObjectID `json:"operationId" bson:"operation_id,omitempty"`
	Page         int                `json:"page" bson:"page,omitempty"`
	Expand       bool               `json:"collapse" bson:"collapse,omitempty"`
}

type OperationDiff struct {
	NameChanged            bool
	PhotoAdded             bool
	RecipientsAdded        []RecipientWithSum
	RecipientsRemoved      []RecipientWithSum
	RecipientsShareChanged []RecipientShareChange
}

// Изменение суммы конкретного получателя
type RecipientShareChange struct {
	User   User
	OldSum float64
	NewSum float64
}

// BugReport репорт о баге, отправленный командой /report в боте
type BugReport struct {
	ID          primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	UserId      int                `json:"userId" bson:"user_id"`
	Username    string             `json:"username" bson:"username"`
	DisplayName string             `json:"displayName" bson:"display_name"`
	Text        string             `json:"text" bson:"text"`
	CreateAt    time.Time          `json:"createAt" bson:"create_at"`
}

// LoginCode одноразовый код входа в приложение, выдаётся командой /login в боте
type LoginCode struct {
	ID        primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Code      string             `json:"code" bson:"code"`
	UserId    int                `json:"userId" bson:"user_id"`
	ExpiresAt time.Time          `json:"expiresAt" bson:"expires_at"`
	Used      bool               `json:"used" bson:"used"`
}

func NewButton(action Action, data *CallbackData) *Button {
	return &Button{
		ID:           primitive.NewObjectID(),
		Action:       action,
		CallbackData: data,
		CreateAt:     time.Now(),
	}
}
