# object-storage-service

Go 1.26.4 HTTP service. Module: `github.com/dliakhov/object-storage-service`.

## Commands

```bash
make build   # compile binary → ./object-storage-service
make test    # run tests with race detector
make lint    # run golangci-lint
```

Run locally:
```bash
./object-storage-service --port 8080
```

Run in Docker:
```bash
docker build -t object-storage-service .
docker run -p 8080:8080 object-storage-service
# Override port:
docker run -p 9090:9090 object-storage-service --port 9090
```

## Project Structure

```
cmd/server/          # main entry point — CLI flag parsing only
internal/server/     # HTTP server, route registration, handlers
internal/storage/    # Store interface + MemoryStore implementation
```

`cmd/server/main.go` parses the `--port` flag, creates a `*server.Server` with a `storage.NewMemoryStore()`, and calls `Run()`.
`internal/server/server.go` owns the `http.Server` (with timeouts), mux, and handler methods.
`internal/storage/store.go` defines the `Store` interface and `NotFoundError`.
`internal/storage/memory.go` implements `MemoryStore` with per-object locking and per-bucket content deduplication.

## API

| Method | Path | Success | Description |
|--------|------|---------|-------------|
| PUT | `/objects/{bucket}/{objectID}` | 201 `{"id":"<objectID>"}` | Store object; creates bucket if needed |
| GET | `/objects/{bucket}/{objectID}` | 200 body bytes | Retrieve object |
| DELETE | `/objects/{bucket}/{objectID}` | 200 | Remove object |
| GET | `/health` | 200 | Health check |

## Storage Design

`MemoryStore` uses three-level locking for maximum concurrency:

- `m.mu` (store-level RWMutex) — protects the `buckets` map
- `b.indexMu` (bucket-level RWMutex) — protects the `objects` map within a bucket
- `e.mu` (per-object RWMutex) — protects each object's `hash` field
- `b.blobsMu` (bucket-level RWMutex) — protects `blobs` and `refcount` maps

Lock ordering is always `e.mu` → `b.blobsMu`. Bucket and entry creation use a double-check pattern (RLock fast path, Lock slow path) to avoid write-lock contention on the common read case.

Content deduplication is per-bucket: objects with identical content share one blob, tracked by SHA-256 hash with a reference count. The blob is freed when refcount reaches 0.

## Adding a New Endpoint

1. Add a handler method on `*Server` in `internal/server/server.go`:
   ```go
   func (s *Server) handleFoo(w http.ResponseWriter, r *http.Request) { ... }
   ```
2. Register it in `registerRoutes`:
   ```go
   mux.HandleFunc("GET /foo", s.handleFoo)
   ```
3. Add a test case to the table in `internal/server/server_test.go`.

Use Go 1.22+ method-in-pattern syntax (`"GET /path"`) for all route registrations.

## Key Linting Rules

The project runs golangci-lint v2 with a strict ruleset (`.golangci.yml`). Rules that commonly affect new code:

- **No package-level `var`** (`gochecknoglobals`) — use constructor arguments or constants.
- **No `init()` functions** (`gochecknoinits`).
- **No named return values** (`nonamedreturns`).
- **All errors must be handled** (`errcheck`) — no silent `_` discards on the left of an assignment.
- **Wrap errors with `%w`** (`errorlint`) when returning them up the call stack.
- **HTTP requests need a context** (`noctx`) — always use `http.NewRequestWithContext`.
- **Close HTTP response bodies** (`bodyclose`) — drain with `io.Copy(io.Discard, resp.Body)` before closing.
- **Use stdlib constants** (`usestdlibvars`) — `http.MethodGet`, `http.StatusOK`, etc., not bare strings/numbers.

Run `make lint` before committing. Fix real issues; use `//nolint:<linter> // reason` only for confirmed false positives.
