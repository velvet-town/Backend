package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"velvet/Player_Logic"
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

	// Initialize room manager (starts cleanup routines)
	roomManager := Player_Logic.GetRoomManager()

	// Set up graceful shutdown
	defer func() {
		log.Println("Starting graceful shutdown...")

		// Shutdown room manager cleanup routines
		roomManager.Shutdown()

		// Close database connections
		if err := config.CloseDB(); err != nil {
			log.Printf("Error closing database: %v", err)
		}

		log.Println("Graceful shutdown completed")
	}()

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

	// Create HTTP server
	server := &http.Server{
		Addr:    port,
		Handler: corsHandler,
	}

	// Channel to listen for interrupt signal to terminate server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		fmt.Printf("Server starting on port %s...\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Error starting server: ", err)
		}
	}()

	// Wait for interrupt signal
	<-quit
	log.Println("Shutting down server...")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
