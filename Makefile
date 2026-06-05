BINARY     := object-storage-service
CMD        := ./cmd/server
IMAGE_NAME := object-storage-service

.PHONY: build test lint docker-build docker-run

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
