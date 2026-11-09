# Copilot Instructions

## Build, Test & Run

All commands run from the repository root:

```bash
go run ./cmd/server          # start the server on :8080
make run                     # delete cached data/data.json, then run
make run-cached              # run using cached data/data.json
make build                   # compile binary
go test ./...                # run all tests
go test ./internal/wattpilot # run tests for a single package
go test ./internal/wattpilot -run TestCalculatePrice  # run a single test
```

A `WATTPILOT_KEY` environment variable is required. Place it in `.env` at the repo root (see `.env.example`).

## Architecture

Go web application that fetches EV charging session data from the Fronius Wattpilot API, calculates monthly costs using official Austrian electricity rates, and serves an HTML dashboard.

**Data flow:** Wattpilot API → `data/data.json` (local) or Azure Blob → parsed in-memory → filtered by month → rendered to HTML templates or Excel.

- Uses Go's `net/http` standard library with `html/template` — no web framework.
- **`internal/wattpilot/wattpilot.go`** — Core data layer: API client, JSON caching/refresh, backup fallback, date parsing, price calculation.
- **`internal/wattpilot/storage.go`** — `DataStore` interface with two backends: `LocalStore` (filesystem) and `AzureBlobStore`. Selected at startup via `InitStore()` based on `AZURE_STORAGE_ACCOUNT_NAME`.
- **`internal/handlers/`** — HTTP handlers for dashboard (`/`), charts (`/charts`), Excel download (`/download`), settings (`/settings`, `/settings/fetch-price`), info (`/info`), and refresh (`/refresh`).
- **`cmd/server/telemetry.go`** — OpenTelemetry trace + log provider initialisation. Logs use `slog` bridged to OTel.
- Static assets (Chart.js, Tailwind CSS) are served from `static/`. HTML templates live in `templates/`.

## Key Conventions

- **Date parsing** — Wattpilot API dates use European format, parsed with Go layout `"02.01.2006 15:04:05"`. Month navigation uses `"2006-01"` format throughout.
- **Electricity prices** — Yearly defaults live in `internal/settings/settings.go`; lookups use monthly overrides, then yearly overrides, then defaults. When adding an official yearly rate, update `Defaults().OfficialPrices`, the matching constant in `internal/wattpilot/wattpilot.go`, the info page, and the README. Loading settings merges missing official yearly defaults without replacing overrides. Purchase prices are independently configurable.
- **Data caching** — The app fetches from the API once and saves to `data/data.json`. Monthly snapshots are written to `data/data-YYYY-MM_backup.json`. Auto-refresh triggers when data is older than `DataTTLMinutes` (default 60 min, configurable via `/settings`). Hit `/refresh` to force re-fetch.
- **Refresh safety** — Fetched payloads are parsed/validated before overwriting `data.json`. On failure, existing cached data is preserved and monthly backups remain available. Writes use atomic temp-file + rename.
- **Historical data starts from June 2024** — `GetPrevMonth` enforces this lower bound.
- **Observability** — Handlers and core functions create OTel spans via package-level `tracer` variables. Use `slog.InfoContext`/`slog.ErrorContext` (not `log.Printf`) for structured logging.
- **Tests** — Use table-driven tests with `[]struct` patterns. Tests live alongside code in `_test.go` files within the same package (not a separate `_test` package). Example test files: `internal/wattpilot/wattpilot_test.go`, `internal/handlers/dashboard_test.go`.

## Azure Deployment

Deployed to Azure Container Apps via Azure Developer CLI (`azd`). See [AZD-SETUP.md](AZD-SETUP.md) for full setup. Key points:

- Infrastructure as Code: Bicep templates in `infra/`.
- `WATTPILOT_KEY` stored in Azure Key Vault; consumed via managed identity.
- `azd deploy` builds the Docker image, pushes to Docker Hub (`jetzlstorfer/wattpilot-exporter`), and updates the Container App.
- CI/CD: `.github/workflows/deploy-container-app.yml` — PR validation builds the image; push to `main` deploys via Azure OIDC (workload identity federation).

## Pull Request Guidelines

- **Always add screenshots** to every pull request that includes UI or visual changes. Place screenshot images in the `assets/` directory and embed them in the PR description.
