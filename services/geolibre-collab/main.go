package main

import (
"encoding/json"
"log"
"net/http"
"sync"

"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
conn *websocket.Conn
room string
}

var (
clients   = make(map[*Client]bool)
broadcast = make(chan []byte)
mutex     sync.Mutex
)

func handleConnections(w http.ResponseWriter, r *http.Request) {
room := r.URL.Query().Get("room")
if room == "" {
room = "default"
}

ws, err := upgrader.Upgrade(w, r, nil)
if err != nil {
log.Fatal(err)
}
defer ws.Close()

client := &Client{conn: ws, room: room}

mutex.Lock()
clients[client] = true
mutex.Unlock()

for {
_, msg, err := ws.ReadMessage()
if err != nil {
mutex.Lock()
delete(clients, client)
mutex.Unlock()
break
}

// Parse message to ensure it's valid JSON
var data map[string]interface{}
if err := json.Unmarshal(msg, &data); err != nil {
continue
}

// Inject room into message
data["room"] = room
enrichedMsg, _ := json.Marshal(data)

broadcast <- enrichedMsg
}
}

func handleMessages() {
for {
msg := <-broadcast

var data map[string]interface{}
json.Unmarshal(msg, &data)
room, _ := data["room"].(string)

mutex.Lock()
for client := range clients {
if client.room == room {
err := client.conn.WriteMessage(websocket.TextMessage, msg)
if err != nil {
client.conn.Close()
delete(clients, client)
}
}
}
mutex.Unlock()
}
}

func main() {
http.HandleFunc("/ws", handleConnections)

// Health check
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
w.Write([]byte("OK"))
})

go handleMessages()

log.Println("GeoLibre Collaboration Server starting on :8080")
err := http.ListenAndServe(":8080", nil)
if err != nil {
log.Fatal("ListenAndServe: ", err)
}
}
