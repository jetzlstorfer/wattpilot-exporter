param name string
param location string
param tags object = {}
param containerAppsEnvironmentId string
param containerImage string
param keyVaultName string
param keyVaultSecretUri string
param dockerUsername string = ''
@secure()
param dockerPassword string = ''

var hasDockerCredentials = !empty(dockerUsername) && !empty(dockerPassword)

resource containerApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  tags: union(tags, { 'azd-service-name': 'wattpilot' })
  identity: {
    type: 'SystemAssigned'
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
      }
      secrets: [
        {
          name: 'wattpilot-key'
          keyVaultUrl: keyVaultSecretUri
          identity: 'system'
        }
        ...(hasDockerCredentials ? [
          {
            name: 'docker-password'
            value: dockerPassword
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
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          env: [
            {
              name: 'WATTPILOT_KEY'
              secretRef: 'wattpilot-key'
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
