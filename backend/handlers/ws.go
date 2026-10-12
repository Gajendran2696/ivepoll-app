package handlers

import (
	"net/http"
	"net/url"
	"slices"

	"livepoll/realtime"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WatchPoll upgrades to a WebSocket that streams live results for one poll.
// The poll page is public (anyone with the link may watch), so no login is needed.
func (a *App) WatchPoll(c *gin.Context) {
	p, ok := a.pollFromParam(c)
	if !ok {
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     a.checkOrigin,
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade already wrote the HTTP error
	}

	// Snapshot so the client is correct immediately, even if it connected between two votes.
	var initial [][]byte
	if counts, err := a.Broker.Counts(c.Request.Context(), p.ID.Hex(), a.loader(p)); err == nil {
		initial = append(initial, realtime.Marshal(realtime.ResultsEvent{
			Type: "results", Counts: counts, Total: realtime.Total(counts),
		}))
	}
	initial = append(initial, realtime.Marshal(realtime.StatusEvent{Type: "status", Closed: p.Closed}))

	a.Hub.Attach(conn, p.ID.Hex(), initial)
}

// checkOrigin blocks cross-site WebSocket hijacking: the Origin must be same-host or allow-listed.
func (a *App) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser clients (curl, tests)
	}
	if slices.Contains(a.Cfg.AllowedOrigins, origin) {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}
