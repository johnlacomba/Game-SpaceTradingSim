# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o space-trader-server ./cmd/server

# Final stage
FROM alpine:latest

# Install ca-certificates for SSL and wget for health checks
RUN apk --no-cache add ca-certificates wget

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/space-trader-server .

# Create certs directory
RUN mkdir -p /app/certs

# Expose HTTPS port
EXPOSE 8443

# Run the server
CMD ["./space-trader-server", "-https-port=8443", "-tls-only"]
