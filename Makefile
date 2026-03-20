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
	$(GO) build $(BUILD_FLAGS) -o $(EXECUTABLE) ./cmd/server

# Run the application
run:
	rm -f data/data.json
	@pid=`command -v lsof >/dev/null 2>&1 && lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -n 1 || true`; \
	if [ -n "$$pid" ]; then \
		echo "Port 8080 already in use (PID $$pid). Stopping old process..."; \
		kill $$pid; \
		sleep 1; \
	fi
	$(GO) run $(RUN_FLAGS) ./cmd/server

run-cached: # don't delete data/data.json before running
	@pid=`command -v lsof >/dev/null 2>&1 && lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -n 1 || true`; \
	if [ -n "$$pid" ]; then \
		echo "Port 8080 already in use (PID $$pid). Stopping old process..."; \
		kill $$pid; \
		sleep 1; \
	fi
	$(GO) run $(RUN_FLAGS) ./cmd/server

# Clean the build artifacts
clean:
	rm -f $(EXECUTABLE)
	rm -f data/data.json

docker-build:
	docker build -t jetzlstorfer/wattpilot-exporter:local .

docker-run:
	docker run --env-file ./.env -p 8080:8080 jetzlstorfer/wattpilot-exporter:local
