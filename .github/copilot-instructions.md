# Copilot Instructions

## Build & Run

All commands are run from the **repository root** (where `go.mod` lives):

```bash
go run ./cmd/server       # start the server on :8080
make run                  # delete cached data/data.json, then run
make run-cached           # run using cached data/data.json
make build                # compile binary
make docker-build         # build Docker image
make docker-run           # run Docker container (needs .env)
go test ./...             # run all tests
```

A `WATTPILOT_KEY` environment variable is required. Place it in `.env` at the repo root.

## Project Layout

```
wattpilot-exporter/
  go.mod                           # module root
  Makefile
  Dockerfile
  azure.yaml
  cmd/
    server/
      main.go                      # entrypoint — server wiring, signal handling
      telemetry.go                 # OpenTelemetry initialisation
  internal/
    wattpilot/
      wattpilot.go                 # core data layer: types, API client, caching, pricing
      wattpilot_test.go            # unit tests for pricing, dates, parsing
    handlers/
      dashboard.go                 # GET / — main dashboard handler + types
      charts.go                    # GET /charts — monthly aggregation handler
      download.go                  # GET /download — Excel report generation
  templates/                       # HTML templates (html/template)
    template.html
    charts.html
    info.html
  static/                          # client-side assets (CSS, JS, icons, PWA manifest)
  data/                            # runtime cache — gitignored
  infra/                           # Azure Bicep IaC
```

## Architecture

This is a Go web application that fetches EV charging session data from the Fronius Wattpilot API, calculates monthly costs using official Austrian electricity rates, and serves an HTML dashboard.

- **`cmd/server/main.go`** — HTTP server setup, route registration, graceful shutdown
- **`cmd/server/telemetry.go`** — OpenTelemetry trace + log provider initialisation
- **`internal/handlers/dashboard.go`** — `/` (dashboard) and `/refresh` handlers
- **`internal/handlers/charts.go`** — `/charts` handler aggregating month-over-month statistics
- **`internal/handlers/download.go`** — `/download` handler generating Excel (`.xlsx`) reports via excelize
- **`internal/wattpilot/wattpilot.go`** — Core data layer: API client, JSON caching, date parsing, price calculation

Data flows: Wattpilot API → `data/data.json` (local cache) → parsed in-memory → filtered by month → rendered to HTML templates or Excel.

The server uses Go's `net/http` standard library with `html/template` for rendering (no web framework). Static assets (Chart.js, Tailwind CSS) are served from `static/`.

## Key Conventions

- **Dates from the Wattpilot API** use European format and are parsed with Go layout `"02.01.2006 15:04:05"`. Month navigation uses `"2006-01"` format.
- **Electricity prices** are hardcoded per year as constants in `internal/wattpilot/wattpilot.go` (e.g., `OfficialPricePerKwh2025`). When a new year's rate is published, add a new constant and update the switch statements in `getSellingPriceOfYear`, `getPurchasePriceOfYear`, and `GetOfficialPricePerKwhForMonth`.
- **Data caching** — The app fetches from the API once and saves to `data/data.json`. Subsequent requests use the cache. Monthly snapshots are written to `data/*_backup.json`. Hit `/refresh` to re-fetch. The `make run` target deletes the cache before starting.
- **Historical data starts from June 2024** — `GetPrevMonth` enforces this lower bound.
- **`internal/` packages** are not importable by external code — this is intentional as this is an application, not a library.

## Azure Deployment

The application is deployed to **Azure Container Apps** using:

- **Infrastructure as Code**: Bicep templates in `infra/` (see `infra/main.bicep` and modules)
- **Azure Developer CLI**: Configuration in `azure.yaml` for automated provisioning and deployment
- **Secrets Management**: `WATTPILOT_KEY` stored securely in **Azure Key Vault**; the Container App uses a system-assigned managed identity to access it
- **Container Build & Push**: `azd deploy` builds the Docker image and pushes it to the Docker Hub repository `jetzlstorfer/wattpilot-export` using a timestamp-based image tag per deployment, then updates the Container App to use the new image

### Deployment workflow:

```bash
cd /path/to/repo
azd init -e <environment-name>        # Initialize environment
azd env set AZURE_LOCATION swedencentral
azd env set WATTPILOT_KEY <api-key>
azd env set DOCKER_USERNAME <username>
azd env set DOCKER_PASSWORD <token>
azd provision                         # Create Azure resources
azd deploy                            # Build image, push to Docker Hub, update Container App
```

See [AZD-SETUP.md](AZD-SETUP.md) for detailed instructions.

### Infrastructure:
- **Resource Group**: Groups all resources
- **Container Apps Environment**: Managed hosting environment  
- **Container App**: Runs the Go app (0.5 vCPU, 1Gi memory) on port 8080
- **Key Vault**: Stores `WATTPILOT_KEY` secret
- **Log Analytics Workspace**: Collects container logs
- **Managed Identity**: RBAC access to Key Vault
