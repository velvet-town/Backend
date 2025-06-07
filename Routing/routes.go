package Routing

import (
	"net/http"
)

// SetupRoutes configures all the routes for the application
func SetupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Home route
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to the Home Page"))
	})

	// Mount auth routes
	mux.Handle("/auth/", SetupAuthRoutes())

	// Mount player routes
	mux.Handle("/player/", SetupPlayerRoutes())

	return mux
}
