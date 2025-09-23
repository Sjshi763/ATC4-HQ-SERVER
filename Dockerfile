# Stage 1: Build the Go application
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum to download dependencies
COPY go.mod ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application for a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./service.go

# Stage 2: Create the final, minimal image
FROM alpine:latest

WORKDIR /root/

# Copy the pre-built binary from the builder stage
COPY --from=builder /server .

# Copy the 'files' directory which contains the files to be served
COPY files ./files

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the executable
CMD ["./server"]
