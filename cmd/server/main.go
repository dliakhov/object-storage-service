package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/dliakhov/object-storage-service/internal/server"
	"github.com/dliakhov/object-storage-service/internal/storage"
)

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

	srv := server.New(*port, logger, store)
	if err := srv.Run(); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		os.Exit(1)
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
