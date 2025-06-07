package Player_Logic

import (
	"fmt"
	"sync"
	"time"
)

const (
	MaxPlayersPerRoom   = 20
	RoomCleanupInterval = 5 * time.Minute
)

type Room struct {
	ID        string
	Players   map[string]*Player // Using map for O(1) player lookup
	CreatedAt time.Time
	mu        sync.RWMutex
}

type RoomManager struct {
	rooms    map[string]*Room
	mu       sync.RWMutex
	stopChan chan struct{}
}

var (
	manager *RoomManager
	once    sync.Once
)

// GetRoomManager returns singleton instance
func GetRoomManager() *RoomManager {
	once.Do(func() {
		manager = &RoomManager{
			rooms:    make(map[string]*Room),
			stopChan: make(chan struct{}),
		}
		go manager.cleanupInactiveRooms()
	})
	return manager
}

// CreateRoom creates a new room
func (rm *RoomManager) CreateRoom() *Room {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	roomID := fmt.Sprintf("room_%d", len(rm.rooms)+1)
	room := &Room{
		ID:        roomID,
		Players:   make(map[string]*Player),
		CreatedAt: time.Now(),
	}
	rm.rooms[roomID] = room
	return room
}

// FindOrCreateRoom finds a room with space or creates a new one
func (rm *RoomManager) FindOrCreateRoom() *Room {
	rm.mu.RLock()
	for _, room := range rm.rooms {
		room.mu.RLock()
		if len(room.Players) < MaxPlayersPerRoom {
			room.mu.RUnlock()
			rm.mu.RUnlock()
			return room
		}
		room.mu.RUnlock()
	}
	rm.mu.RUnlock()
	return rm.CreateRoom()
}

// AddPlayer adds a player to a room or reconnects them if in grace period
func (rm *RoomManager) AddPlayer(playerID string) (*Room, error) {
	// First check if player is in grace period in any room
	rm.mu.RLock()
	for _, room := range rm.rooms {
		room.mu.Lock()
		if player, exists := room.Players[playerID]; exists {
			if player.IsGracePeriodActive() {
				// Reconnect player
				player.mu.Lock()
				player.IsActive = true
				player.LastSeen = time.Now()
				player.mu.Unlock()
				room.mu.Unlock()
				rm.mu.RUnlock()
				return room, nil
			}
		}
		room.mu.Unlock()
	}
	rm.mu.RUnlock()

	// If not in grace period, add to new room
	room := rm.FindOrCreateRoom()

	room.mu.Lock()
	defer room.mu.Unlock()

	if len(room.Players) >= MaxPlayersPerRoom {
		return nil, fmt.Errorf("room is full")
	}

	player := &Player{
		ID:       playerID,
		RoomID:   room.ID,
		IsActive: true,
		LastSeen: time.Now(),
	}
	room.Players[playerID] = player
	return room, nil
}

// RemovePlayer marks a player as disconnected instead of removing them immediately
func (rm *RoomManager) RemovePlayer(playerID string) {
	rm.mu.RLock()
	for _, room := range rm.rooms {
		room.mu.Lock()
		if player, exists := room.Players[playerID]; exists {
			player.MarkDisconnected()
			room.mu.Unlock()
			rm.mu.RUnlock()
			return
		}
		room.mu.Unlock()
	}
	rm.mu.RUnlock()
}

// GetRoomPlayers returns all players in a room
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

// GetRoomPlayers returns all players in the room containing the specified player
func (rm *RoomManager) GetRoomPlayers(playerID string) []*Player {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// First find the room containing the player
	var targetRoom *Room
	for _, room := range rm.rooms {
		room.mu.RLock()
		if _, exists := room.Players[playerID]; exists {
			targetRoom = room
			room.mu.RUnlock()
			break
		}
		room.mu.RUnlock()
	}

	if targetRoom == nil {
		return nil
	}

	// Get all players from the target room
	targetRoom.mu.RLock()
	defer targetRoom.mu.RUnlock()

	players := make([]*Player, 0, len(targetRoom.Players))
	for _, player := range targetRoom.Players {
		players = append(players, player)
	}
	return players
}

// cleanupInactiveRooms periodically removes empty rooms and players past grace period
func (rm *RoomManager) cleanupInactiveRooms() {
	ticker := time.NewTicker(5 * time.Second) // Check more frequently
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.mu.Lock()
			for roomID, room := range rm.rooms {
				room.mu.Lock()
				// Remove players past grace period
				for playerID, player := range room.Players {
					if !player.IsGracePeriodActive() {
						delete(room.Players, playerID)
					}
				}
				// Remove empty rooms
				if len(room.Players) == 0 {
					delete(rm.rooms, roomID)
				}
				room.mu.Unlock()
			}
			rm.mu.Unlock()
		case <-rm.stopChan:
			return
		}
	}
}

// UpdatePlayerPosition updates a player's position and broadcasts it to other players in the room
func (rm *RoomManager) UpdatePlayerPosition(playerID string, x, y float64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Find the room containing the player
	var playerRoom *Room
	for _, room := range rm.rooms {
		if _, exists := room.Players[playerID]; exists {
			playerRoom = room
			break
		}
	}

	if playerRoom == nil {
		return fmt.Errorf("player not found in any room")
	}

	// Update player position
	player := playerRoom.Players[playerID]
	player.Position.X = x
	player.Position.Y = y

	// Broadcast position update to other players in the room
	positionUpdate := struct {
		Type     string  `json:"type"`
		PlayerID string  `json:"player_id"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
	}{
		Type:     "position_update",
		PlayerID: playerID,
		X:        x,
		Y:        y,
	}

	// Send update to all other players in the room
	for otherPlayerID, otherPlayer := range playerRoom.Players {
		if otherPlayerID != playerID && otherPlayer.WS != nil {
			otherPlayer.WS.WriteJSON(positionUpdate)
		}
	}

	return nil
}
