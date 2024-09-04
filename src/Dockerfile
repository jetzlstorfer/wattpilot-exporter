# Use the official Golang image to create a build artifact.
FROM golang:1.23-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Build the Go app
RUN go build -o /wattpilot-export 

# Start a new stage from scratch
FROM alpine:3.17  

WORKDIR /

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /wattpilot-export .

EXPOSE 8080

# Command to run the executable
CMD [ "/wattpilot-export" ]