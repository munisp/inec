// Package gotv — WebSocket hub for real-time GOTV events.
// Supports channels: volunteer.location, ride.status, turnout.pu,
// campaign.progress, alert.geofence, canvass.log
package gotv

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// WSMessage is a typed WebSocket event.
type WSMessage struct {
	Channel   string      `json:"channel"`
	PartyID   int         `json:"party_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// WSClient tracks a single WebSocket connection.
type WSClient struct {
	conn       *websocket.Conn
	partyID    int
	channels   map[string]bool
	send       chan []byte
	hub        *WSHub
	mu         sync.Mutex
}

// WSHub manages WebSocket connections with party-scoped broadcasting.
type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan WSMessage
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
	maxPerUser int
	maxGlobal  int
}

// NewWSHub creates a WebSocket hub with connection limits.
func NewWSHub(maxGlobal, maxPerUser int) *WSHub {
	if maxGlobal == 0 {
		maxGlobal = 10000
	}
	if maxPerUser == 0 {
		maxPerUser = 5
	}
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan WSMessage, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		maxPerUser: maxPerUser,
		maxGlobal:  maxGlobal,
	}
}

// Run starts the hub event loop. Call as a goroutine.
func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if len(h.clients) >= h.maxGlobal {
				h.mu.Unlock()
				client.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "max connections"))
				client.conn.Close()
				continue
			}
			count := 0
			for c := range h.clients {
				if c.partyID == client.partyID {
					count++
				}
			}
			if count >= h.maxPerUser {
				h.mu.Unlock()
				client.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "max party connections"))
				client.conn.Close()
				continue
			}
			h.clients[client] = true
			h.mu.Unlock()
			log.Info().Int("party_id", client.partyID).Int("total", len(h.clients)).Msg("WS client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.mu.RLock()
			for client := range h.clients {
				if client.partyID != msg.PartyID && msg.PartyID != 0 {
					continue
				}
				if !client.channels[msg.Channel] && !client.channels["*"] {
					continue
				}
				select {
				case client.send <- data:
				default:
					go func(c *WSClient) {
						h.unregister <- c
						c.conn.Close()
					}(client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all matching clients.
func (h *WSHub) Broadcast(channel string, partyID int, data interface{}) {
	h.broadcast <- WSMessage{
		Channel:   channel,
		PartyID:   partyID,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// HandleWS upgrades an HTTP request to WebSocket.
func (h *WSHub) HandleWS(w http.ResponseWriter, r *http.Request, partyID int) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WS upgrade failed")
		return
	}

	client := &WSClient{
		conn:     conn,
		partyID:  partyID,
		channels: map[string]bool{"*": true},
		send:     make(chan []byte, 64),
		hub:      h,
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var sub struct {
			Action   string   `json:"action"`
			Channels []string `json:"channels"`
		}
		if json.Unmarshal(msg, &sub) == nil && sub.Action == "subscribe" {
			c.mu.Lock()
			c.channels = make(map[string]bool)
			for _, ch := range sub.Channels {
				c.channels[ch] = true
			}
			c.mu.Unlock()
		}
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ClientCount returns the number of active WebSocket connections.
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
