package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"velvet/Routing"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

func main() {
	// Load environment variables
	if err := godotenv.Load("config/config.env"); err != nil {
		log.Fatal("Error loading config.env file:", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	} else {
		port = ":" + port
	}

	// Create a new ServeMux
	mux := http.NewServeMux()

	// Create CORS middleware handler
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigin := "http://localhost:3000"

		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Credentials", "false")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Upgrade, Connection, Sec-WebSocket-Key, Sec-WebSocket-Version, Sec-WebSocket-Protocol")
		w.Header().Set("Access-Control-Expose-Headers", "Upgrade")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		mux.ServeHTTP(w, r)
	})

	// Setup routes
	playerRouter := Routing.SetupPlayerRoutes()
	mux.Handle("/player/", playerRouter)

	// Start server
	fmt.Printf("Server starting on port %s...\n", port)
	if err := http.ListenAndServe(port, corsHandler); err != nil {
		log.Fatal("Error starting server: ", err)
	}
}
