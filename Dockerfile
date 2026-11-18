# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o zkteco-api ./cmd/api

# Final Minimal Runtime Stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /app/zkteco-api .
COPY .env.example .env

EXPOSE 4000

ENTRYPOINT ["./zkteco-api"]
