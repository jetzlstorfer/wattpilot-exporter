# Copilot Instructions

## Build & Run

All commands must be run from the `src/` directory:

```bash
cd src
go run .                  # start the server on :8080
make run                  # delete cached data/data.json, then run
make run-cached           # run using cached data/data.json
make build                # compile binary
make docker-build         # build Docker image
make docker-run           # run Docker container (needs src/.env)
```

A `WATTPILOT_KEY` environment variable is required. Place it in `src/.env`.

There are no tests in this project yet.

## Architecture

This is a Go web application that fetches EV charging session data from the Fronius Wattpilot API, calculates monthly costs using official Austrian electricity rates, and serves an HTML dashboard.

- **`src/main.go`** — HTTP server setup, route handlers for `/` (dashboard) and `/refresh`
- **`src/chart.go`** — `/charts` handler aggregating month-over-month statistics
- **`src/download.go`** — `/download` handler generating Excel (`.xlsx`) reports via excelize
- **`src/utils/wattpilotutils.go`** — Core data layer: API client, JSON caching, date parsing, price calculation

Data flows: Wattpilot API → `data/data.json` (local cache) → parsed in-memory → filtered by month → rendered to HTML templates or Excel.

The server uses Go's `net/http` standard library with `html/template` for rendering (no web framework). Static assets (Chart.js, Tailwind CSS) are served from `src/static/`.

## Key Conventions

- **Dates from the Wattpilot API** use European format and are parsed with Go layout `"02.01.2006 15:04:05"`. Month navigation uses `"2006-01"` format.
- **Electricity prices** are hardcoded per year as constants in `wattpilotutils.go` (e.g., `OfficialPricePerKwh2025`). When a new year's rate is published, add a new constant and update the switch statements in `getSellingPriceOfYear`, `getPurchasePriceOfYear`, and `GetOfficialPricePerKwhForMonth`.
- **Data caching** — The app fetches from the API once and saves to `data/data.json`. Subsequent requests use the cache. Monthly snapshots are written to `data/*_backup.json`. Hit `/refresh` to re-fetch. The `make run` target deletes the cache before starting.
- **Historical data starts from June 2024** — `GetPrevMonth` enforces this lower bound.

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
