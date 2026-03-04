# Azure Developer CLI (azd) Setup Guide

This guide walks you through provisioning the wattpilot-exporter app on Azure using `azd`, including Azure Key Vault for the `WATTPILOT_KEY` secret.

## Prerequisites

```bash
# Install Azure Developer CLI (if not already installed)
winget install Microsoft.Azd

# Verify installation
azd version

# Install Azure CLI (needed for some operations)
winget install Microsoft.AzureCLI

# Login to Azure
azd auth login
```

## Step 1: Initialize the azd environment

```bash
# Initialize a new azd environment (choose a name like "wattpilot-prod")
azd init -e wattpilot-prod
```

This links the project to the `azure.yaml` and Bicep infrastructure already in this repo.

## Step 2: Configure environment variables

```bash
# Set the Azure region
azd env set AZURE_LOCATION westeurope

# Set the Wattpilot API key (this will be stored in Azure Key Vault)
azd env set WATTPILOT_KEY <your-wattpilot-api-key>

# Set Docker Hub credentials (needed to pull the container image)
azd env set DOCKER_USERNAME <your-dockerhub-username>
azd env set DOCKER_PASSWORD <your-dockerhub-password>

# Optionally set a specific container image (defaults to jetzlstorfer/wattpilot-export:latest)
azd env set CONTAINER_IMAGE jetzlstorfer/wattpilot-export:latest
```

## Step 3: Provision Azure resources

```bash
# Provision all infrastructure (Resource Group, Key Vault, Container Apps Environment, Container App)
azd provision
```

This creates:
- **Resource Group** — `rg-wattpilot-prod`
- **Azure Key Vault** — stores `WATTPILOT_KEY` as a secret called `wattpilot-key`
- **Log Analytics Workspace** — for container app logging
- **Container Apps Environment** — managed environment for the container app
- **Container App** (`wattpilot`) — with:
  - System-assigned **Managed Identity**
  - **Key Vault reference** for the `WATTPILOT_KEY` env var (no secrets in config!)
  - External ingress on port 8080

## Step 4: Deploy the application

```bash
# Deploy the container app
azd deploy
```

## Full provisioning in one command

If you prefer a single command that does both provision + deploy:

```bash
azd up
```

## Useful commands

```bash
# Check the status of your environment
azd env list

# Show current environment values
azd env get-values

# View deployed app URL
azd show

# Stream container app logs
az containerapp logs show -n wattpilot -g rg-wattpilot-prod --follow

# Update the Key Vault secret (e.g., rotate the key)
az keyvault secret set --vault-name <vault-name> --name wattpilot-key --value <new-key>

# Restart the container app to pick up the new secret
az containerapp revision restart -n wattpilot -g rg-wattpilot-prod --revision <revision-name>

# Tear down all resources
azd down
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

| Resource | Purpose |
|---|---|
| Resource Group | Groups all resources together |
| Log Analytics Workspace | Collects container app logs |
| Azure Key Vault | Securely stores `WATTPILOT_KEY` |
| Container Apps Environment | Managed hosting environment |
| Container App (`wattpilot`) | Runs the Go web application |
| Managed Identity | Allows Container App to read Key Vault secrets |
| RBAC Role Assignment | Grants `Key Vault Secrets User` role to the identity |
