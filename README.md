# object-storage-service

A lightweight HTTP object storage service written in Go. Objects are organised into buckets and can be stored either in memory or persisted to disk with content deduplication.

## Features

- PUT / GET / DELETE objects under `/{bucket}/{objectID}`
- Two storage backends: **in-memory** (default) and **file-based**
- Per-bucket content deduplication via SHA-256 — identical content is stored once regardless of how many objects reference it
- File backend: atomic writes, SHA-256 integrity check on every read, startup cleanup of crash-orphaned blobs
- Configurable via CLI flags or environment variables

## Requirements

- Go 1.26.4+
- Docker (optional)

## Running locally

```bash
# Build
make build

# Memory storage (default)
./object-storage-service --port 8080

# File storage
./object-storage-service --port 8080 --storage=file --storage-dir=./data
```

## Running in Docker

### Using the published image

The image is automatically built and pushed to GitHub Container Registry on every merge to `main`.

```bash
# Memory storage (default)
docker pull ghcr.io/dliakhov/object-storage-service:latest
docker run -p 8080:8080 ghcr.io/dliakhov/object-storage-service:latest

# File storage — mount a volume and pass env vars
docker run -p 8080:8080 \
  -e STORAGE_MODE=file \
  -e STORAGE_DIR=/data \
  -v ./data:/data \
  ghcr.io/dliakhov/object-storage-service:latest
```

A SHA-tagged image is published alongside `latest` for each release, useful when you need a pinned version:

```bash
docker pull ghcr.io/dliakhov/object-storage-service:sha-abc1234
```

### Building locally

```bash
# Build the image
make docker-build

# Memory storage
docker run -p 8080:8080 object-storage-service

# File storage — mount a volume and pass env vars
docker run -p 8080:8080 \
  -e STORAGE_MODE=file \
  -e STORAGE_DIR=/data \
  -v ./data:/data \
  object-storage-service

# Or via Makefile
make docker-run          # memory
make docker-run-file     # file, mounts ./data
```

## Configuration

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--port` | — | `8080` | TCP port to listen on |
| `--storage` | `STORAGE_MODE` | `memory` | Backend: `memory` or `file` |
| `--storage-dir` | `STORAGE_DIR` | `./data` | Root directory for file storage |

CLI flags take precedence over environment variables.

## API

| Method | Path | Status | Description |
|--------|------|--------|-------------|
| `PUT` | `/objects/{bucket}/{objectID}` | 201 | Store an object; creates the bucket if needed |
| `GET` | `/objects/{bucket}/{objectID}` | 200 | Retrieve an object |
| `DELETE` | `/objects/{bucket}/{objectID}` | 200 | Remove an object |
| `GET` | `/health` | 200 | Health check |

**PUT** response body: `{"id":"<objectID>"}`

**Error codes:** `400` invalid bucket or objectID name · `404` object not found · `500` internal error

Bucket and objectID names must match `[a-zA-Z0-9._-]+` and must not be `.` or `..`.

### Examples

```bash
# Store an object
curl -X PUT http://localhost:8080/objects/my-bucket/hello.txt -d "Hello, world!"
# → {"id":"hello.txt"}

# Retrieve it
curl http://localhost:8080/objects/my-bucket/hello.txt
# → Hello, world!

# Delete it
curl -X DELETE http://localhost:8080/objects/my-bucket/hello.txt
```

## Development

```bash
make test   # run tests with race detector
make lint   # run golangci-lint
make build  # compile binary → ./object-storage-service
```

## AI assistance

This project was built with AI assistance. See [AI_ASSISTANCE.md](./AI_ASSISTANCE.md) for a full account of what was AI-generated, what decisions were made by hand, and how the output was validated.
