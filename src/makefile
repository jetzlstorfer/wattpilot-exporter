# Define the name of the executable
EXECUTABLE := wattpilot-exporter

# Define the Go compiler
GO := go

# Define the build flags
BUILD_FLAGS := -v

# Define the run flags
RUN_FLAGS :=

# Define the default target
.DEFAULT_GOAL := build

# Build the application
build:
	$(GO) build $(BUILD_FLAGS) -o $(EXECUTABLE)

# Run the application
run:
	$(GO) run $(RUN_FLAGS) .

# Clean the build artifacts
clean:
	rm -f $(EXECUTABLE)

make docker-build:
	docker build -t jetzlstorfer/wattpilot-exporter:local .

make docker-run:
	docker run -p 8080:8080 jetzlstorfer/wattpilot-exporter:local
