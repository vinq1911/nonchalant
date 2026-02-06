// If you are AI: This file provides HTTP-FLV service integration.
// The service is integrated into the main HTTP server.

package httpflv

import (
	"net/http"

	"nonchalant/internal/core/bus"
)

// Service provides HTTP-FLV streaming functionality.
type Service struct {
	handler *Handler
}

// NewService creates a new HTTP-FLV service.
func NewService(registry *bus.Registry) *Service {
	return &Service{
		handler: NewHandler(registry),
	}
}

// RegisterRoutes registers HTTP-FLV routes on the provided mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	s.handler.RegisterRoutes(mux)
}
