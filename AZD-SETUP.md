# Azure Developer CLI (azd) Setup Guide

This guide walks you through provisioning the wattpilot-exporter app on Azure using `azd`, including Azure Key Vault for the `WATTPILOT_KEY` secret.

## Prerequisites

### Windows

```bash
# Install Azure Developer CLI
winget install Microsoft.Azd

# Install Azure CLI
winget install Microsoft.AzureCLI
```

### macOS

```bash
# Install Azure Developer CLI
brew tap azure/cli && brew install azd

# Install Azure CLI
brew install azure-cli
```

### Linux (Ubuntu/Debian)

```bash
# Install Azure Developer CLI
curl -fsSL https://aka.ms/install-azd.sh | bash

# Install Azure CLI
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash
```

### Verify installations

```bash
azd version
az version
```

### Login to Azure

```bash
azd auth login
```

## Step 1: Initialize the azd environment

**Run from the repository root:**

```bash
cd /path/to/wattpilot-exporter
azd init -e wattpilot-prod
```

This creates a local `.azure/` directory and links the project to `azure.yaml` and the Bicep infrastructure in `infra/`.

## Step 2: Configure environment variables

```bash
# Set the Azure region (available options: eastus, westus, swedencentral, etc.)
azd env set AZURE_LOCATION swedencentral

# Set the Wattpilot API key (this will be stored in Azure Key Vault)
azd env set WATTPILOT_KEY <your-wattpilot-api-key>

# Set Docker Hub credentials (required to push the container image)
azd env set DOCKER_USERNAME <your-dockerhub-username>
azd env set DOCKER_PASSWORD <your-dockerhub-token>
```

> **Note on Docker credentials:** Use a Personal Access Token (PAT) instead of your password. Create one at https://hub.docker.com/settings/security.
>
> **Note on container image tags:** `azd deploy` automatically generates unique tags for each deployment (format: `<env-name>-<timestamp>`). You don't need to manually set `CONTAINER_IMAGE` — azd will build, tag, and push the image automatically.

## Step 3: Provision Azure resources

**Run from the repository root:**

```bash
cd /path/to/wattpilot-exporter
azd provision
```

This creates the following resources in Azure:
- **Resource Group** — `rg-<environment-name>`
- **Azure Key Vault** — securely stores `WATTPILOT_KEY` as a secret named `wattpilot-key`
- **Log Analytics Workspace** — collects container app logs and diagnostics
- **Container Apps Environment** — managed hosting environment
- **Container App** (`wattpilot`) — runs the Go application with:
  - System-assigned **Managed Identity** (for accessing Key Vault)
  - **Key Vault secret reference** for the `WATTPILOT_KEY` environment variable (no secrets in config)
  - External ingress on port 8080 (publicly accessible)
  - Resource limits: 0.5 vCPU, 1Gi memory (smallest paid tier)

Provisioning typically takes 2-3 minutes.

> **Important for first-time setup:** The initial provision uses a bootstrap container image. After provisioning completes, you must run `azd deploy` to build and deploy your actual application code.

## Step 4: Deploy the application

**Run from the repository root:**

```bash
cd /path/to/wattpilot-exporter
azd deploy
```

This:
1. Builds the Docker image using the `Dockerfile` in `src/`
2. Tags it with a unique identifier (format: `<env-name>-azd-deploy-<timestamp>`)
3. Pushes it to Docker Hub as `jetzlstorfer/wattpilot-export:<unique-tag>`
4. Updates the Container App to use the new image
5. Automatically restarts the container with the new image

Each deployment creates a new immutable image tag on Docker Hub, allowing easy rollback if needed.

Deployment typically takes 1-2 minutes.

## Recommended Workflow

**For first-time setup, run separately:**

```bash
azd provision  # Create Azure infrastructure (uses bootstrap image)
azd deploy     # Build, push, and deploy your application code
```

**For subsequent updates:**

```bash
azd deploy     # Just redeploy with new code changes
```

**Full provisioning (if infrastructure already exists):**

```bash
azd up  # Provision + deploy in one command
```

> **Note:** `azd up` on a fresh environment may fail if no container image exists yet. Use the separate `provision` → `deploy` workflow for first-time setup.

## Useful commands

```bash
# List all azd environments
azd env list

# Show all environment variables for current environment
azd env get-values

# View deployed app info (including FQDN)
azd show

# Get only the deployed app URL
azd env get-values | grep AZURE_CONTAINER_APP_FQDN

# Stream container app logs in real-time
az containerapp logs show -n wattpilot -g rg-wattpilot-prod --follow

# View recent logs (last 50 lines)
az containerapp logs show -n wattpilot -g rg-wattpilot-prod --tail 50

# Rotate the Wattpilot API key
az keyvault secret set --vault-name $(azd env get-values | grep AZURE_KEY_VAULT_NAME | cut -d= -f2 | tr -d '"') --name wattpilot-key --value <new-key>

# Restart the container app to pick up updated secrets
az containerapp revision restart -n wattpilot -g $(azd env get-values | grep AZURE_RESOURCE_GROUP | cut -d= -f2 | tr -d '"')

# Delete all Azure resources and local environment
azd down

# Delete environment without deleting resources
rm -rf .azure/wattpilot-prod
```

## How it works

```
┌─────────────────────────────────────────────────┐
│                  Azure                          │
│                                                 │
│  ┌──────────────┐     ┌──────────────────────┐  │
│  │  Key Vault   │────▶│  Container App       │  │
│  │              │     │  (wattpilot)         │  │
│  │  wattpilot-  │     │                      │  │
│  │  key         │     │  WATTPILOT_KEY       │  │
│  │  (secret)    │◀────│  = secretRef →       │  │
│  │              │ RBAC│    Key Vault ref      │  │
│  └──────────────┘     │                      │  │
│                       │  Managed Identity    │  │
│                       │  (system-assigned)   │  │
│                       └──────────────────────┘  │
│                                                 │
└─────────────────────────────────────────────────┘
```

The Container App uses a **system-assigned managed identity** with the **Key Vault Secrets User** RBAC role. The `WATTPILOT_KEY` environment variable is injected at runtime via a Key Vault secret reference — no secrets are stored in app configuration or source code.

## Architecture of provisioned resources

| Resource | Purpose | Location in Bicep |
|---|---|---|
| Resource Group | Groups all resources together | `infra/main.bicep` |
| Log Analytics Workspace | Collects container app logs and diagnostics | `infra/modules/log-analytics.bicep` |
| Azure Key Vault | Securely stores secrets (e.g., `WATTPILOT_KEY`) | `infra/modules/key-vault.bicep` |
| Container Apps Environment | Managed hosting environment for containers | `infra/modules/container-apps-env.bicep` |
| Container App (`wattpilot`) | Runs the Go web application on port 8080 | `infra/modules/container-app.bicep` |
| Managed Identity | System-assigned identity for the Container App | `infra/modules/container-app.bicep` |
| RBAC Role Assignment | Grants `Key Vault Secrets User` role to the Managed Identity | `infra/modules/key-vault-access.bicep` |

## Resource Cost

The deployment uses:
- **Container Apps**: 0.5 vCPU + 1Gi memory (smallest paid tier) — ~$10-15/month
- **Key Vault**: Standard tier — ~$0.60/month + access charges
- **Log Analytics**: Pay-per-GB ingestion — typically <$1/month for low-traffic app
- **Storage**: Minimal (Key Vault secrets only)

Total estimated cost: **~$15-20/month**

## Troubleshooting

### Container App won't start

Check logs:
```bash
az containerapp logs show -n wattpilot -g rg-wattpilot-prod --follow
```

Common issues:
- `WATTPILOT_KEY` not set in Key Vault — run `azd env set WATTPILOT_KEY <key>` and redeploy
- Application crashes on startup — check logs above

### Docker push fails with "denied"

Ensure Docker Hub credentials are set:
```bash
azd env set DOCKER_USERNAME <your-username>
azd env set DOCKER_PASSWORD <your-token>
```

Then retry:
```bash
azd deploy
```

### `azd provision` hangs or times out

This is usually just slow Azure deployment. Check progress in the Azure Portal link provided by azd.
