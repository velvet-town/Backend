package config

import (
	"net/http"
	"strings"
)

// Router represents our custom router
type Router struct {
	routes map[string]http.HandlerFunc
	prefix string
}

// NewRouter creates a new router instance
func NewRouter(prefix string) *Router {
	return &Router{
		routes: make(map[string]http.HandlerFunc),
		prefix: prefix,
	}
}

// HandleFunc adds a new route to the router
func (r *Router) HandleFunc(path string, handler http.HandlerFunc) {
	fullPath := r.prefix + path
	r.routes[fullPath] = handler
}

// ServeHTTP implements the http.Handler interface
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// Check if the path starts with our prefix
	if !strings.HasPrefix(path, r.prefix) {
		http.NotFound(w, req)
		return
	}

	// Look for the handler
	if handler, exists := r.routes[path]; exists {
		handler(w, req)
		return
	}

	http.NotFound(w, req)
}
