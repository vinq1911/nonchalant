// If you are AI: This file implements the WebSocket handler for FLV stream requests.
// Handles GET /ws/{app}/{name} requests and manages subscriber lifecycle.

package wsflv

import (
	"net/http"
	"strings"

	"nonchalant/internal/core/bus"

	"github.com/gorilla/websocket"
)

// Handler handles WebSocket-FLV requests.
type Handler struct {
	registry *bus.Registry
	upgrader websocket.Upgrader
}

// NewHandler creates a new WebSocket-FLV handler.
func NewHandler(registry *bus.Registry) *Handler {
	return &Handler{
		registry: registry,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for now
				// NOTE: In production, this should be restricted
				return true
			},
		},
	}
}

// ServeHTTP handles WebSocket upgrade and FLV streaming.
// Endpoint: GET /ws/{app}/{name}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /ws/{app}/{name}
	urlPath := strings.TrimPrefix(r.URL.Path, "/ws/")
	if urlPath == r.URL.Path {
		// Path doesn't start with /ws/
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Split into app and name
	parts := strings.SplitN(urlPath, "/", 2)
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	app := parts[0]
	name := parts[1]

	// Get stream from registry
	streamKey := bus.NewStreamKey(app, name)
	stream := h.registry.Get(streamKey)
	if stream == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Check if stream has a publisher
	if !stream.HasPublisher() {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade failed, response already sent
		return
	}

	// Create subscriber
	sub := NewSubscriber(conn, stream)
	defer func() {
		sub.Detach()
		conn.Close()
	}()

	// Attach to stream
	sub.Attach()

	// Write FLV header
	// NOTE: We assume both audio and video for now
	// In a real implementation, we'd detect from stream metadata
	if err := sub.WriteHeader(true, true); err != nil {
		return
	}

	// Process messages until connection closes
	// NOTE: This blocks until client disconnects or error occurs
	if err := sub.ProcessMessages(); err != nil {
		// Client disconnected or error occurred
		return
	}
}

// RegisterRoutes registers WebSocket-FLV routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/", h.ServeHTTP)
}
