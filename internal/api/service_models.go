package api

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
	"time"
)

type Room struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Name         string             `json:"name" bson:"name"`
	Chat         Chat               `json:"chat" bson:"chat"`
	Members      *[]User            `json:"users" bson:"users"`
	Transactions *[]Transaction     `json:"transactions" bson:"transactions"`
	CreateAt     time.Time          `json:"createAt" bson:"create_at"`
}

type Transaction struct {
	ID          primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Description string             `json:"description" bson:"description"`
	Donor       *User              `json:"donor" bson:"donor"`
	Recipients  *[]User            `json:"recipients" bson:"recipients"`
	Sum         float32            `json:"sum" bson:"sum"`
}

// ChatState stores user state
type ChatState struct {
	UserId     int    `json:"userId" bson:"user_id"`
	Action     string `json:"action" bson:"action"`
	ExternalId string `json:"externalId" bson:"extern_id"`
}

// Button which is sent to the user as ReplyMarkup
type Button struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	CallbackData string             `json:"callbackData" bson:"callback_data"`
	Action       string             `json:"action" bson:"action"`
}
