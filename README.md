# Wattpilot Data Exporter ⚡

A lightweight Go web application that fetches EV charging session data from [Fronius Wattpilot](https://www.fronius.com/en/solar-energy/installers-partners/products-solutions/e-mobility/wattpilot) and calculates monthly charging costs based on the official Austrian government electricity rates ([BMF Sachbezug](https://www.bmf.gv.at/themen/steuern/arbeitnehmerveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html)).

## Features

- **Monthly dashboard** — view charging sessions, total energy (kWh), cost (€), and margin for any month
- **Historical charts** — visualize energy consumption and costs over time (data since June 2024)
- **Excel export** — download a per-month `.xlsx` billing report with detailed session data and cost summary
- **Live charging indicator** — detects whether a charging session is currently active
- **Data caching** — fetched data is cached locally as `data.json`; use the `/refresh` endpoint to re-fetch
- **Docker support** — multi-stage Docker build for minimal container images

## Prerequisites

- **Go 1.25+** (or Docker)
- A **Wattpilot API key** (`WATTPILOT_KEY`)

## Configuration

| Variable | Required | Description |
|---|---|---|
| `WATTPILOT_KEY` | Yes | Your Wattpilot data export key |

You can find the key on your Wattpilot export page — it is the `e=` query parameter in the URL:

```
https://data.wattpilot.io/export?e=THIS_IS_YOUR_KEY
```

Create a `.env` file inside the `src/` directory:

```bash
# src/.env
WATTPILOT_KEY=your_key_here
```

## Getting Started

### Run locally

```bash
cd src
go run .
```

Or use the Makefile:

```bash
cd src
make run        # fetches fresh data (deletes cached data.json first)
make run-cached # uses cached data.json if available
```

The application starts on **http://localhost:8080**.

### Run with Docker

```bash
cd src
make docker-build
make docker-run
```

> Make sure a `.env` file with your `WATTPILOT_KEY` exists in `src/` — it is passed to the container via `--env-file`.

### Deploy to Azure

This project is configured for deployment to **Azure Container Apps** using the **Azure Developer CLI (azd)**.

**Prerequisites:**
- [Azure Developer CLI (`azd`)](https://learn.microsoft.com/en-us/azure/developer/azure-developer-cli/)
- [Azure CLI (`az`)](https://learn.microsoft.com/en-us/cli/azure/)
- An Azure subscription
- Docker Hub account with push credentials

**Quick start:**

```bash
# Login to Azure
azd auth login

# Initialize environment (from repo root)
azd init -e wattpilot-prod

# Configure
azd env set AZURE_LOCATION swedencentral
azd env set WATTPILOT_KEY <your-wattpilot-api-key>
azd env set DOCKER_USERNAME <your-dockerhub-username>
azd env set DOCKER_PASSWORD <your-dockerhub-password>
azd env set CONTAINER_IMAGE jetzlstorfer/wattpilot-export:latest

# Provision infrastructure
azd provision

# Deploy application
azd deploy

# Get the deployed URL
azd env get-values | grep AZURE_CONTAINER_APP_FQDN
```

See [AZD-SETUP.md](AZD-SETUP.md) for detailed Azure deployment instructions.

## Routes

| Route | Description |
|---|---|
| `/` | Monthly dashboard (use `?date=YYYY-MM` to navigate) |
| `/charts` | Historical charts across all months |
| `/download` | Download monthly Excel report (`?date=YYYY-MM`) |
| `/info` | Info page |
| `/refresh` | Force re-fetch of data from the Wattpilot API |

## Official Electricity Rates

Charging costs are calculated using the yearly rates published by the Austrian Federal Ministry of Finance:

| Year | Rate (€/kWh) |
|---|---|
| 2024 | 0.33182 |
| 2025 | 0.35889 |
| 2026 | 0.32806 |

## Tech Stack

- [Go](https://go.dev/) — HTTP server & business logic
- [Chart.js](https://www.chartjs.org/) — client-side charting
- [Tailwind CSS](https://tailwindcss.com/) — styling
- [excelize](https://github.com/qax-os/excelize) — Excel file generation
- [godotenv](https://github.com/joho/godotenv) — `.env` file loading

## Project Structure

```
src/
├── main.go          # HTTP server, routes & main handler
├── chart.go         # /charts handler — historical month-over-month stats
├── download.go      # /download handler — Excel export
├── utils/
│   └── wattpilotutils.go  # API client, price calculation, data parsing
├── template.html    # Main dashboard template
├── charts.html      # Charts page template
├── info.html        # Info page template
├── static/          # Chart.js & Tailwind CSS assets
├── Dockerfile       # Multi-stage Alpine build
├── makefile         # Build, run & Docker targets
└── go.mod
```

## Makefile Targets

| Target | Description |
|---|---|
| `make build` | Compile the binary |
| `make run` | Delete cached data and run the app |
| `make run-cached` | Run the app using cached data |
| `make clean` | Remove binary and cached data |
| `make docker-build` | Build the Docker image |
| `make docker-run` | Run the Docker container |

## License

This project is provided as-is for personal use.

## Resources

- [BMF Sachbezug Kraftfahrzeug](https://www.bmf.gv.at/themen/steuern/arbeitnehmerinnenveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html) — official electricity price reference
- [Wattpilot Data Export](https://data.wattpilot.io/) — Fronius Wattpilot data API
