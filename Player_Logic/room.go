package Player_Logic

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	MaxPlayersPerRoom = 20
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
	mu       sync.RWMutex
}

var (
	manager *RoomManager
	once    sync.Once
)

// GetRoomManager returns singleton instance
func GetRoomManager() *RoomManager {
	once.Do(func() {
		manager = &RoomManager{
			mainRoom: &Room{
				ID:        "main_room",
				Players:   make(map[string]*Player),
				CreatedAt: time.Now(),
			},
		}
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
		Position: Position{X: 0, Y: 0},
		IsActive: true,
		LastSeen: time.Now(),
	}

	rm.mainRoom.Players[playerID] = player
	log.Printf("Added player %s to room", playerID)

	return rm.mainRoom, nil
}

// RemovePlayer removes a player from the room
func (rm *RoomManager) RemovePlayer(playerID string) {
	log.Printf("Removing player %s from room", playerID)

	rm.mainRoom.mu.Lock()
	defer rm.mainRoom.mu.Unlock()

	if player, exists := rm.mainRoom.Players[playerID]; exists {
		player.IsActive = false
		player.LastSeen = time.Now()
		delete(rm.mainRoom.Players, playerID)
		log.Printf("Removed player %s from room", playerID)
	}
}

// GetPlayer returns a player by ID
func (rm *RoomManager) GetPlayer(playerID string) *Player {
	rm.mainRoom.mu.RLock()
	defer rm.mainRoom.mu.RUnlock()

	if player, exists := rm.mainRoom.Players[playerID]; exists {
		return player
	}
	return nil
}

// GetPlayerRoom returns the room containing the specified player
func (rm *RoomManager) GetPlayerRoom(playerID string) *Room {
	rm.mainRoom.mu.RLock()
	defer rm.mainRoom.mu.RUnlock()

	if _, exists := rm.mainRoom.Players[playerID]; exists {
		return rm.mainRoom
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

// handlePositionUpdate updates a player's position and broadcasts it
func (rm *RoomManager) handlePositionUpdate(playerID string, position Position) {
	rm.mainRoom.mu.Lock()
	defer rm.mainRoom.mu.Unlock()

	if player, exists := rm.mainRoom.Players[playerID]; exists {
		player.Position = position
		player.LastSeen = time.Now()

		// Broadcast position to other players
		message := WebSocketMessage{
			Type:     "position_update",
			PlayerID: playerID,
			Position: &position,
		}

		for id, otherPlayer := range rm.mainRoom.Players {
			if id != playerID && otherPlayer.WS != nil {
				if err := otherPlayer.WS.WriteJSON(message); err != nil {
					log.Printf("Error broadcasting position to player %s: %v", id, err)
				}
			}
		}
	}
}
