# Stage 1: Build the Go application
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-w -s' -o pronto ./cmd/pronto

# Stage 2: Create minimal runtime image
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache git ca-certificates openssh-client

# Create a non-root user
RUN addgroup -S pronto && adduser -S pronto -G pronto

# Set working directory
WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/pronto /app/pronto

# Set ownership
RUN chown -R pronto:pronto /app

# Switch to non-root user
USER pronto

# Set entrypoint
ENTRYPOINT ["/app/pronto"]
