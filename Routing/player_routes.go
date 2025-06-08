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

	// WebSocket endpoint for real-time communication
	router.HandleFunc("/ws", handleWebSocket)

	return router
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

// handleWebSocket handles WebSocket connections
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	Player_Logic.HandleWebSocket(w, r)
}
