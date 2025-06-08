package Player_Logic

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for development
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	// Enable compression
	EnableCompression: true,
	// Handle subprotocols if needed
	Subprotocols: []string{"json"},
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	Type     string          `json:"type"`
	PlayerID string          `json:"player_id"`
	Position *Position       `json:"position,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// HandleWebSocket handles WebSocket connections
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("token")
	if playerID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Get room manager
	rm := GetRoomManager()

	// Get player
	rm.mainRoom.mu.RLock()
	player, exists := rm.mainRoom.Players[playerID]
	rm.mainRoom.mu.RUnlock()

	if !exists {
		return
	}

	// Update player's WebSocket connection
	rm.mainRoom.mu.Lock()
	player.WS = conn
	player.IsActive = true
	player.LastSeen = time.Now()
	rm.mainRoom.mu.Unlock()

	// Send current room state
	rm.mainRoom.mu.RLock()
	players := make([]*Player, 0, len(rm.mainRoom.Players))
	for _, p := range rm.mainRoom.Players {
		if p.ID != playerID {
			players = append(players, p)
		}
	}
	rm.mainRoom.mu.RUnlock()

	// Send player_joined message to other players
	joinMessage := WebSocketMessage{
		Type:     "player_joined",
		PlayerID: playerID,
		Position: &player.Position,
	}

	rm.mainRoom.mu.RLock()
	for id, otherPlayer := range rm.mainRoom.Players {
		if id != playerID && otherPlayer.WS != nil {
			otherPlayer.WS.WriteJSON(joinMessage)
		}
	}
	rm.mainRoom.mu.RUnlock()

	// Send current players to new player
	for _, p := range players {
		message := WebSocketMessage{
			Type:     "player_joined",
			PlayerID: p.ID,
			Position: &p.Position,
		}
		if err := conn.WriteJSON(message); err != nil {
			return
		}
	}

	// Handle WebSocket messages
	for {
		var message WebSocketMessage
		err := conn.ReadJSON(&message)
		if err != nil {
			break
		}

		switch message.Type {
		case "position_update":
			if message.Position != nil {
				rm.handlePositionUpdate(playerID, *message.Position)
			}
		case "leave_room":
			rm.RemovePlayer(playerID)
			return
		}
	}

	// Cleanup on disconnect
	rm.mainRoom.mu.Lock()
	if player, exists := rm.mainRoom.Players[playerID]; exists {
		player.WS = nil
		player.IsActive = false
		player.LastSeen = time.Now()
	}
	rm.mainRoom.mu.Unlock()

	// Notify other players
	leaveMessage := WebSocketMessage{
		Type:     "player_left",
		PlayerID: playerID,
	}

	rm.mainRoom.mu.RLock()
	for id, otherPlayer := range rm.mainRoom.Players {
		if id != playerID && otherPlayer.WS != nil {
			otherPlayer.WS.WriteJSON(leaveMessage)
		}
	}
	rm.mainRoom.mu.RUnlock()
}
