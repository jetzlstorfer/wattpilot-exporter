targetScope = 'subscription'

@minLength(1)
@maxLength(64)
@description('Name of the environment (e.g., dev, prod)')
param environmentName string

@minLength(1)
@description('Primary location for all resources')
param location string

@secure()
@description('The Wattpilot API key to store in Key Vault')
param wattpilotKey string

@description('Container image to deploy (azd will inject dynamic tag during deploy)')
param containerImage string = 'jetzlstorfer/wattpilot-export:latest'

@description('Docker Hub username for pulling images')
param dockerUsername string = ''

@secure()
@description('Docker Hub password for pulling images')
param dockerPassword string = ''

var abbrs = loadJsonContent('abbreviations.json')
// Storage accounts don't allow hyphens; strip them from the environment name
var cleanEnvironmentName = replace(environmentName, '-', '')
var tags = { 'azd-env-name': environmentName }

// Resource Group
resource rg 'Microsoft.Resources/resourceGroups@2022-09-01' = {
  name: '${abbrs.resourceGroup}${environmentName}'
  location: location
  tags: tags
}

// Log Analytics Workspace
module logAnalytics 'modules/log-analytics.bicep' = {
  name: 'log-analytics'
  scope: rg
  params: {
    name: '${abbrs.logAnalyticsWorkspace}${environmentName}'
    location: location
    tags: tags
  }
}

// Key Vault
module keyVault 'modules/key-vault.bicep' = {
  name: 'key-vault'
  scope: rg
  params: {
    name: '${abbrs.keyVault}${environmentName}'
    location: location
    tags: tags
    secretName: 'wattpilot-key'
    secretValue: wattpilotKey
  }
}

// Container Apps Environment
module containerAppsEnv 'modules/container-apps-env.bicep' = {
  name: 'container-apps-env'
  scope: rg
  params: {
    name: '${abbrs.containerAppsEnvironment}${environmentName}'
    location: location
    tags: tags
    logAnalyticsWorkspaceId: logAnalytics.outputs.id
  }
}

// Container App
module containerApp 'modules/container-app.bicep' = {
  name: 'container-app'
  scope: rg
  params: {
    name: 'wattpilot'
    location: location
    tags: tags
    containerAppsEnvironmentId: containerAppsEnv.outputs.id
    containerImage: containerImage
    keyVaultSecretUri: keyVault.outputs.secretUri
    dockerUsername: dockerUsername
    dockerPassword: dockerPassword
    storageAccountName: storageAccount.outputs.name
  }
}

// Storage Account for persisting charging data
module storageAccount 'modules/storage-account.bicep' = {
  name: 'storage-account'
  scope: rg
  params: {
    name: '${abbrs.storageAccount}${cleanEnvironmentName}'
    location: location
    tags: tags
  }
}

// Grant the Container App's managed identity access to Key Vault
module keyVaultAccess 'modules/key-vault-access.bicep' = {
  name: 'key-vault-access'
  scope: rg
  params: {
    keyVaultName: keyVault.outputs.name
    principalId: containerApp.outputs.identityPrincipalId
  }
}

// Grant the Container App's managed identity access to Storage Account
module storageAccountAccess 'modules/storage-account-access.bicep' = {
  name: 'storage-account-access'
  scope: rg
  params: {
    storageAccountName: storageAccount.outputs.name
    principalId: containerApp.outputs.identityPrincipalId
  }
}

output AZURE_RESOURCE_GROUP string = rg.name
output AZURE_CONTAINER_APP_NAME string = containerApp.outputs.name
output AZURE_CONTAINER_APP_FQDN string = containerApp.outputs.fqdn
output AZURE_KEY_VAULT_NAME string = keyVault.outputs.name
output AZURE_STORAGE_ACCOUNT_NAME string = storageAccount.outputs.name
