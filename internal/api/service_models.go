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
	ID          primitive.ObjectID `json:"id" bson:"_id"`
	Description string             `json:"description" bson:"description"`
	Donor       *User              `json:"donor" bson:"donor"`
	Recipients  *[]User            `json:"recipients" bson:"recipients"`
	Sum         float32            `json:"sum" bson:"sum"`
}
