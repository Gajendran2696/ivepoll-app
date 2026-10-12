// Package handlers contains the HTTP/WebSocket endpoints. Business rules live here;
// persistence is in store, realtime state is in realtime.
package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"livepoll/config"
	"livepoll/models"
	"livepoll/realtime"
	"livepoll/store"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type App struct {
	Cfg    *config.Config
	Store  *store.Store
	Broker *realtime.Broker
	Hub    *realtime.Hub
}

func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// ---- validation helpers: nothing from the client reaches MongoDB without passing these ----

var (
	emailRe   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	voterIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)
	slugRe    = regexp.MustCompile(`^[a-z0-9]{6,16}$`)
)

func cleanText(s string) string {
	// Collapse whitespace and strip control characters.
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func userID(c *gin.Context) (primitive.ObjectID, bool) {
	raw, _ := c.Get("userID")
	s, _ := raw.(string)
	id, err := primitive.ObjectIDFromHex(s)
	return id, err == nil
}

// loader returns a function that rebuilds a poll's counters from MongoDB, zero-filling every option.
func (a *App) loader(p *models.Poll) realtime.Loader {
	return func(ctx context.Context) (map[string]int64, error) {
		counts, err := a.Store.CountVotes(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, o := range p.Options {
			if _, ok := counts[o.ID]; !ok {
				counts[o.ID] = 0
			}
		}
		return counts, nil
	}
}

// view builds the public representation of a poll, with live counts from Redis.
func (a *App) view(c *gin.Context, p *models.Poll) (*models.PollView, error) {
	counts, err := a.Broker.Counts(c.Request.Context(), p.ID.Hex(), a.loader(p))
	if err != nil {
		// Redis trouble must not take the poll page down: fall back to MongoDB.
		counts, err = a.loader(p)(c.Request.Context())
		if err != nil {
			return nil, err
		}
	}
	v := &models.PollView{
		Slug: p.Slug, Question: p.Question, Options: p.Options, Closed: p.Closed,
		CreatedAt: p.CreatedAt, Counts: counts, Total: realtime.Total(counts),
	}
	if uid, ok := userID(c); ok && uid == p.OwnerID {
		v.IsOwner = true
	}
	return v, nil
}

func abortServer(c *gin.Context, err error) {
	_ = c.Error(err)
	fail(c, http.StatusInternalServerError, "something went wrong, please try again")
}
