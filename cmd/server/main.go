package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dliakhov/object-storage-service/internal/server"
	"github.com/dliakhov/object-storage-service/internal/storage"
)

const shutdownTimeout = 30 * time.Second

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	storageMode := flag.String("storage", envOr("STORAGE_MODE", "memory"), "storage backend: memory or file")
	storageDir := flag.String("storage-dir", envOr("STORAGE_DIR", "./data"), "root directory for file storage (used when --storage=file)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	store, err := newStore(*storageMode, *storageDir, logger)
	if err != nil {
		logger.Error("failed to initialise storage", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := run(logger, server.New(*port, logger, store)); err != nil {
		logger.Error("server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run starts the server and blocks until it exits cleanly or a signal is received.
// Returning an error means the process should exit non-zero.
func run(logger *slog.Logger, srv *server.Server) error {
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-runErr:
		return err
	case sig := <-sigCh:
		logger.Info("shutting down", slog.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", slog.String("error", err.Error()))
		}
		return <-runErr
	}
}

func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func newStore(mode, dir string, logger *slog.Logger) (storage.Store, error) {
	switch mode {
	case "memory":
		logger.Info("using in-memory storage")
		return storage.NewMemoryStore(), nil
	case "file":
		logger.Info("using file storage", slog.String("dir", dir))
		return storage.NewFileStore(dir)
	default:
		return nil, fmt.Errorf("unknown storage mode %q; valid values: memory, file", mode)
	}
}
