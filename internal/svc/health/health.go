// If you are AI: This file implements the health check endpoint for monitoring and integration tests.

package health

import (
	"net/http"
)

// Service provides health check functionality.
type Service struct{}

// New creates a new health service instance.
func New() *Service {
	return &Service{}
}

// RegisterRoutes adds health check routes to the provided mux.
// Currently registers /healthz which returns 200 OK.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
}

// handleHealth responds to health check requests.
// Returns 200 OK to indicate the server is running.
func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
}
