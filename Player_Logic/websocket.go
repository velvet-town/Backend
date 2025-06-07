package Player_Logic

import (
	"encoding/json"
	"log"
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

// HandleWebSocket handles a new WebSocket connection
func (rm *RoomManager) HandleWebSocket(conn *websocket.Conn, playerID string) {
	defer conn.Close()

	// Get or create player
	rm.mu.Lock()
	player := rm.GetPlayer(playerID)
	if player == nil {
		player = &Player{
			ID:       playerID,
			Position: Position{X: 0, Y: 0},
			IsActive: true,
			LastSeen: time.Now(),
			WS:       conn,
		}
		rm.AddPlayer(playerID)
	} else {
		player.WS = conn
		player.IsActive = true
		player.LastSeen = time.Now()
	}
	rm.mu.Unlock()

	// Notify other players about new player
	rm.broadcastPlayerJoined(player)

	// Handle incoming messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			break
		}

		var wsMessage WebSocketMessage
		if err := json.Unmarshal(message, &wsMessage); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// Handle different message types
		switch wsMessage.Type {
		case "position_update":
			if wsMessage.Position != nil {
				rm.handlePositionUpdate(playerID, *wsMessage.Position)
			}
		}
	}

	// Handle disconnection
	rm.mu.Lock()
	if player := rm.GetPlayer(playerID); player != nil {
		player.IsActive = false
		player.LastSeen = time.Now()
		player.WS = nil
	}
	rm.mu.Unlock()

	// Notify other players about player leaving
	rm.broadcastPlayerLeft(playerID)
}

// broadcastPlayerJoined notifies other players about a new player
func (rm *RoomManager) broadcastPlayerJoined(player *Player) {
	message := WebSocketMessage{
		Type:     "player_joined",
		PlayerID: player.ID,
		Position: &player.Position,
	}

	rm.broadcastToOthers(player.ID, message)
}

// broadcastPlayerLeft notifies other players about a player leaving
func (rm *RoomManager) broadcastPlayerLeft(playerID string) {
	message := WebSocketMessage{
		Type:     "player_left",
		PlayerID: playerID,
	}

	rm.broadcastToOthers(playerID, message)
}

// broadcastToOthers sends a message to all players except the sender
func (rm *RoomManager) broadcastToOthers(senderID string, message WebSocketMessage) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		for id, player := range room.Players {
			if id != senderID && player.WS != nil {
				if err := player.WS.WriteJSON(message); err != nil {
					log.Printf("Error sending message to player %s: %v", id, err)
				}
			}
		}
	}
}

// handlePositionUpdate processes a position update from a player
func (rm *RoomManager) handlePositionUpdate(playerID string, position Position) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if player := rm.GetPlayer(playerID); player != nil {
		player.Position = position
		player.LastSeen = time.Now()

		// Broadcast position update to other players
		message := WebSocketMessage{
			Type:     "position_update",
			PlayerID: playerID,
			Position: &position,
		}
		rm.broadcastToOthers(playerID, message)
	}
}
