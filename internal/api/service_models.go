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
	ID          primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Description string             `json:"description" bson:"description"`
	Donor       *User              `json:"donor" bson:"donor"`
	Recipients  *[]User            `json:"recipients" bson:"recipients"`
	Sum         float32            `json:"sum" bson:"sum"`
}

// ChatState stores user state
type ChatState struct {
	ID         primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	UserId     int                `json:"userId" bson:"user_id"`
	Action     Action             `json:"action" bson:"action"`
	ExternalId string             `json:"externalId" bson:"extern_id"`
}

// Button which is sent to the user as ReplyMarkup
type Button struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	CallbackData *CallbackData      `json:"callbackData" bson:"callback_data"`
	Action       Action             `json:"action" bson:"action"`
}

type Action string

type CallbackData struct {
	RoomId string `json:"roomId" bson:"room_id,omitempty"`
}

func NewButton(action Action, data *CallbackData) *Button {
	return &Button{
		Action:       action,
		CallbackData: data,
	}
}
