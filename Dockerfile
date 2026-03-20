# Use the official Golang image to create a build artifact.
FROM golang:1.26-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go.mod and go.sum first for dependency caching
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN go build -o /wattpilot-exporter ./cmd/server

# Ensure the data directory exists even when it is empty and not tracked in git
RUN mkdir -p /app/data

# Start a new stage from scratch
FROM alpine:3.23

WORKDIR /app

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /wattpilot-exporter .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/data ./data

# Update Alpine packages to ensure latest security patches
RUN apk update && apk upgrade --no-cache

EXPOSE 8080

# Command to run the executable
CMD [ "/app/wattpilot-exporter" ]
