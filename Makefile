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
	@(sleep 2; [ -n "$$BROWSER" ] && "$$BROWSER" http://localhost:8080 >/dev/null 2>&1 || true) &
	$(GO) run $(RUN_FLAGS) ./cmd/server

run-cached: # don't delete data/data.json before running
	@pid=`command -v lsof >/dev/null 2>&1 && lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -n 1 || true`; \
	if [ -n "$$pid" ]; then \
		echo "Port 8080 already in use (PID $$pid). Stopping old process..."; \
		kill $$pid; \
		sleep 1; \
	fi
	@(sleep 2; [ -n "$$BROWSER" ] && "$$BROWSER" http://localhost:8080 >/dev/null 2>&1 || true) &
	$(GO) run $(RUN_FLAGS) ./cmd/server

sample-init: # initialize data/sample from existing local cache/backups (non-destructive)
	@mkdir -p data/sample
	@test -f data/data.json && cp -n data/data.json data/sample/data.json || true
	@cp -n data/data-*_backup.json data/sample/ 2>/dev/null || true
	@echo "Initialized data/sample (existing files were not overwritten)."

sample: # run with sample data from data/sample and no .env key
	@test -d data/sample || { echo "Missing data/sample directory. Run 'make sample-init' first."; exit 1; }
	@test -n "`ls -1 data/sample/*.json 2>/dev/null`" || { echo "No sample JSON files found in data/sample. Run 'make sample-init' first."; exit 1; }
	@pid=`command -v lsof >/dev/null 2>&1 && lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -n 1 || true`; \
	if [ -n "$$pid" ]; then \
		echo "Port 8080 already in use (PID $$pid). Stopping old process..."; \
		kill $$pid; \
		sleep 1; \
	fi
	@sample_dir=`mktemp -d /tmp/wattpilot-sample-XXXXXX`; \
	cp -f data/sample/*.json "$$sample_dir"/; \
	if [ ! -f "$$sample_dir/data.json" ]; then \
		latest_backup=`ls -1 "$$sample_dir"/data-*_backup.json 2>/dev/null | tail -n 1`; \
		if [ -n "$$latest_backup" ]; then \
			cp -f "$$latest_backup" "$$sample_dir/data.json"; \
		else \
			echo "data/sample must include data.json or at least one data-*_backup.json"; \
			rm -rf "$$sample_dir"; \
			exit 1; \
		fi; \
	fi; \
	default_month=`ls -1 "$$sample_dir"/data-*_backup.json 2>/dev/null | sed -E 's|.*/data-([0-9]{4}-[0-9]{2})_backup.json|\1|' | sort | tail -n 1`; \
	if [ -z "$$default_month" ]; then \
		default_month=`jq -r '[.data[].end] | map(split(" ")[0] | split(".") | .[2] + "-" + .[1]) | max // empty' "$$sample_dir/data.json" 2>/dev/null`; \
	fi; \
	open_url="http://localhost:8080"; \
	if [ -n "$$default_month" ]; then \
		open_url="http://localhost:8080/?date=$$default_month"; \
	fi; \
	(sleep 2; [ -n "$$BROWSER" ] && "$$BROWSER" "$$open_url" >/dev/null 2>&1 || true) & \
	trap 'status=$$?; rm -rf "$$sample_dir"; exit $$status' EXIT INT TERM; \
	WATTPILOT_DATA_DIR="$$sample_dir" WATTPILOT_DEFAULT_MONTH="$$default_month" WATTPILOT_SKIP_DOTENV=1 WATTPILOT_KEY="" $(GO) run $(RUN_FLAGS) ./cmd/server

# Clean the build artifacts
clean:
	rm -f $(EXECUTABLE)
	rm -f data/data.json

docker-build:
	docker build -t jetzlstorfer/wattpilot-exporter:local .

docker-run:
	docker run --env-file ./.env -p 8080:8080 jetzlstorfer/wattpilot-exporter:local
