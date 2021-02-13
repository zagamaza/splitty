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
	CreateAt   time.Time          `json:"createAt" bson:"create_at"`
}

type Operation struct {
	ID              primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Description     string             `json:"description" bson:"description"`
	Donor           *User              `json:"donor" bson:"donor"`
	Recipients      *[]User            `json:"recipients" bson:"recipients"`
	IsDebtRepayment bool               `json:"IsDebtRepayment" bson:"is_debt_repayment"`
	Sum             int                `json:"sum" bson:"sum"`
	CreateAt        time.Time          `json:"createAt" bson:"create_at"`
}

type Debt struct {
	Lender *User `json:"lender" bson:"lender"`
	Debtor *User `json:"debtor" bson:"debtor"`
	Sum    int   `json:"sum" bson:"sum"`
}

// ChatState stores user state
type ChatState struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	UserId       int                `json:"userId" bson:"user_id"`
	Action       Action             `json:"action" bson:"action"`
	CallbackData *CallbackData      `json:"callbackData" bson:"callback_data"`
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
	RoomId      string             `json:"roomId" bson:"room_id,omitempty"`
	UserId      int                `json:"userId" bson:"user_id,omitempty"`
	OperationId primitive.ObjectID `json:"operationId" bson:"operation_id,omitempty"`
	Page        int                `json:"page" bson:"page,omitempty"`
}

func NewButton(action Action, data *CallbackData) *Button {
	return &Button{
		ID:           primitive.NewObjectID(),
		Action:       action,
		CallbackData: data,
		CreateAt:     time.Now(),
	}
}
