# Stage 1: Build
FROM golang:1.22-alpine AS builder

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .

# Build the Go binary
RUN go build -o proxy-server .

# Stage 2: Run
FROM alpine:latest

# Set working directory
WORKDIR /app

# Copy binary and environment file example
COPY --from=builder /app/proxy-server .
COPY .env.example .env

# Expose the port
EXPOSE 3000

# Start the server
CMD ["./proxy-server"]
