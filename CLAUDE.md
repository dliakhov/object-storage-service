# object-storage-service

Go 1.26.4 HTTP service. Module: `github.com/dliakhov/object-storage-service`.

## Commands

```bash
make build   # compile binary → ./object-storage-service
make test    # run tests with race detector
make lint    # run golangci-lint
```

Run locally (memory, default):
```bash
./object-storage-service --port 8080
```

Run locally with file storage:
```bash
./object-storage-service --port 8080 --storage=file --storage-dir=./data
```

Run in Docker (memory):
```bash
docker build -t object-storage-service .
docker run -p 8080:8080 object-storage-service
```

Run in Docker with file storage:
```bash
docker run -p 8080:8080 \
  -e STORAGE_MODE=file \
  -e STORAGE_DIR=/data \
  -v ./data:/data \
  object-storage-service
# Or via Makefile:
make docker-run-file
```

## Project Structure

```
cmd/server/          # main entry point — CLI flag parsing and storage wiring
internal/server/     # HTTP server, route registration, handlers
internal/storage/    # Store interface, MemoryStore, FileStore
```

`cmd/server/main.go` parses `--port`, `--storage`, and `--storage-dir` flags (each with an env-var fallback: `PORT`, `STORAGE_MODE`, `STORAGE_DIR`), constructs the chosen store, and calls `srv.Run()`.
`internal/server/server.go` owns the `http.Server` (with timeouts), mux, and handler methods.
`internal/storage/store.go` defines the `Store` interface, `NotFoundError`, and `InvalidInputError`.
`internal/storage/memory.go` implements `MemoryStore` with per-object locking and per-bucket content deduplication.
`internal/storage/filestore.go` implements `FileStore` with per-bucket locking, content deduplication via SHA-256, atomic writes, and startup GC.

## CLI Flags / Environment Variables

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--port` | — | `8080` | TCP port to listen on |
| `--storage` | `STORAGE_MODE` | `memory` | Backend: `memory` or `file` |
| `--storage-dir` | `STORAGE_DIR` | `./data` | Root directory for file storage |

CLI flags always take precedence over environment variables.

## API

| Method | Path | Success | Error |Description |
|--------|------|---------|-------|-------------|
| PUT | `/objects/{bucket}/{objectID}` | 201 `{"id":"<objectID>"}` | 400 invalid name | Store object; creates bucket if needed |
| GET | `/objects/{bucket}/{objectID}` | 200 body bytes | 400 invalid name, 404 not found | Retrieve object |
| DELETE | `/objects/{bucket}/{objectID}` | 200 | 400 invalid name, 404 not found | Remove object |
| GET | `/health` | 200 | — | Health check |

Bucket and objectID names must match `[a-zA-Z0-9._-]+` and must not be `.` or `..`. Invalid names return 400.

## MemoryStore Design

`MemoryStore` uses three-level locking for maximum concurrency:

- `m.mu` (store-level RWMutex) — protects the `buckets` map
- `b.indexMu` (bucket-level RWMutex) — protects the `objects` map within a bucket
- `e.mu` (per-object RWMutex) — protects each object's `hash` field
- `b.blobsMu` (bucket-level RWMutex) — protects `blobs` and `refcount` maps

Lock ordering is always `e.mu` → `b.blobsMu`. Bucket and entry creation use a double-check pattern (RLock fast path, Lock slow path) to avoid write-lock contention on the common read case.

Content deduplication is per-bucket: objects with identical content share one blob, tracked by SHA-256 hash with a reference count. The blob is freed when refcount reaches 0.

## FileStore Design

Directory layout on disk:
```
<root>/
  <bucket>/
    blobs/<sha256>      ← blob content (one file per unique content hash)
    objects/<objectID>  ← text file containing the sha256 of the referenced blob
```

`FileStore` uses two-level locking:

- `f.mu` (store-level RWMutex) — protects the `buckets` map
- `bl.mu` (bucket-level RWMutex) — serialises all operations within one bucket

`Get` and `Delete` use `requireBucketLock`, which checks disk when the in-memory map is empty (e.g. after a restart) to avoid inserting map entries for non-existent buckets.

Key implementation details:
- **Atomic writes**: `os.CreateTemp` + `os.Rename` (POSIX atomic) prevents partial reads
- **Content deduplication**: blob file is written only when `os.Stat` returns `ErrNotExist`
- **Integrity check on `Get`**: SHA-256 of the blob is recomputed and compared against the filename
- **Path confinement**: `validateName` allowlist + `confine()` defence-in-depth on every constructed path, including blob paths derived from ref-file content
- **Startup GC**: `NewFileStore` calls `cleanupStale` to remove leftover `.tmp-*` files and orphaned blobs from any prior crash

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
