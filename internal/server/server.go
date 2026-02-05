// If you are AI: This file implements the HTTP server lifecycle and routing.

package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nonchalant/internal/config"
	"nonchalant/internal/svc/health"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	httpServer *http.Server
	healthSvc  *health.Service
}

// New creates a new server instance with the given configuration.
// The server is not started until Start is called.
func New(cfg *config.Config) *Server {
	mux := http.NewServeMux()

	healthSvc := health.New()
	healthSvc.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.HealthPort),
		Handler: mux,
	}

	return &Server{
		httpServer: httpServer,
		healthSvc:  healthSvc,
	}
}

// Start begins serving HTTP requests.
// This method blocks until the server is stopped or encounters an error.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server with a timeout.
// Returns an error if shutdown fails or times out.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// ShutdownWithTimeout stops the server with a fixed 5-second timeout.
// This is a convenience wrapper around Shutdown.
func (s *Server) ShutdownWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}
