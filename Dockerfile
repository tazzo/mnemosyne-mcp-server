# STAGE 1: Build
FROM golang:1.23-bookworm AS builder

WORKDIR /app

# Copy and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary (Static linking for Distroless)
RUN CGO_ENABLED=0 GOOS=linux go build -o mnemosyne-mcp cmd/mnemosyne-mcp/main.go

# STAGE 2: Final Image (Distroless)
FROM gcr.io/distroless/static-debian12

WORKDIR /

# Copy the binary from builder
COPY --from=builder /app/mnemosyne-mcp /mnemosyne-mcp

# Env defaults
ENV DB_PORT=5432
ENV DB_USER=mnemosyne

# Standard I/O based server doesn't strictly need EXPOSE but good for documentation
# if we later move to SSE/HTTP
EXPOSE 8080

ENTRYPOINT ["/mnemosyne-mcp"]
