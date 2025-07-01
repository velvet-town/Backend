package Routing

import (
	"encoding/json"
	"log"
	"net/http"
	"velvet/Player_Logic"
	"velvet/config"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

var roomManager = Player_Logic.GetRoomManager()

// SetupPlayerRoutes configures all player-related routes
func SetupPlayerRoutes() *config.Router {
	router := config.NewRouter("/player")

	// Join room endpoint
	router.HandleFunc("/join-room", handleJoinRoom)

	// Join specific room endpoint
	router.HandleFunc("/join-specific-room", handleJoinSpecificRoom)

	// Leave room endpoint
	router.HandleFunc("/leave-room", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get token from Authorization header
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Remove player from room
		roomManager.RemovePlayer(token)

		// Return success response
		response := struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: true,
			Message: "Successfully left the room",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Database stats endpoint for monitoring
	router.HandleFunc("/db-stats", handleDatabaseStats)

	// WebSocket connection stats endpoint for monitoring
	router.HandleFunc("/ws-stats", handleWebSocketStats)

	// WebSocket endpoint for real-time communication
	router.HandleFunc("/ws", Player_Logic.HandleWebSocket)

	return router
}

// handleDatabaseStats returns database connection pool statistics
func handleDatabaseStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := config.GetDBStats()

	response := map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration_ms":     stats.WaitDuration.Milliseconds(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding database stats response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleWebSocketStats returns WebSocket connection statistics
func handleWebSocketStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	wsStats := Player_Logic.GetConnectionStats()
	dbStats := config.GetDBStats()
	roomStats := roomManager.GetRoomStats()
	managerStats := roomManager.GetManagerStats()

	response := map[string]interface{}{
		"websocket": wsStats,
		"database": map[string]interface{}{
			"active_connections": dbStats.OpenConnections,
			"max_connections":    dbStats.MaxOpenConnections,
		},
		"rooms":        roomStats,
		"room_manager": managerStats,
		"server_performance": map[string]interface{}{
			"buffer_size_kb":          8, // 8KB buffers
			"batching_enabled":        true,
			"async_broadcasting":      true,
			"connection_pooling":      true,
			"o1_player_lookup":        true,
			"reduced_lock_contention": true,
			"automatic_cleanup":       true,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding WebSocket stats response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleJoinRoom handles player joining a room
func handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get player ID from authorization header
	playerID := r.Header.Get("Authorization")
	if playerID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("Join room request received")
	log.Printf("Adding player %s to room", playerID)

	// Add player to room
	room, err := roomManager.AddPlayer(playerID)
	if err != nil {
		log.Printf("Error adding player to room: %v", err)
		http.Error(w, "Failed to join room", http.StatusInternalServerError)
		return
	}

	// 💾 Update last_room in User table (async - non-blocking)
	config.UpdateLastRoomAsync(playerID, room.ID)

	// Get all players in the room
	players := make([]map[string]interface{}, 0)
	for id, player := range room.Players {
		players = append(players, map[string]interface{}{
			"id": id,
			"position": map[string]float64{
				"x": player.Position.X,
				"y": player.Position.Y,
			},
		})
	}

	// Send response
	response := map[string]interface{}{
		"room_id": room.ID,
		"players": players,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("Join room request completed successfully")
}

// handleJoinSpecificRoom handles player joining a specific room
func handleJoinSpecificRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get player ID from authorization header
	playerID := r.Header.Get("Authorization")
	if playerID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body to get room ID
	type RequestBody struct {
		RoomID string `json:"room_id"`
	}
	var body RequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.RoomID == "" {
		http.Error(w, "room_id is required", http.StatusBadRequest)
		return
	}

	// Validate room ID format (should be alphanumeric, max 10 characters for safety)
	if len(body.RoomID) > 10 {
		http.Error(w, "room_id too long (max 10 characters)", http.StatusBadRequest)
		return
	}

	log.Printf("Join specific room request received - Player: %s, Room: %s", playerID, body.RoomID)

	// Add player to specific room
	room, err := roomManager.AddPlayerToSpecificRoom(playerID, body.RoomID)
	if err != nil {
		log.Printf("Error adding player to specific room: %v", err)
		// Return the specific error message
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 💾 Update last_room in User table (async - non-blocking)
	config.UpdateLastRoomAsync(playerID, room.ID)

	// Get all players in the room
	players := make([]map[string]interface{}, 0)
	for id, player := range room.Players {
		players = append(players, map[string]interface{}{
			"id": id,
			"position": map[string]float64{
				"x": player.Position.X,
				"y": player.Position.Y,
			},
		})
	}

	// Send response
	response := map[string]interface{}{
		"room_id": room.ID,
		"players": players,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("Join specific room request completed successfully - Player: %s, Room: %s", playerID, body.RoomID)
}
