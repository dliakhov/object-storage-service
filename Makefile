BINARY     := object-storage-service
CMD        := ./cmd/server
IMAGE_NAME := object-storage-service

.PHONY: build test lint docker-build

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -race ./...

lint:
	golangci-lint run ./...

docker-build:
	docker build -t $(IMAGE_NAME) .
