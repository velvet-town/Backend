package Routing

import (
	"encoding/json"
	"log"
	"net/http"
	"velvet/config"
)

// SetupAuthRoutes configures all authentication-related routes
func SetupAuthRoutes() *config.Router {
	router := config.NewRouter("/auth")

	// User exists endpoint
	router.HandleFunc("/user-exists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type reqBody struct {
			UserId string `json:"userId"`
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			log.Println("Decode error:", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if body.UserId == "" {
			http.Error(w, "userId is required", http.StatusBadRequest)
			return
		}
		var exists bool
		err := config.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM "User" WHERE "userId" = $1)`, body.UserId).Scan(&exists)
		if err != nil {
			log.Println("Database error:", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"exists": exists})
	})

	// Update or insert user endpoint
	router.HandleFunc("/update-user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type reqBody struct {
			UserId     string `json:"userId"`
			Username   string `json:"username"`
			Gender     string `json:"gender"`
			Email      string `json:"email"`
			ProfilePic string `json:"profile_pic"`
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			log.Println("Decode error:", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if body.UserId == "" || body.Username == "" || body.Gender == "" {
			http.Error(w, "userId, username, and gender are required", http.StatusBadRequest)
			return
		}
		_, err := config.DB.Exec(`
			INSERT INTO "User" ("userId", username, gender, email, profile_pic)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT ("userId") DO UPDATE SET username = $2, gender = $3, email = $4, profile_pic = $5
		`, body.UserId, body.Username, body.Gender, body.Email, body.ProfilePic)
		if err != nil {
			log.Println("Database error:", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	// Get user data by userId
	router.HandleFunc("/get-user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type reqBody struct {
			UserId string `json:"userId"`
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			log.Println("Decode error:", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if body.UserId == "" {
			http.Error(w, "userId is required", http.StatusBadRequest)
			return
		}
		var username, gender, email, profilePic string
		var lastRoom *string

		log.Printf("🔍 Fetching user data for userId: %s", body.UserId)
		err := config.DB.QueryRow(`SELECT username, gender, email, profile_pic, last_room FROM "User" WHERE "userId" = $1`, body.UserId).Scan(&username, &gender, &email, &profilePic, &lastRoom)
		if err != nil {
			log.Printf("❌ Database error getting user %s: %v", body.UserId, err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		lastRoomStr := ""
		if lastRoom != nil {
			lastRoomStr = *lastRoom
			log.Printf("✅ Found last_room for user %s: %s", body.UserId, lastRoomStr)
		} else {
			log.Printf("⚠️ No last_room found for user %s", body.UserId)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"username":    username,
			"gender":      gender,
			"email":       email,
			"profile_pic": profilePic,
			"last_room":   lastRoomStr,
		})
	})

	return router
}
