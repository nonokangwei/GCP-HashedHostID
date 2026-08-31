package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"hosthashedid/internal/store"
)

// Server wraps http.Server for graceful lifecycle management.
type Server struct {
	httpServer *http.Server
	handler    *APIHandler
}

// NewServer creates a new HTTP Server.
func NewServer(host string, port int, syncer SyncerService, st store.Store) *Server {
	mux := http.NewServeMux()
	handler := NewAPIHandler(syncer, st)
	handler.RegisterRoutes(mux)

	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &Server{
		httpServer: srv,
		handler:    handler,
	}
}

// Start starts listening for HTTP requests.
func (s *Server) Start() error {
	log.Printf("[HTTP Server] Listening on http://%s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Printf("[HTTP Server] Shutting down...")
	return s.httpServer.Shutdown(ctx)
}
