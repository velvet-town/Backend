package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"velvet/Routing"

	"velvet/config"

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

	// Initialize database
	if err := config.InitDB(); err != nil {
		log.Fatal("Error initializing database:", err)
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
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Expose-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		mux.ServeHTTP(w, r)
	})

	// Setup routes
	playerRouter := Routing.SetupPlayerRoutes()
	mux.Handle("/player/", playerRouter)
	authRouter := Routing.SetupAuthRoutes()
	mux.Handle("/auth/", authRouter)

	// Start server
	fmt.Printf("Server starting on port %s...\n", port)
	if err := http.ListenAndServe(port, corsHandler); err != nil {
		log.Fatal("Error starting server: ", err)
	}
}
