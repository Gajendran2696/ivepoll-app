package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"livepoll/models"
	"livepoll/realtime"
	"livepoll/store"

	"github.com/gin-gonic/gin"
)

const (
	minOptions  = 2
	maxOptions  = 10
	maxOptLen   = 100
	maxQuestion = 200
	slugAlpha   = "abcdefghijkmnpqrstuvwxyz23456789" // no look-alike chars (l, o, 0, 1)
)

type createPollReq struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

func (a *App) CreatePoll(c *gin.Context) {
	owner, ok := userID(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "invalid session")
		return
	}
	var req createPollReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid request body")
		return
	}

	question := cleanText(req.Question)
	if runeLen(question) < 3 || runeLen(question) > maxQuestion {
		fail(c, http.StatusBadRequest, fmt.Sprintf("question must be 3-%d characters", maxQuestion))
		return
	}
	if len(req.Options) > maxOptions*3 { // reject absurd payloads before looping over them
		fail(c, http.StatusBadRequest, fmt.Sprintf("a poll can have at most %d options", maxOptions))
		return
	}

	seen := map[string]bool{}
	var opts []models.Option
	for _, raw := range req.Options {
		text := cleanText(raw)
		if text == "" {
			continue // ignore blank rows from the form
		}
		if runeLen(text) > maxOptLen {
			fail(c, http.StatusBadRequest, fmt.Sprintf("each option must be at most %d characters", maxOptLen))
			return
		}
		key := strings.ToLower(text)
		if seen[key] {
			fail(c, http.StatusBadRequest, fmt.Sprintf("duplicate option: %q", text))
			return
		}
		seen[key] = true
		opts = append(opts, models.Option{ID: fmt.Sprintf("o%d", len(opts)), Text: text})
	}
	if len(opts) < minOptions || len(opts) > maxOptions {
		fail(c, http.StatusBadRequest, fmt.Sprintf("provide %d-%d options", minOptions, maxOptions))
		return
	}

	poll := &models.Poll{OwnerID: owner, Question: question, Options: opts}
	var err error
	for attempt := 0; attempt < 5; attempt++ { // slug collisions are astronomically rare, but handled
		poll.Slug = newSlug(8)
		if err = a.Store.CreatePoll(c.Request.Context(), poll); !errors.Is(err, store.ErrDuplicate) {
			break
		}
	}
	if err != nil {
		abortServer(c, err)
		return
	}

	// Warm the Redis counters at zero so the first viewer already hits the live path.
	zero := make(map[string]int64, len(opts))
	for _, o := range opts {
		zero[o.ID] = 0
	}
	_, _ = a.Broker.Counts(c.Request.Context(), poll.ID.Hex(), func(context.Context) (map[string]int64, error) { return zero, nil })

	v, err := a.view(c, poll)
	if err != nil {
		abortServer(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (a *App) ListMyPolls(c *gin.Context) {
	owner, ok := userID(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "invalid session")
		return
	}
	polls, err := a.Store.PollsByOwner(c.Request.Context(), owner)
	if err != nil {
		abortServer(c, err)
		return
	}
	out := make([]models.PollSummary, 0, len(polls))
	for i := range polls {
		p := &polls[i]
		counts, err := a.Broker.Counts(c.Request.Context(), p.ID.Hex(), a.loader(p))
		if err != nil {
			counts, _ = a.loader(p)(c.Request.Context())
		}
		out = append(out, models.PollSummary{
			Slug: p.Slug, Question: p.Question, Closed: p.Closed,
			CreatedAt: p.CreatedAt, Total: realtime.Total(counts),
		})
	}
	c.JSON(http.StatusOK, gin.H{"polls": out})
}

func (a *App) GetPoll(c *gin.Context) {
	p, ok := a.pollFromParam(c)
	if !ok {
		return
	}
	v, err := a.view(c, p)
	if err != nil {
		abortServer(c, err)
		return
	}
	if vk, ok := voterKey(c); ok {
		if opt, err := a.Store.VotedOption(c.Request.Context(), p.ID, vk); err == nil {
			v.VotedOptionID = opt
		}
	}
	c.JSON(http.StatusOK, v)
}

type voteReq struct {
	OptionID string `json:"optionId"`
}

func (a *App) Vote(c *gin.Context) {
	p, ok := a.pollFromParam(c)
	if !ok {
		return
	}
	var req voteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Closed {
		fail(c, http.StatusConflict, "this poll is closed")
		return
	}
	valid := false
	for _, o := range p.Options {
		if o.ID == req.OptionID {
			valid = true
			break
		}
	}
	if !valid {
		fail(c, http.StatusBadRequest, "unknown option")
		return
	}
	vk, ok := voterKey(c)
	if !ok {
		fail(c, http.StatusBadRequest, "missing or invalid X-Voter-Id header")
		return
	}

	// 1) Durable write first. The unique (pollId, voterKey) index makes double-voting impossible,
	//    even with concurrent requests from the same voter.
	if err := a.Store.InsertVote(c.Request.Context(), p.ID, req.OptionID, vk); err != nil {
		if errors.Is(err, store.ErrDuplicateVote) {
			fail(c, http.StatusConflict, "you have already voted in this poll")
			return
		}
		abortServer(c, err)
		return
	}

	// 2) Live path: atomic Redis increment + publish to every connected viewer.
	counts, err := a.Broker.RecordVote(c.Request.Context(), p.ID.Hex(), req.OptionID, a.loader(p))
	if err != nil {
		// The vote is safely stored; only the live broadcast failed. Answer from MongoDB.
		_ = c.Error(err)
		if counts, err = a.loader(p)(c.Request.Context()); err != nil {
			abortServer(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"counts": counts, "total": realtime.Total(counts), "votedOptionId": req.OptionID,
	})
}

type patchPollReq struct {
	Closed *bool `json:"closed"`
}

func (a *App) UpdatePoll(c *gin.Context) {
	owner, ok := userID(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "invalid session")
		return
	}
	var req patchPollReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Closed == nil {
		fail(c, http.StatusBadRequest, "body must be {\"closed\": true|false}")
		return
	}
	slug := c.Param("slug")
	if !slugRe.MatchString(slug) {
		fail(c, http.StatusNotFound, "poll not found")
		return
	}
	if err := a.Store.SetClosed(c.Request.Context(), slug, owner, *req.Closed); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "poll not found")
			return
		}
		abortServer(c, err)
		return
	}
	p, err := a.Store.PollBySlug(c.Request.Context(), slug)
	if err != nil {
		abortServer(c, err)
		return
	}
	_ = a.Broker.PublishStatus(c.Request.Context(), p.ID.Hex(), "status", p.Closed)
	c.JSON(http.StatusOK, gin.H{"closed": p.Closed})
}

func (a *App) DeletePoll(c *gin.Context) {
	owner, ok := userID(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "invalid session")
		return
	}
	slug := c.Param("slug")
	if !slugRe.MatchString(slug) {
		fail(c, http.StatusNotFound, "poll not found")
		return
	}
	p, err := a.Store.DeletePoll(c.Request.Context(), slug, owner)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "poll not found")
			return
		}
		abortServer(c, err)
		return
	}
	_ = a.Broker.PublishStatus(c.Request.Context(), p.ID.Hex(), "deleted", true)
	_ = a.Broker.DeletePoll(c.Request.Context(), p.ID.Hex())
	c.Status(http.StatusNoContent)
}

// ---- helpers ----

func (a *App) pollFromParam(c *gin.Context) (*models.Poll, bool) {
	slug := c.Param("slug")
	if !slugRe.MatchString(slug) {
		fail(c, http.StatusNotFound, "poll not found")
		return nil, false
	}
	p, err := a.Store.PollBySlug(c.Request.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "poll not found")
			return nil, false
		}
		abortServer(c, err)
		return nil, false
	}
	return p, true
}

// voterKey derives the stored voter identity from the browser's random X-Voter-Id.
// It is hashed so raw IDs are never persisted.
func voterKey(c *gin.Context) (string, bool) {
	id := c.GetHeader("X-Voter-Id")
	if !voterIDRe.MatchString(id) {
		return "", false
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]), true
}

func newSlug(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = slugAlpha[int(b[i])%len(slugAlpha)]
	}
	return string(b)
}
