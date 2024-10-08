# Dockerfile for Go application
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire app source code
COPY . .

# Build the Go application
RUN go build -o main .

# Start a new, smaller image for running the app
FROM alpine:latest
WORKDIR /app

# Copy the Go binary from the builder stage
COPY --from=builder /app/main .

# Run the application
CMD ["./main", "-workers=3"]
