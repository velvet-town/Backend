package Routing

import (
	"net/http"
	"velvet/config"
)

// SetupAuthRoutes configures all authentication-related routes
func SetupAuthRoutes() *config.Router {
	router := config.NewRouter("/auth")

	// Login route
	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("Login endpoint"))
	})

	// Register route
	router.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("Register endpoint"))
	})

	// Logout route
	router.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("Logout endpoint"))
	})

	// Forgot password route
	router.HandleFunc("/forgot-password", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("Forgot password endpoint"))
	})

	return router
}
