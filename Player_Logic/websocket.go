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
	Text     string          `json:"text,omitempty"`
	Username string          `json:"username,omitempty"`
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

	// Find player in any room
	player := rm.GetPlayer(playerID)
	if player == nil {
		log.Printf("Player %s not found in any room for WebSocket connection", playerID)
		return
	}

	// Get the room containing this player
	room := rm.GetPlayerRoom(playerID)
	if room == nil {
		log.Printf("Room not found for player %s", playerID)
		return
	}

	// Update player's WebSocket connection
	room.mu.Lock()
	player.WS = conn
	player.IsActive = true
	player.LastSeen = time.Now()
	room.mu.Unlock()

	log.Printf("WebSocket connected for player %s in room %s", playerID, room.ID)

	// Send current room state
	room.mu.RLock()
	players := make([]*Player, 0, len(room.Players))
	for _, p := range room.Players {
		if p.ID != playerID {
			players = append(players, p)
		}
	}
	room.mu.RUnlock()

	// Send player_joined message to other players in the same room
	joinMessage := WebSocketMessage{
		Type:     "player_joined",
		PlayerID: playerID,
		Position: &player.Position,
	}

	room.mu.RLock()
	for id, otherPlayer := range room.Players {
		if id != playerID && otherPlayer.WS != nil {
			otherPlayer.WS.WriteJSON(joinMessage)
		}
	}
	room.mu.RUnlock()

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
		case "chat_message":
			// Get the current room for this player
			currentRoom := rm.GetPlayerRoom(playerID)
			if currentRoom == nil {
				log.Printf("Player %s not found in any room for chat message", playerID)
				continue
			}

			// Always use playerID from the connection, ignore any player_id sent by the client for security and consistency
			log.Printf("[Chat Debug] Received chat message from %s in room %s: %s (username: %s)", playerID, currentRoom.ID, message.Text, message.Username)
			// Broadcast chat message to all players in the same room except the sender
			chatMessage := WebSocketMessage{
				Type:     "chat_message",
				PlayerID: playerID,
				Text:     message.Text,
				Username: message.Username,
			}
			log.Printf("[Chat Debug] Broadcasting chat message to room %s: %+v", currentRoom.ID, chatMessage)
			currentRoom.mu.RLock()
			for id, otherPlayer := range currentRoom.Players {
				if id != playerID && otherPlayer.WS != nil {
					otherPlayer.WS.WriteJSON(chatMessage)
				}
			}
			currentRoom.mu.RUnlock()
		}
	}

	// Cleanup on disconnect
	disconnectRoom := rm.GetPlayerRoom(playerID)
	if disconnectRoom != nil {
		disconnectRoom.mu.Lock()
		if _, exists := disconnectRoom.Players[playerID]; exists {
			delete(disconnectRoom.Players, playerID)
			log.Printf("Removed player %s from room %s. Remaining players: %d", playerID, disconnectRoom.ID, len(disconnectRoom.Players))
		}
		disconnectRoom.mu.Unlock()

		// Notify other players in the same room
		leaveMessage := WebSocketMessage{
			Type:     "player_left",
			PlayerID: playerID,
		}
		disconnectRoom.mu.RLock()
		for id, otherPlayer := range disconnectRoom.Players {
			if id != playerID && otherPlayer.WS != nil {
				otherPlayer.WS.WriteJSON(leaveMessage)
			}
		}
		disconnectRoom.mu.RUnlock()
	}
}
