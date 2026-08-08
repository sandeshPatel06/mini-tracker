# =====================================================================
# Multi-stage Dockerfile for the get-Hike backend API server
# This is a SEPARATE component from the Wails desktop app.
# It can be used to run a standalone HTTP API + web viewer on another
# machine or as a data aggregation endpoint.
# =====================================================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build only the server binary (no Wails, no desktop deps)
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /get-hike-server \
    ./cmd/server

# =====================================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app
COPY --from=builder /get-hike-server .

# Data volume (SQLite DB + images)
VOLUME ["/data"]

# API port
EXPOSE 8080

ENV DATA_DIR=/data
ENV PORT=8080

ENTRYPOINT ["/app/get-hike-server"]
