// Package realtime wraps Redis: live vote counters, pub/sub fan-out, presence and rate limiting.
package realtime

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Messages pushed to browsers over WebSocket.
type ResultsEvent struct {
	Type   string           `json:"type"` // "results"
	Counts map[string]int64 `json:"counts"`
	Total  int64            `json:"total"`
}

type ViewersEvent struct {
	Type  string `json:"type"` // "viewers"
	Count int64  `json:"count"`
}

type StatusEvent struct {
	Type   string `json:"type"` // "status" | "deleted"
	Closed bool   `json:"closed"`
}

type Broker struct {
	rdb *redis.Client
}

func NewBroker(ctx context.Context, url string) (*Broker, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return nil, err
	}
	return &Broker{rdb: rdb}, nil
}

func (b *Broker) Ping(ctx context.Context) error { return b.rdb.Ping(ctx).Err() }
func (b *Broker) Close() error                   { return b.rdb.Close() }

func countsKey(pollID string) string  { return "poll:" + pollID + ":counts" }
func viewersKey(pollID string) string { return "poll:" + pollID + ":viewers" }
func channel(pollID string) string    { return "poll:" + pollID + ":events" }

// Loader rebuilds counts from the source of truth (MongoDB) after a cache miss.
type Loader func(ctx context.Context) (map[string]int64, error)

// Counts returns live counts from Redis, rebuilding the hash from MongoDB if it is missing.
func (b *Broker) Counts(ctx context.Context, pollID string, load Loader) (map[string]int64, error) {
	raw, err := b.rdb.HGetAll(ctx, countsKey(pollID)).Result()
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		return parseCounts(raw), nil
	}
	return b.rebuild(ctx, pollID, load)
}

func (b *Broker) rebuild(ctx context.Context, pollID string, load Loader) (map[string]int64, error) {
	counts, err := load(ctx)
	if err != nil {
		return nil, err
	}
	pipe := b.rdb.Pipeline()
	for opt, n := range counts {
		// HSETNX: never clobber a counter another request already rebuilt/incremented.
		pipe.HSetNX(ctx, countsKey(pollID), opt, n)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	raw, err := b.rdb.HGetAll(ctx, countsKey(pollID)).Result()
	if err != nil {
		return nil, err
	}
	return parseCounts(raw), nil
}

// incrScript increments only if the counter hash exists, so a cold cache is detected, not silently zeroed.
var incrScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HINCRBY', KEYS[1], ARGV[1], 1)
  return 1
end
return 0`)

// RecordVote bumps the option counter atomically, then publishes the fresh totals to every viewer.
// Call it AFTER the vote is durably stored in MongoDB.
func (b *Broker) RecordVote(ctx context.Context, pollID, optionID string, load Loader) (map[string]int64, error) {
	hit, err := incrScript.Run(ctx, b.rdb, []string{countsKey(pollID)}, optionID).Int()
	if err != nil {
		return nil, err
	}
	var counts map[string]int64
	if hit == 1 {
		raw, err := b.rdb.HGetAll(ctx, countsKey(pollID)).Result()
		if err != nil {
			return nil, err
		}
		counts = parseCounts(raw)
	} else {
		// Cache was cold: MongoDB already contains this vote, so rebuilding includes it.
		if counts, err = b.rebuild(ctx, pollID, load); err != nil {
			return nil, err
		}
	}
	return counts, b.PublishResults(ctx, pollID, counts)
}

func (b *Broker) PublishResults(ctx context.Context, pollID string, counts map[string]int64) error {
	return b.publish(ctx, pollID, ResultsEvent{Type: "results", Counts: counts, Total: Total(counts)})
}

func (b *Broker) PublishStatus(ctx context.Context, pollID, typ string, closed bool) error {
	return b.publish(ctx, pollID, StatusEvent{Type: typ, Closed: closed})
}

func (b *Broker) publish(ctx context.Context, pollID string, ev any) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, channel(pollID), payload).Err()
}

// DeletePoll drops every Redis key belonging to a poll.
func (b *Broker) DeletePoll(ctx context.Context, pollID string) error {
	return b.rdb.Del(ctx, countsKey(pollID), viewersKey(pollID)).Err()
}

// ---- presence ("watching now") ----
// A sorted set of connection IDs scored by last-seen time. Stale entries (crashed
// instances, dropped networks) simply age out, so the count self-heals.

const presenceTTL = 60 * time.Second

func (b *Broker) Touch(ctx context.Context, pollID, clientID string) error {
	return b.rdb.ZAdd(ctx, viewersKey(pollID), redis.Z{Score: float64(time.Now().Unix()), Member: clientID}).Err()
}

func (b *Broker) Leave(ctx context.Context, pollID, clientID string) error {
	return b.rdb.ZRem(ctx, viewersKey(pollID), clientID).Err()
}

func (b *Broker) Viewers(ctx context.Context, pollID string) (int64, error) {
	cutoff := time.Now().Add(-presenceTTL).Unix()
	pipe := b.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, viewersKey(pollID), "-inf", strconv.FormatInt(cutoff, 10))
	card := pipe.ZCard(ctx, viewersKey(pollID))
	pipe.Expire(ctx, viewersKey(pollID), 5*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return card.Val(), nil
}

func (b *Broker) PublishViewers(ctx context.Context, pollID string) (int64, error) {
	n, err := b.Viewers(ctx, pollID)
	if err != nil {
		return 0, err
	}
	return n, b.publish(ctx, pollID, ViewersEvent{Type: "viewers", Count: n})
}

// ---- rate limiting (fixed window) ----

func (b *Broker) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	k := "rl:" + key
	n, err := b.rdb.Incr(ctx, k).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		b.rdb.Expire(ctx, k, window)
	}
	return n <= limit, nil
}

// ---- helpers ----

func parseCounts(raw map[string]string) map[string]int64 {
	out := make(map[string]int64, len(raw))
	for k, v := range raw {
		n, _ := strconv.ParseInt(v, 10, 64)
		out[k] = n
	}
	return out
}

func Total(counts map[string]int64) int64 {
	var t int64
	for _, n := range counts {
		t += n
	}
	return t
}

// Subscribe exposes a pattern subscription for the hub.
func (b *Broker) Subscribe(ctx context.Context) *redis.PubSub {
	return b.rdb.PSubscribe(ctx, "poll:*:events")
}
