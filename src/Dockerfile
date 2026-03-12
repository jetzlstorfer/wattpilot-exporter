# Use the official Golang image to create a build artifact.
FROM golang:1.26-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Build the Go app
RUN go build -o /wattpilot-export 

# Ensure the data directory exists even when it is empty and not tracked in git
RUN mkdir -p /app/data

# Start a new stage from scratch
FROM alpine:3.23

WORKDIR /

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /wattpilot-export .
COPY --from=builder /app/template.html .
COPY --from=builder /app/charts.html .
COPY --from=builder /app/info.html .
COPY --from=builder /app/favicon.svg .
COPY --from=builder /app/favicon.ico .
COPY --from=builder /app/static ./static
COPY --from=builder /app/data ./data

# Update Alpine packages to ensure latest security patches
RUN apk update && apk upgrade --no-cache

EXPOSE 8080

# Command to run the executable
CMD [ "/wattpilot-export" ]