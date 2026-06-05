BINARY     := object-storage-service
CMD        := ./cmd/server
IMAGE_NAME := object-storage-service

.PHONY: build test lint docker-build docker-run docker-run-file

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -race ./...

lint:
	golangci-lint run ./...

docker-build:
	docker build -t $(IMAGE_NAME) .

docker-run: docker-build
	docker run --rm -p 8080:8080 $(IMAGE_NAME)

# Run with file storage, mounting ./data as the persistent volume.
# For a named volume: docker run -e STORAGE_MODE=file -e STORAGE_DIR=/data -v object-data:/data -p 8080:8080 $(IMAGE_NAME)
docker-run-file: docker-build
	mkdir -p data
	docker run --rm -p 8080:8080 \
		-e STORAGE_MODE=file \
		-e STORAGE_DIR=/data \
		-v "$(PWD)/data:/data" \
		$(IMAGE_NAME)
