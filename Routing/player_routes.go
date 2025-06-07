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

// SetupPlayerRoutes configures all player-related routes
func SetupPlayerRoutes() *config.Router {
	router := config.NewRouter("/player")

	// Join room endpoint
	router.HandleFunc("/join-room", func(w http.ResponseWriter, r *http.Request) {
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

		// Get room manager instance
		roomManager := Player_Logic.GetRoomManager()

		// Add player to a room
		room, err := roomManager.AddPlayer(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Return room information
		response := struct {
			RoomID  string   `json:"room_id"`
			Players []string `json:"players"`
		}{
			RoomID:  room.ID,
			Players: make([]string, 0, len(room.Players)),
		}

		// Get all player IDs in the room
		for playerID := range room.Players {
			response.Players = append(response.Players, playerID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

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

		// Get room manager instance
		roomManager := Player_Logic.GetRoomManager()

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
	router.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get token from URL query parameter
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Unauthorized - No token provided", http.StatusUnauthorized)
			return
		}

		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Error upgrading to WebSocket: %v", err)
			return
		}

		// Get room manager instance
		roomManager := Player_Logic.GetRoomManager()

		// Handle WebSocket connection
		roomManager.HandleWebSocket(conn, token)
	})

	return router
}
