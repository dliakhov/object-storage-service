FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server ./cmd/server

FROM alpine:3.23

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=builder /app/server .

USER app

EXPOSE 8080

ENTRYPOINT ["./server"]
CMD ["--port", "8080"]
