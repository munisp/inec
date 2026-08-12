package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SECURITY hardening:
// - Token auth per connection (GEOLIBRE_COLLAB_TOKEN); fails closed (503) when
//   unconfigured. Previously CheckOrigin:true + no auth let anyone join any
//   room and inject JSON.
// - Upgrade errors no longer kill the process (was log.Fatal).
// - Per-client buffered write channels with write deadlines; slow clients are
//   disconnected instead of stalling every room behind a global mutex write.
// - SetReadLimit bounds inbound message size.

const (
	maxMessageSize  = 64 * 1024 // 64 KiB read limit
	writeWait       = 5 * time.Second
	sendChannelSize = 32
	pongWait        = 60 * time.Second
	pingPeriod      = 45 * time.Second
)

var collabToken = os.Getenv("GEOLIBRE_COLLAB_TOKEN")

var upgrader = websocket.Upgrader{
	// CheckOrigin still permits browser cross-origin WS handshakes; access
	// control is enforced by the token check below, not by origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn *websocket.Conn
	room string
	send chan []byte // buffered; slow clients are dropped, never block others
}

var (
	clients = make(map[*Client]bool)
	mutex   sync.RWMutex
)

// tokenValid uses a constant-time comparison.
func tokenValid(provided string) bool {
	if collabToken == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(collabToken)) == 1
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// SECURITY: fail closed when no token is configured.
	if collabToken == "" {
		log.Println("GEOLIBRE_COLLAB_TOKEN not configured; refusing connection")
		http.Error(w, `{"error":"collab token not configured; service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	// Token via Sec-WebSocket-Protocol-independent query param or Authorization header.
	token := r.URL.Query().Get("token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		if len(auth) > 7 && auth[:7] == "Bearer " {
			token = auth[7:]
		}
	}
	if !tokenValid(token) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	room := r.URL.Query().Get("room")
	if room == "" {
		room = "default"
	}
	if len(room) > 128 {
		http.Error(w, `{"error":"room name too long"}`, http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Return the error to the logs; never take down the server (was log.Fatal).
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	client := &Client{conn: ws, room: room, send: make(chan []byte, sendChannelSize)}

	mutex.Lock()
	clients[client] = true
	mutex.Unlock()

	go client.writePump()
	client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		mutex.Lock()
		if clients[c] {
			delete(clients, c)
			close(c.send) // exactly-once: only the holder of the map entry closes
		}
		mutex.Unlock()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse message to ensure it's valid JSON
		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		// Inject room into message
		data["room"] = c.room
		enrichedMsg, err := json.Marshal(data)
		if err != nil {
			continue
		}

		broadcastToRoom(c.room, enrichedMsg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// broadcastToRoom fans a message out to room members via their buffered send
// channels. A full channel means a slow/dead client: disconnect it instead of
// stalling every room (previously a single global-mutex WriteMessage with no
// deadline blocked all broadcasts on one dead client).
func broadcastToRoom(room string, msg []byte) {
	mutex.RLock()
	var stale []*Client
	for client := range clients {
		if client.room != room {
			continue
		}
		select {
		case client.send <- msg:
		default:
			stale = append(stale, client)
		}
	}
	mutex.RUnlock()

	if len(stale) > 0 {
		mutex.Lock()
		for _, client := range stale {
			if clients[client] {
				delete(clients, client)
				close(client.send)
				client.conn.Close()
				log.Printf("disconnected slow client in room %q", client.room)
			}
		}
		mutex.Unlock()
	}
}

func main() {
	if collabToken == "" {
		// SECURITY: fail loudly at startup — the service refuses connections
		// (503) in this state; make the misconfiguration unmissable.
		log.Println("WARNING: GEOLIBRE_COLLAB_TOKEN is not set — all WebSocket connections will be rejected (503)")
	}

	http.HandleFunc("/ws", handleConnections)

	// Health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Println("GeoLibre Collaboration Server starting on :8080")
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
