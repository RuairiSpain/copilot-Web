@description('Location for the app.')
param location string

@description('Name of the Container App.')
param name string

@description('Resource ID of the Container Apps managed environment.')
param environmentId string

@description('Full container image reference, e.g. ghcr.io/OWNER/copilot-web:latest')
param containerImage string

@description('Container registry hostname.')
param registryServer string = 'ghcr.io'

@description('Container registry username (a GitHub username for ghcr.io).')
param registryUsername string

@description('Container registry password/PAT.')
@secure()
param registryPassword string

@description('Key Vault secret URIs (from the keyvault module) — pulled at runtime via this Container App\'s system-assigned identity, never stored as plain values here.')
param githubClientSecretUri string
param authSecretUri string
param tokenEncryptionKeyUri string
// A Key Vault URI, not the secret value itself — see the matching
// disable-comment on keyvault.bicep's output of the same name.
#disable-next-line secure-secrets-in-params
param postgresAdminPasswordUri string

@description('Non-secret app configuration.')
param githubClientId string
param authUrl string
param postgresServerFqdn string
param postgresDatabaseName string
param postgresAdminUsername string

@description('Keep at 1 so auto-mode sessions can actually keep running unattended — scaling to 0 kills their in-memory state. Bump only if you need more headroom for concurrent live sessions (MAX_LIVE_SESSIONS in the app).')
param minReplicas int = 1
param maxReplicas int = 1

@description('Smallest Consumption-plan allocation. Raise if the app OOMs under real usage.')
param cpu string = '0.25'
param memory string = '0.5Gi'

param targetPort int = 3000

// No DATABASE_URL here: Container Apps can't string-interpolate a
// secretRef into a larger composed value, so there's no way to build
// "postgresql://user:${secret}@host/db" directly in this definition.
// Discrete PGHOST/PGUSER/PGPASSWORD/PGDATABASE env vars go in instead
// (PGPASSWORD via secretRef, resolved to a real value at runtime), and
// src/server/bootstrap-env.ts assembles DATABASE_URL from them at
// container startup, before Prisma reads it.

resource containerApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    environmentId: environmentId
    configuration: {
      ingress: {
        external: true
        targetPort: targetPort
        transport: 'auto' // supports the WebSocket upgrade the app's /ws/sessions/:id route needs
        allowInsecure: false
      }
      registries: [
        {
          server: registryServer
          username: registryUsername
          passwordSecretRef: 'registry-password'
        }
      ]
      secrets: [
        {
          name: 'registry-password'
          value: registryPassword
        }
        {
          name: 'github-client-secret'
          keyVaultUrl: githubClientSecretUri
          identity: 'system'
        }
        {
          name: 'auth-secret'
          keyVaultUrl: authSecretUri
          identity: 'system'
        }
        {
          name: 'token-encryption-key'
          keyVaultUrl: tokenEncryptionKeyUri
          identity: 'system'
        }
        {
          name: 'postgres-admin-password'
          keyVaultUrl: postgresAdminPasswordUri
          identity: 'system'
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'copilot-web'
          image: containerImage
          resources: {
            cpu: json(cpu)
            memory: memory
          }
          env: [
            { name: 'PORT', value: string(targetPort) }
            { name: 'NODE_ENV', value: 'production' }
            { name: 'AUTH_URL', value: authUrl }
            { name: 'AUTH_TRUST_HOST', value: 'true' }
            { name: 'AUTH_GITHUB_ID', value: githubClientId }
            { name: 'AUTH_GITHUB_SECRET', secretRef: 'github-client-secret' }
            { name: 'AUTH_SECRET', secretRef: 'auth-secret' }
            { name: 'TOKEN_ENCRYPTION_KEY', secretRef: 'token-encryption-key' }
            { name: 'PGHOST', value: postgresServerFqdn }
            { name: 'PGPORT', value: '5432' }
            { name: 'PGUSER', value: postgresAdminUsername }
            { name: 'PGPASSWORD', secretRef: 'postgres-admin-password' }
            { name: 'PGDATABASE', value: postgresDatabaseName }
            { name: 'PGSSLMODE', value: 'require' }
          ]
          probes: [
            {
              type: 'Readiness'
              httpGet: {
                path: '/manifest.webmanifest'
                port: targetPort
              }
              initialDelaySeconds: 5
              periodSeconds: 10
            }
          ]
        }
      ]
      scale: {
        minReplicas: minReplicas
        maxReplicas: maxReplicas
      }
    }
  }
}

output fqdn string = containerApp.properties.configuration.ingress.fqdn
output principalId string = containerApp.identity.principalId
