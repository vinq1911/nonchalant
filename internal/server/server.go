// If you are AI: This file implements the HTTP server lifecycle and routing.

package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nonchalant/internal/config"
	"nonchalant/internal/core/bus"
	"nonchalant/internal/svc/health"
	"nonchalant/internal/svc/httpflv"
	"nonchalant/internal/svc/rtmp"
	"nonchalant/internal/svc/wsflv"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	httpServer *http.Server
	healthSvc  *health.Service
	httpflvSvc *httpflv.Service
	wsflvSvc   *wsflv.Service
	rtmpServer *rtmp.Server
	registry   *bus.Registry
}

// New creates a new server instance with the given configuration.
// The server is not started until Start is called.
func New(cfg *config.Config) *Server {
	mux := http.NewServeMux()

	healthSvc := health.New()
	healthSvc.RegisterRoutes(mux)

	// Create bus registry
	registry := bus.NewRegistry()

	// Create HTTP-FLV service
	httpflvSvc := httpflv.NewService(registry)
	httpflvSvc.RegisterRoutes(mux)

	// Create WebSocket-FLV service
	wsflvSvc := wsflv.NewService(registry)
	wsflvSvc.RegisterRoutes(mux)

	// Create RTMP server
	rtmpServer := rtmp.NewServer(registry)

	// HTTP server listens on HTTP port
	// Health endpoint is also available on this port
	// NOTE: Health port is kept for backward compatibility but not used
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.HTTPPort),
		Handler: mux,
	}

	return &Server{
		httpServer: httpServer,
		healthSvc:  healthSvc,
		httpflvSvc: httpflvSvc,
		wsflvSvc:   wsflvSvc,
		rtmpServer: rtmpServer,
		registry:   registry,
	}
}

// Start begins serving HTTP requests and RTMP connections.
// This method blocks until the server is stopped or encounters an error.
func (s *Server) Start(cfg *config.Config) error {
	// Start RTMP server
	if err := s.rtmpServer.Listen(fmt.Sprintf(":%d", cfg.Server.RTMPPort)); err != nil {
		return fmt.Errorf("RTMP server listen: %w", err)
	}
	go func() {
		if err := s.rtmpServer.Accept(); err != nil {
			// Log error but don't fail startup
		}
	}()

	// Start HTTP server (blocks)
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

	// Close RTMP server
	if s.rtmpServer != nil {
		s.rtmpServer.Close()
	}

	return s.Shutdown(ctx)
}
