# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Cache module downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/server ./cmd/server

# ── Runtime stage ──────────────────────────────────────────────────────────────
# distroless/static:nonroot runs as uid 65532, no shell, minimal attack surface
FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 8080

ENTRYPOINT ["/app/server"]
