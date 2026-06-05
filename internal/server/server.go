package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	readTimeout       = 5 * time.Second
	writeTimeout      = 10 * time.Second
	readHeaderTimeout = 2 * time.Second
	idleTimeout       = 60 * time.Second
)

type Server struct {
	logger     *slog.Logger
	httpServer *http.Server
}

func New(port int, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		logger: logger,
		httpServer: &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           mux,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
		},
	}
	s.registerRoutes(mux)
	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Run() error {
	s.logger.Info("starting server", slog.String("addr", s.httpServer.Addr))
	return s.httpServer.ListenAndServe()
}
