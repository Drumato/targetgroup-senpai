# Build stage
FROM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download and verify dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags '-s -w -buildid=' -o main .

# Final stage - using distroless for better security and smaller size
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Use nonroot user (numeric UID/GID of distroless nonroot) for better security
USER 65532:65532

# Run the binary
ENTRYPOINT ["./main"]
