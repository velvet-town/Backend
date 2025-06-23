package Player_Logic

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	MaxPlayersPerRoom     = 20
	RoomCodeLength        = 6
	RoomCodeChars         = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	CleanupInterval       = 5 * time.Minute  // Cleanup every 5 minutes
	InactiveRoomTimeout   = 30 * time.Minute // Remove empty rooms after 30 minutes
	DisconnectedPlayerTTL = 80 * time.Second // Grace period for reconnection
)

// Room represents a game room with optimized concurrency
type Room struct {
	ID           string
	Players      map[string]*Player
	CreatedAt    time.Time
	LastActivity time.Time
	mu           sync.RWMutex
	// Performance optimizations
	playerCount int32 // Atomic counter to avoid map len() calls
}

// RoomManager manages all game rooms with optimized lookups
type RoomManager struct {
	// Core data structures
	mainRoom *Room
	rooms    map[string]*Room // Map of room ID to room
	mu       sync.RWMutex

	// Optimization: Player-to-room mapping for O(1) lookups
	playerToRoom map[string]string // playerID -> roomID
	playerMu     sync.RWMutex      // Separate lock for player mapping

	// Cleanup management
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
	cleanupWG     sync.WaitGroup

	// Statistics and monitoring
	stats struct {
		totalRoomsCreated  int64
		totalPlayersServed int64
		currentActiveRooms int32
		cleanupOperations  int64
		mu                 sync.RWMutex
	}
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

// GetRoomManager returns optimized singleton instance
func GetRoomManager() *RoomManager {
	once.Do(func() {
		mainRoomID := generateRoomCode()
		mainRoom := &Room{
			ID:           mainRoomID,
			Players:      make(map[string]*Player),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
			playerCount:  0,
		}

		ctx, cancel := context.WithCancel(context.Background())
		manager = &RoomManager{
			mainRoom:      mainRoom,
			rooms:         make(map[string]*Room),
			playerToRoom:  make(map[string]string),
			cleanupCtx:    ctx,
			cleanupCancel: cancel,
		}

		// Add main room to rooms map
		manager.rooms[mainRoomID] = mainRoom

		// Start cleanup routines
		manager.startCleanupRoutines()

		log.Printf("Room manager initialized with main room: %s", mainRoomID)
	})
	return manager
}

// startCleanupRoutines starts background cleanup tasks
func (rm *RoomManager) startCleanupRoutines() {
	// Room cleanup routine
	rm.cleanupWG.Add(1)
	go func() {
		defer rm.cleanupWG.Done()
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rm.performCleanup()
			case <-rm.cleanupCtx.Done():
				return
			}
		}
	}()

	// Player activity monitor
	rm.cleanupWG.Add(1)
	go func() {
		defer rm.cleanupWG.Done()
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rm.cleanupInactivePlayers()
			case <-rm.cleanupCtx.Done():
				return
			}
		}
	}()

	log.Println("Room cleanup routines started")
}

// performCleanup removes empty rooms and inactive players
func (rm *RoomManager) performCleanup() {
	rm.stats.mu.Lock()
	rm.stats.cleanupOperations++
	rm.stats.mu.Unlock()

	now := time.Now()
	var roomsToDelete []string

	rm.mu.RLock()
	for roomID, room := range rm.rooms {
		// Skip main room
		if roomID == rm.mainRoom.ID {
			continue
		}

		room.mu.RLock()
		isEmpty := len(room.Players) == 0
		isInactive := now.Sub(room.LastActivity) > InactiveRoomTimeout
		room.mu.RUnlock()

		if isEmpty && isInactive {
			roomsToDelete = append(roomsToDelete, roomID)
		}
	}
	rm.mu.RUnlock()

	// Delete empty rooms
	if len(roomsToDelete) > 0 {
		rm.mu.Lock()
		for _, roomID := range roomsToDelete {
			delete(rm.rooms, roomID)
			log.Printf("Cleaned up empty room: %s", roomID)
		}
		rm.stats.mu.Lock()
		rm.stats.currentActiveRooms = int32(len(rm.rooms))
		rm.stats.mu.Unlock()
		rm.mu.Unlock()

		log.Printf("Cleanup completed: removed %d empty rooms", len(roomsToDelete))
	}
}

// cleanupInactivePlayers removes disconnected players after grace period
func (rm *RoomManager) cleanupInactivePlayers() {
	now := time.Now()
	var playersToRemove []string

	rm.playerMu.RLock()
	for playerID, roomID := range rm.playerToRoom {
		room := rm.getRoomByID(roomID)
		if room == nil {
			playersToRemove = append(playersToRemove, playerID)
			continue
		}

		room.mu.RLock()
		if player, exists := room.Players[playerID]; exists {
			if !player.IsActive && now.Sub(player.LastSeen) > DisconnectedPlayerTTL {
				playersToRemove = append(playersToRemove, playerID)
			}
		}
		room.mu.RUnlock()
	}
	rm.playerMu.RUnlock()

	// Remove inactive players
	for _, playerID := range playersToRemove {
		rm.RemovePlayerOptimized(playerID)
		log.Printf("Cleaned up inactive player: %s", playerID)
	}

	if len(playersToRemove) > 0 {
		log.Printf("Cleanup completed: removed %d inactive players", len(playersToRemove))
	}
}

// getRoomByID safely gets a room by ID
func (rm *RoomManager) getRoomByID(roomID string) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.rooms[roomID]
}

// AddPlayer adds a player to the main room (optimized)
func (rm *RoomManager) AddPlayer(playerID string) (*Room, error) {
	// Fast path: check if player already exists using O(1) lookup
	if existingRoomID := rm.getPlayerRoomID(playerID); existingRoomID != "" {
		if existingRoomID == rm.mainRoom.ID {
			log.Printf("Player %s already exists in main room", playerID)
			return rm.mainRoom, nil
		}
		// Remove from current room first
		rm.RemovePlayerOptimized(playerID)
	}

	return rm.addPlayerToRoom(playerID, rm.mainRoom.ID)
}

// AddPlayerToSpecificRoom adds a player to a specific room (optimized)
func (rm *RoomManager) AddPlayerToSpecificRoom(playerID, roomID string) (*Room, error) {
	log.Printf("Attempting to add player %s to specific room %s", playerID, roomID)

	// Fast path: check if player already in target room
	if existingRoomID := rm.getPlayerRoomID(playerID); existingRoomID == roomID {
		log.Printf("Player %s already exists in room %s", playerID, roomID)
		return rm.getRoomByID(roomID), nil
	}

	// Remove from current room if exists
	if existingRoomID := rm.getPlayerRoomID(playerID); existingRoomID != "" {
		rm.RemovePlayerOptimized(playerID)
	}

	// Create room if it doesn't exist
	rm.mu.Lock()
	room, exists := rm.rooms[roomID]
	if !exists {
		log.Printf("Room %s doesn't exist, creating new room", roomID)
		room = &Room{
			ID:           roomID,
			Players:      make(map[string]*Player),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
			playerCount:  0,
		}
		rm.rooms[roomID] = room
		rm.stats.mu.Lock()
		rm.stats.totalRoomsCreated++
		rm.stats.currentActiveRooms = int32(len(rm.rooms))
		rm.stats.mu.Unlock()
	}
	rm.mu.Unlock()

	return rm.addPlayerToRoom(playerID, roomID)
}

// addPlayerToRoom adds a player to a specific room (internal optimized helper)
func (rm *RoomManager) addPlayerToRoom(playerID, roomID string) (*Room, error) {
	room := rm.getRoomByID(roomID)
	if room == nil {
		return nil, fmt.Errorf("room %s not found", roomID)
	}

	// Check room capacity with minimal locking
	room.mu.RLock()
	if len(room.Players) >= MaxPlayersPerRoom {
		room.mu.RUnlock()
		log.Printf("Room %s is full, cannot add player %s", roomID, playerID)
		return nil, fmt.Errorf("room %s is full", roomID)
	}
	room.mu.RUnlock()

	// Create player
	player := &Player{
		ID:       playerID,
		Username: "",
		Position: Position{X: 0, Y: 0},
		IsActive: true,
		LastSeen: time.Now(),
	}

	// Add player with minimal lock scope
	room.mu.Lock()
	// Double-check capacity after acquiring lock
	if len(room.Players) >= MaxPlayersPerRoom {
		room.mu.Unlock()
		return nil, fmt.Errorf("room %s is full", roomID)
	}

	room.Players[playerID] = player
	room.LastActivity = time.Now()
	room.playerCount = int32(len(room.Players))
	room.mu.Unlock()

	// Update player-to-room mapping
	rm.playerMu.Lock()
	rm.playerToRoom[playerID] = roomID
	rm.playerMu.Unlock()

	rm.stats.mu.Lock()
	rm.stats.totalPlayersServed++
	rm.stats.mu.Unlock()

	log.Printf("Added player %s to room %s", playerID, roomID)
	return room, nil
}

// getPlayerRoomID gets the room ID for a player using O(1) lookup
func (rm *RoomManager) getPlayerRoomID(playerID string) string {
	rm.playerMu.RLock()
	defer rm.playerMu.RUnlock()
	return rm.playerToRoom[playerID]
}

// GetPlayer returns a player by ID using O(1) lookup
func (rm *RoomManager) GetPlayer(playerID string) *Player {
	roomID := rm.getPlayerRoomID(playerID)
	if roomID == "" {
		return nil
	}

	room := rm.getRoomByID(roomID)
	if room == nil {
		// Clean up stale mapping
		rm.playerMu.Lock()
		delete(rm.playerToRoom, playerID)
		rm.playerMu.Unlock()
		return nil
	}

	room.mu.RLock()
	defer room.mu.RUnlock()
	return room.Players[playerID]
}

// GetPlayerRoom returns the room containing the specified player using O(1) lookup
func (rm *RoomManager) GetPlayerRoom(playerID string) *Room {
	roomID := rm.getPlayerRoomID(playerID)
	if roomID == "" {
		return nil
	}
	return rm.getRoomByID(roomID)
}

// RemovePlayerOptimized removes a player using O(1) lookup
func (rm *RoomManager) RemovePlayerOptimized(playerID string) {
	roomID := rm.getPlayerRoomID(playerID)
	if roomID == "" {
		return // Player not found
	}

	room := rm.getRoomByID(roomID)
	if room == nil {
		// Clean up stale mapping
		rm.playerMu.Lock()
		delete(rm.playerToRoom, playerID)
		rm.playerMu.Unlock()
		return
	}

	room.mu.Lock()
	if player, exists := room.Players[playerID]; exists {
		player.IsActive = false
		player.LastSeen = time.Now()
		delete(room.Players, playerID)
		room.LastActivity = time.Now()
		room.playerCount = int32(len(room.Players))
		log.Printf("Removed player %s from room %s. Remaining players: %d",
			playerID, room.ID, len(room.Players))
	}
	room.mu.Unlock()

	// Remove from player-to-room mapping
	rm.playerMu.Lock()
	delete(rm.playerToRoom, playerID)
	rm.playerMu.Unlock()
}

// RemovePlayer removes a player from all rooms (legacy compatibility)
func (rm *RoomManager) RemovePlayer(playerID string) {
	rm.RemovePlayerOptimized(playerID)
}

// GetRoomStats returns statistics about all rooms (optimized)
func (rm *RoomManager) GetRoomStats() map[string]int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := make(map[string]int, len(rm.rooms))
	for roomID, room := range rm.rooms {
		room.mu.RLock()
		stats[roomID] = len(room.Players)
		room.mu.RUnlock()
	}
	return stats
}

// GetRoomPlayers returns all players in the main room
func (rm *RoomManager) GetRoomPlayers() []*Player {
	rm.mainRoom.mu.RLock()
	defer rm.mainRoom.mu.RUnlock()

	players := make([]*Player, 0, len(rm.mainRoom.Players))
	for _, player := range rm.mainRoom.Players {
		players = append(players, player)
	}
	return players
}

// handlePositionUpdateOptimized updates a player's position with O(1) lookup
func (rm *RoomManager) handlePositionUpdateOptimized(playerID string, position Position, username string) {
	// O(1) room lookup instead of linear search
	room := rm.GetPlayerRoom(playerID)
	if room == nil {
		log.Printf("Player %s not found in any room for position update", playerID)
		return
	}

	// Minimal lock scope for position update
	room.mu.Lock()
	if player, exists := room.Players[playerID]; exists {
		player.Position = position
		player.LastSeen = time.Now()
		if username != "" {
			player.Username = username
		}
		room.LastActivity = time.Now()
	} else {
		room.mu.Unlock()
		return
	}
	room.mu.Unlock()

	// Broadcast position asynchronously
	message := WebSocketMessage{
		Type:      "position_update",
		PlayerID:  playerID,
		Position:  &position,
		Username:  username,
		Timestamp: time.Now().UnixMilli(),
	}

	go broadcastToRoomAsync(room, playerID, message)
}

// handlePositionUpdate legacy function for compatibility
func (rm *RoomManager) handlePositionUpdate(playerID string, position Position, username string) {
	rm.handlePositionUpdateOptimized(playerID, position, username)
}

// GetManagerStats returns comprehensive room manager statistics
func (rm *RoomManager) GetManagerStats() map[string]interface{} {
	rm.stats.mu.RLock()
	defer rm.stats.mu.RUnlock()

	rm.mu.RLock()
	roomCount := len(rm.rooms)
	rm.mu.RUnlock()

	rm.playerMu.RLock()
	playerCount := len(rm.playerToRoom)
	rm.playerMu.RUnlock()

	return map[string]interface{}{
		"total_rooms_created":    rm.stats.totalRoomsCreated,
		"total_players_served":   rm.stats.totalPlayersServed,
		"current_active_rooms":   roomCount,
		"current_active_players": playerCount,
		"cleanup_operations":     rm.stats.cleanupOperations,
		"optimization_features": map[string]bool{
			"o1_player_lookup":        true,
			"reduced_lock_contention": true,
			"automatic_cleanup":       true,
			"player_to_room_mapping":  true,
		},
	}
}

// Shutdown gracefully shuts down the room manager
func (rm *RoomManager) Shutdown() {
	log.Println("Shutting down room manager...")
	rm.cleanupCancel()
	rm.cleanupWG.Wait()
	log.Println("Room manager shutdown complete")
}
