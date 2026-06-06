package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/dliakhov/object-storage-service/internal/storage"
)

const (
	readTimeout       = 5 * time.Second
	writeTimeout      = 10 * time.Second
	readHeaderTimeout = 2 * time.Second
	idleTimeout       = 60 * time.Second

	// maxBodyBytes caps PUT request bodies at 64 MiB to prevent memory exhaustion.
	maxBodyBytes = 64 * 1024 * 1024

	bucketParam   = "bucket"
	objectIDParam = "objectID"
)

type putResponse struct {
	ID string `json:"id"`
}

type Server struct {
	logger     *slog.Logger
	httpServer *http.Server
	store      storage.Store
}

func New(port int, logger *slog.Logger, store storage.Store) *Server {
	mux := http.NewServeMux()
	s := &Server{
		logger: logger,
		store:  store,
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
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("PUT /objects/{bucket}/{objectID}", s.handlePutObject)
	mux.HandleFunc("GET /objects/{bucket}/{objectID}", s.handleGetObject)
	mux.HandleFunc("DELETE /objects/{bucket}/{objectID}", s.handleDeleteObject)
}

// handleHealth is the liveness probe — it returns 200 as long as the process is running.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReady is the readiness probe — it returns 503 when the storage backend is unavailable.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if checker, ok := s.store.(storage.Checker); ok {
		if err := checker.Check(r.Context()); err != nil {
			s.logger.Error("readiness check failed", slog.String("error", err.Error()))
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePutObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue(bucketParam)
	objectID := r.PathValue(objectIDParam)

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}

	if err := s.store.Put(r.Context(), bucket, objectID, data); err != nil {
		if _, ok := errors.AsType[storage.InvalidInputError](err); ok {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.logger.Error("store put", slog.String("bucket", bucket), slog.String("object_id", objectID), slog.String("error", err.Error()))
		http.Error(w, "failed to store object", http.StatusInternalServerError)
		return
	}

	b, err := json.Marshal(putResponse{ID: objectID})
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(b); err != nil {
		s.logger.Error("write response", slog.String("error", err.Error()))
	}
}

func (s *Server) handleGetObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue(bucketParam)
	objectID := r.PathValue(objectIDParam)

	data, err := s.store.Get(r.Context(), bucket, objectID)

	if _, ok := errors.AsType[storage.NotFoundError](err); ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if _, ok := errors.AsType[storage.InvalidInputError](err); ok {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err != nil {
		s.logger.Error("store get", slog.String("bucket", bucket), slog.String("object_id", objectID), slog.String("error", err.Error()))
		http.Error(w, "failed to get object", http.StatusInternalServerError)
		return
	}

	if data == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(data); err != nil { //nolint:gosec // G705: intentional — this endpoint exists to return stored content as-is
		s.logger.Error("write response", slog.String("error", err.Error()))
	}
}

func (s *Server) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue(bucketParam)
	objectID := r.PathValue(objectIDParam)

	err := s.store.Delete(r.Context(), bucket, objectID)

	if _, ok := errors.AsType[storage.NotFoundError](err); ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if _, ok := errors.AsType[storage.InvalidInputError](err); ok {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err != nil {
		s.logger.Error("store delete", slog.String("bucket", bucket), slog.String("object_id", objectID), slog.String("error", err.Error()))
		http.Error(w, "failed to delete object", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Run starts the HTTP server and blocks until it is stopped.
// It returns nil when the server is shut down cleanly via Shutdown.
func (s *Server) Run() error {
	s.logger.Info("starting server", slog.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully drains in-flight requests and stops the server.
// The provided context controls the drain deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
