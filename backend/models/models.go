// Package models holds the persisted documents and the JSON views sent to clients.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Name         string             `bson:"name"`
	Email        string             `bson:"email"`
	PasswordHash string             `bson:"passwordHash"`
	CreatedAt    time.Time          `bson:"createdAt"`
}

type Option struct {
	ID   string `bson:"id" json:"id"`
	Text string `bson:"text" json:"text"`
}

type Poll struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Slug      string             `bson:"slug"`
	OwnerID   primitive.ObjectID `bson:"ownerId"`
	Question  string             `bson:"question"`
	Options   []Option           `bson:"options"`
	Closed    bool               `bson:"closed"`
	CreatedAt time.Time          `bson:"createdAt"`
}

// Vote is the durable record. A unique index on (pollId, voterKey) guarantees one vote per voter.
type Vote struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	PollID    primitive.ObjectID `bson:"pollId"`
	OptionID  string             `bson:"optionId"`
	VoterKey  string             `bson:"voterKey"`
	CreatedAt time.Time          `bson:"createdAt"`
}

// ---- JSON views ----

type UserView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type PollView struct {
	Slug          string           `json:"slug"`
	Question      string           `json:"question"`
	Options       []Option         `json:"options"`
	Closed        bool             `json:"closed"`
	CreatedAt     time.Time        `json:"createdAt"`
	Counts        map[string]int64 `json:"counts"`
	Total         int64            `json:"total"`
	VotedOptionID string           `json:"votedOptionId,omitempty"`
	IsOwner       bool             `json:"isOwner"`
}

type PollSummary struct {
	Slug      string    `json:"slug"`
	Question  string    `json:"question"`
	Closed    bool      `json:"closed"`
	CreatedAt time.Time `json:"createdAt"`
	Total     int64     `json:"total"`
}
