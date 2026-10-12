// Package store is the MongoDB persistence layer (source of truth for users, polls and votes).
package store

import (
	"context"
	"errors"
	"time"

	"livepoll/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicate     = errors.New("duplicate")
	ErrDuplicateVote = errors.New("duplicate vote")
)

type Store struct {
	client *mongo.Client
	users  *mongo.Collection
	polls  *mongo.Collection
	votes  *mongo.Collection
}

func New(ctx context.Context, uri, dbName string) (*Store, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	db := client.Database(dbName)
	s := &Store{
		client: client,
		users:  db.Collection("users"),
		polls:  db.Collection("polls"),
		votes:  db.Collection("votes"),
	}
	return s, s.ensureIndexes(ctx)
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	unique := options.Index().SetUnique(true)
	_, err := s.users.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: unique})
	if err != nil {
		return err
	}
	if _, err = s.polls.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "slug", Value: 1}}, Options: unique}); err != nil {
		return err
	}
	if _, err = s.polls.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "createdAt", Value: -1}}}); err != nil {
		return err
	}
	// One vote per voter per poll, enforced by the database, not just application code.
	_, err = s.votes.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "pollId", Value: 1}, {Key: "voterKey", Value: 1}},
		Options: unique,
	})
	return err
}

func (s *Store) Ping(ctx context.Context) error { return s.client.Ping(ctx, nil) }

func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

// ---- users ----

func (s *Store) CreateUser(ctx context.Context, u *models.User) error {
	u.CreatedAt = time.Now().UTC()
	res, err := s.users.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return err
	}
	u.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := s.users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	return &u, mapErr(err)
}

func (s *Store) UserByID(ctx context.Context, id primitive.ObjectID) (*models.User, error) {
	var u models.User
	err := s.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	return &u, mapErr(err)
}

// ---- polls ----

func (s *Store) CreatePoll(ctx context.Context, p *models.Poll) error {
	p.CreatedAt = time.Now().UTC()
	res, err := s.polls.InsertOne(ctx, p)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return err
	}
	p.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (s *Store) PollBySlug(ctx context.Context, slug string) (*models.Poll, error) {
	var p models.Poll
	err := s.polls.FindOne(ctx, bson.M{"slug": slug}).Decode(&p)
	return &p, mapErr(err)
}

func (s *Store) PollsByOwner(ctx context.Context, owner primitive.ObjectID) ([]models.Poll, error) {
	cur, err := s.polls.Find(ctx, bson.M{"ownerId": owner},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(100))
	if err != nil {
		return nil, err
	}
	var out []models.Poll
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SetClosed(ctx context.Context, slug string, owner primitive.ObjectID, closed bool) error {
	res, err := s.polls.UpdateOne(ctx, bson.M{"slug": slug, "ownerId": owner}, bson.M{"$set": bson.M{"closed": closed}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePoll removes the poll (owner only) and all its votes. It returns the deleted poll.
func (s *Store) DeletePoll(ctx context.Context, slug string, owner primitive.ObjectID) (*models.Poll, error) {
	var p models.Poll
	err := s.polls.FindOneAndDelete(ctx, bson.M{"slug": slug, "ownerId": owner}).Decode(&p)
	if err != nil {
		return nil, mapErr(err)
	}
	_, err = s.votes.DeleteMany(ctx, bson.M{"pollId": p.ID})
	return &p, err
}

// ---- votes ----

func (s *Store) InsertVote(ctx context.Context, pollID primitive.ObjectID, optionID, voterKey string) error {
	_, err := s.votes.InsertOne(ctx, models.Vote{
		PollID: pollID, OptionID: optionID, VoterKey: voterKey, CreatedAt: time.Now().UTC(),
	})
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateVote
	}
	return err
}

// VotedOption returns the option a voter picked, or "" if they have not voted.
func (s *Store) VotedOption(ctx context.Context, pollID primitive.ObjectID, voterKey string) (string, error) {
	var v models.Vote
	err := s.votes.FindOne(ctx, bson.M{"pollId": pollID, "voterKey": voterKey}).Decode(&v)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	return v.OptionID, err
}

// CountVotes aggregates votes per option. Used to (re)build the Redis counters.
func (s *Store) CountVotes(ctx context.Context, pollID primitive.ObjectID) (map[string]int64, error) {
	cur, err := s.votes.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"pollId": pollID}}},
		{{Key: "$group", Value: bson.M{"_id": "$optionId", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `bson:"_id"`
		N  int64  `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ID] = r.N
	}
	return out, nil
}

func mapErr(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	return err
}
