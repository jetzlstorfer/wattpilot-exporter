param name string
param location string
param tags object = {}
param containerAppsEnvironmentId string
param containerImage string
param keyVaultSecretUri string
param keyVaultIdentityResourceId string
param storageAccountName string
param storageEndpoint string = ''
param entraClientId string = ''
@secure()
param entraClientSecret string = ''
param dockerUsername string = ''
@secure()
param dockerPassword string = ''
param customDomainName string = ''
param managedCertificateId string = ''

var hasDockerCredentials = !empty(dockerUsername) && !empty(dockerPassword)
var hasEntraAuth = !empty(entraClientId) && !empty(entraClientSecret)
var hasCustomDomain = !empty(customDomainName) && !empty(managedCertificateId)

resource containerApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  tags: union(tags, { 'azd-service-name': 'wattpilot' })
  identity: {
    type: 'SystemAssigned,UserAssigned'
    userAssignedIdentities: {
      '${keyVaultIdentityResourceId}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerAppsEnvironmentId
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        allowInsecure: false
        customDomains: hasCustomDomain ? [
          {
            name: customDomainName
            certificateId: managedCertificateId
            bindingType: 'SniEnabled'
          }
        ] : []
      }
      secrets: [
        {
          name: 'wattpilot-key'
          keyVaultUrl: keyVaultSecretUri
          identity: keyVaultIdentityResourceId
        }
        ...(hasDockerCredentials ? [
          {
            name: 'docker-password'
            value: dockerPassword
          }
        ] : [])
        ...(hasEntraAuth ? [
          {
            name: 'microsoft-provider-authentication-secret'
            value: entraClientSecret
          }
        ] : [])
      ]
      registries: hasDockerCredentials ? [
        {
          server: 'docker.io'
          username: dockerUsername
          passwordSecretRef: 'docker-password'
        }
      ] : []
    }
    template: {
      containers: [
        {
          name: 'wattpilot'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: [
            {
              name: 'WATTPILOT_KEY'
              secretRef: 'wattpilot-key'
            }
            {
              name: 'AZURE_STORAGE_ACCOUNT_NAME'
              value: storageAccountName
            }
            {
              name: 'AZURE_STORAGE_ENDPOINT'
              value: storageEndpoint
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 1
      }
    }
  }
}

output name string = containerApp.name
output fqdn string = containerApp.properties.configuration.ingress.fqdn
output identityPrincipalId string = containerApp.identity.principalId

// Easy Auth with Microsoft Entra ID (only if client ID is provided)
resource authConfig 'Microsoft.App/containerApps/authConfigs@2024-03-01' = if (hasEntraAuth) {
  parent: containerApp
  name: 'current'
  properties: {
    platform: {
      enabled: true
    }
    globalValidation: {
      unauthenticatedClientAction: 'RedirectToLoginPage'
      redirectToProvider: 'azureactivedirectory'
    }
    login: {
      tokenStore: {
        enabled: false
      }
    }
    identityProviders: {
      azureActiveDirectory: {
        enabled: true
        registration: {
          clientId: entraClientId
          clientSecretSettingName: 'microsoft-provider-authentication-secret'
          openIdIssuer: '${environment().authentication.loginEndpoint}${subscription().tenantId}/v2.0'
        }
        login: {
          loginParameters: ['scope=openid profile email']
        }
        validation: {
          allowedAudiences: [
            entraClientId
            'api://${entraClientId}'
          ]
        }
      }
    }
  }
}
