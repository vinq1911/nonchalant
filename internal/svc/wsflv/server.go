// If you are AI: This file provides WebSocket-FLV service integration.
// The service is integrated into the main HTTP server.

package wsflv

import (
	"net/http"

	"nonchalant/internal/core/bus"
)

// Service provides WebSocket-FLV streaming functionality.
type Service struct {
	handler *Handler
}

// NewService creates a new WebSocket-FLV service.
func NewService(registry *bus.Registry) *Service {
	return &Service{
		handler: NewHandler(registry),
	}
}

// RegisterRoutes registers WebSocket-FLV routes on the provided mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	s.handler.RegisterRoutes(mux)
}
