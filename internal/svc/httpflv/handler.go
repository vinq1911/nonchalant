// If you are AI: This file implements the HTTP handler for FLV stream requests.
// Handles GET /{app}/{name}.flv requests and manages subscriber lifecycle.

package httpflv

import (
	"net/http"
	"path"
	"strings"

	"nonchalant/internal/core/bus"
)

// Handler handles HTTP-FLV requests.
type Handler struct {
	registry *bus.Registry
}

// NewHandler creates a new HTTP-FLV handler.
func NewHandler(registry *bus.Registry) *Handler {
	return &Handler{
		registry: registry,
	}
}

// ServeHTTP handles HTTP requests for FLV streams.
// Endpoint: GET /{app}/{name}.flv
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /{app}/{name}.flv
	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasSuffix(urlPath, ".flv") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Remove .flv extension
	streamPath := strings.TrimSuffix(urlPath, ".flv")

	// Split into app and name
	parts := strings.SplitN(streamPath, "/", 2)
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

	// Create subscriber
	sub := NewSubscriber(w, stream)
	defer sub.Detach()

	// Attach to stream
	sub.Attach()

	// Write FLV header
	// NOTE: We assume both audio and video for now
	// In a real implementation, we'd detect from stream metadata
	if err := sub.WriteHeader(true, true); err != nil {
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.(http.Flusher).Flush()

	// Process messages until connection closes
	// NOTE: This blocks until client disconnects or error occurs
	if err := sub.ProcessMessages(); err != nil {
		// Client disconnected or error occurred
		return
	}
}

// RegisterRoutes registers HTTP-FLV routes on the given mux.
// Routes are registered with a pattern matcher for .flv files.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Use a handler function that checks for .flv extension
	// This allows other routes (like /healthz) to work normally
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle .flv requests, let others fall through
		if path.Ext(r.URL.Path) == ".flv" {
			h.ServeHTTP(w, r)
		} else {
			// Not an FLV request, return 404
			// NOTE: This means /healthz must be registered before this
			w.WriteHeader(http.StatusNotFound)
		}
	})
}
