package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/dliakhov/object-storage-service/internal/server"
	"github.com/dliakhov/object-storage-service/internal/storage"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := server.New(*port, logger, storage.NewMemoryStore())

	if err := srv.Run(); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
