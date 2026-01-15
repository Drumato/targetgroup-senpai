# Build stage
FROM golang:1.25.5 AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-w -s' -o main .

# Final stage - using distroless for better security and smaller size
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Use nonroot user for better security
USER nonroot:nonroot

# Run the binary
ENTRYPOINT ["./main"]