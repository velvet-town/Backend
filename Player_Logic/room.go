package Player_Logic

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	MaxPlayersPerRoom = 20
	RoomCodeLength    = 6
	RoomCodeChars     = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// Room represents a game room
type Room struct {
	ID        string
	Players   map[string]*Player
	CreatedAt time.Time
	mu        sync.RWMutex
}

// RoomManager manages all game rooms
type RoomManager struct {
	mainRoom *Room
	rooms    map[string]*Room // Map of room ID to room
	mu       sync.RWMutex
}

var (
	manager *RoomManager
	once    sync.Once
)

// generateRoomCode creates a unique 6-character room code
func generateRoomCode() string {
	rand.Seed(time.Now().UnixNano())
	code := make([]byte, RoomCodeLength)
	for i := range code {
		code[i] = RoomCodeChars[rand.Intn(len(RoomCodeChars))]
	}
	return string(code)
}

// GetRoomManager returns singleton instance
func GetRoomManager() *RoomManager {
	once.Do(func() {
		mainRoomID := generateRoomCode()
		mainRoom := &Room{
			ID:        mainRoomID,
			Players:   make(map[string]*Player),
			CreatedAt: time.Now(),
		}
		manager = &RoomManager{
			mainRoom: mainRoom,
			rooms:    make(map[string]*Room),
		}
		// Add main room to rooms map
		manager.rooms[mainRoomID] = mainRoom
	})
	return manager
}

// AddPlayer adds a player to the main room
func (rm *RoomManager) AddPlayer(playerID string) (*Room, error) {
	log.Printf("Attempting to add player %s to room", playerID)

	// Check if player already exists
	rm.mainRoom.mu.RLock()
	if _, exists := rm.mainRoom.Players[playerID]; exists {
		rm.mainRoom.mu.RUnlock()
		log.Printf("Player %s already exists in room", playerID)
		return rm.mainRoom, nil
	}
	rm.mainRoom.mu.RUnlock()

	// Check room capacity
	rm.mainRoom.mu.Lock()
	defer rm.mainRoom.mu.Unlock()

	if len(rm.mainRoom.Players) >= MaxPlayersPerRoom {
		log.Printf("Room is full, cannot add player %s", playerID)
		return nil, fmt.Errorf("room is full")
	}

	// Create and add player
	player := &Player{
		ID:       playerID,
		Username: "", // Will be set when first position update is received
		Position: Position{X: 0, Y: 0},
		IsActive: true,
		LastSeen: time.Now(),
	}

	rm.mainRoom.Players[playerID] = player
	log.Printf("Added player %s to room", playerID)

	return rm.mainRoom, nil
}

// AddPlayerToSpecificRoom adds a player to a specific room (creates room if it doesn't exist)
func (rm *RoomManager) AddPlayerToSpecificRoom(playerID, roomID string) (*Room, error) {
	log.Printf("Attempting to add player %s to specific room %s", playerID, roomID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Get or create the room
	room, exists := rm.rooms[roomID]
	if !exists {
		log.Printf("Room %s doesn't exist, creating new room", roomID)
		room = &Room{
			ID:        roomID,
			Players:   make(map[string]*Player),
			CreatedAt: time.Now(),
		}
		rm.rooms[roomID] = room
	}

	// Remove player from any existing room first
	rm.removePlayerFromAllRooms(playerID)

	// Check if player already exists in target room
	room.mu.RLock()
	if _, playerExists := room.Players[playerID]; playerExists {
		room.mu.RUnlock()
		log.Printf("Player %s already exists in room %s", playerID, roomID)
		return room, nil
	}
	room.mu.RUnlock()

	// Check room capacity
	room.mu.Lock()
	defer room.mu.Unlock()

	if len(room.Players) >= MaxPlayersPerRoom {
		log.Printf("Room %s is full, cannot add player %s", roomID, playerID)
		return nil, fmt.Errorf("room %s is full", roomID)
	}

	// Create and add player
	player := &Player{
		ID:       playerID,
		Username: "", // Will be set when first position update is received
		Position: Position{X: 0, Y: 0},
		IsActive: true,
		LastSeen: time.Now(),
	}

	room.Players[playerID] = player
	log.Printf("Added player %s to room %s", playerID, roomID)

	return room, nil
}

// removePlayerFromAllRooms removes a player from all rooms (internal helper)
func (rm *RoomManager) removePlayerFromAllRooms(playerID string) {
	for _, room := range rm.rooms {
		room.mu.Lock()
		if player, exists := room.Players[playerID]; exists {
			player.IsActive = false
			player.LastSeen = time.Now()
			delete(room.Players, playerID)
			log.Printf("Removed player %s from room %s", playerID, room.ID)
		}
		room.mu.Unlock()
	}
}

// GetRoomStats returns statistics about all rooms (for debugging)
func (rm *RoomManager) GetRoomStats() map[string]int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := make(map[string]int)
	for roomID, room := range rm.rooms {
		room.mu.RLock()
		stats[roomID] = len(room.Players)
		room.mu.RUnlock()
	}
	return stats
}

// RemovePlayer removes a player from all rooms
func (rm *RoomManager) RemovePlayer(playerID string) {
	log.Printf("Removing player %s from all rooms", playerID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.removePlayerFromAllRooms(playerID)
}

// GetPlayer returns a player by ID from any room
func (rm *RoomManager) GetPlayer(playerID string) *Player {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		room.mu.RLock()
		if player, exists := room.Players[playerID]; exists {
			room.mu.RUnlock()
			return player
		}
		room.mu.RUnlock()
	}
	return nil
}

// GetPlayerRoom returns the room containing the specified player
func (rm *RoomManager) GetPlayerRoom(playerID string) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		room.mu.RLock()
		if _, exists := room.Players[playerID]; exists {
			room.mu.RUnlock()
			return room
		}
		room.mu.RUnlock()
	}
	return nil
}

// GetRoomPlayers returns all players in the room
func (rm *RoomManager) GetRoomPlayers() []*Player {
	rm.mainRoom.mu.RLock()
	defer rm.mainRoom.mu.RUnlock()

	players := make([]*Player, 0, len(rm.mainRoom.Players))
	for _, player := range rm.mainRoom.Players {
		players = append(players, player)
	}
	return players
}

// handlePositionUpdate updates a player's position and broadcasts it to players in the same room
func (rm *RoomManager) handlePositionUpdate(playerID string, position Position, username string) {
	// Find the room containing the player
	room := rm.GetPlayerRoom(playerID)
	if room == nil {
		log.Printf("Player %s not found in any room for position update", playerID)
		return
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if player, exists := room.Players[playerID]; exists {
		player.Position = position
		player.LastSeen = time.Now()
		if username != "" {
			player.Username = username
		}

		// Broadcast position to other players in the same room
		message := WebSocketMessage{
			Type:     "position_update",
			PlayerID: playerID,
			Position: &position,
			Username: player.Username,
		}

		for id, otherPlayer := range room.Players {
			if id != playerID && otherPlayer.WS != nil {
				if err := otherPlayer.WS.WriteJSON(message); err != nil {
					log.Printf("Error broadcasting position to player %s in room %s: %v", id, room.ID, err)
				}
			}
		}
	}
}
