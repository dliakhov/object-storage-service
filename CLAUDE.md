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
```

`cmd/server/main.go` parses the `--port` flag, creates a `*server.Server`, and calls `Run()`.
`internal/server/server.go` owns the `http.Server` (with timeouts), mux, and handler methods.

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
