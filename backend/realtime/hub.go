package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 45 * time.Second
	pingPeriod = 20 * time.Second
)

// Hub keeps the WebSocket rooms of THIS server instance. Redis pub/sub connects instances,
// so any instance that records a vote reaches viewers connected to every other instance.
type Hub struct {
	broker *Broker
	mu     sync.RWMutex
	rooms  map[string]map[*Client]struct{}
}

func NewHub(b *Broker) *Hub {
	return &Hub{broker: b, rooms: make(map[string]map[*Client]struct{})}
}

// Run subscribes once to every poll channel and fans messages out to local rooms.
func (h *Hub) Run(ctx context.Context) {
	ps := h.broker.Subscribe(ctx)
	defer ps.Close()
	ch := ps.Channel() // go-redis reconnects and resubscribes automatically
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			// channel name: poll:<id>:events
			parts := strings.Split(m.Channel, ":")
			if len(parts) != 3 {
				continue
			}
			h.broadcast(parts[1], []byte(m.Payload))
		}
	}
}

func (h *Hub) broadcast(pollID string, msg []byte) {
	var slow []*Client
	h.mu.RLock()
	for c := range h.rooms[pollID] {
		select {
		case c.send <- msg:
		default:
			slow = append(slow, c) // client can't keep up: drop it, it will reconnect and resync
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		h.remove(c)
	}
}

// Attach registers a freshly upgraded connection and starts its pumps.
// initial holds snapshot messages (current results/status) for the new client only.
func (h *Hub) Attach(conn *websocket.Conn, pollID string, initial [][]byte) {
	c := &Client{
		id:     newID(),
		pollID: pollID,
		conn:   conn,
		send:   make(chan []byte, 32),
		hub:    h,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.broker.Touch(ctx, pollID, c.id); err != nil {
		log.Printf("presence touch: %v", err)
	}

	h.mu.Lock()
	if h.rooms[pollID] == nil {
		h.rooms[pollID] = make(map[*Client]struct{})
	}
	h.rooms[pollID][c] = struct{}{}
	h.mu.Unlock()

	for _, m := range initial {
		c.send <- m
	}
	// Tell everyone (including the newcomer) how many people are watching.
	if _, err := h.broker.PublishViewers(ctx, pollID); err != nil {
		log.Printf("publish viewers: %v", err)
	}

	go c.writePump()
	go c.readPump()
}

// remove detaches a client exactly once and announces the new viewer count.
func (h *Hub) remove(c *Client) {
	c.once.Do(func() {
		h.mu.Lock()
		if room := h.rooms[c.pollID]; room != nil {
			delete(room, c)
			if len(room) == 0 {
				delete(h.rooms, c.pollID)
			}
		}
		h.mu.Unlock()
		close(c.send)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.broker.Leave(ctx, c.pollID, c.id)
		_, _ = h.broker.PublishViewers(ctx, c.pollID)
	})
}

type Client struct {
	id     string
	pollID string
	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	once   sync.Once
}

// readPump only exists to process control frames (pong/close) and detect disconnects.
func (c *Client) readPump() {
	defer c.hub.remove(c)
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.hub.remove(c)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.hub.remove(c)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.hub.broker.Touch(ctx, c.pollID, c.id)
			cancel()
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Marshal is a small helper for building snapshot messages.
func Marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
