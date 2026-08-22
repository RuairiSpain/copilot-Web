@description('Location for the server.')
param location string

@description('Name of the Postgres flexible server. Must be globally unique.')
param name string

@description('Admin username.')
param adminUsername string = 'copilotadmin'

@description('Admin password.')
@secure()
param adminPassword string

@description('Database name the app connects to.')
param databaseName string = 'copilot_web'

// Burstable B1ms: 1 vCore, 2 GiB RAM — the cheapest managed Postgres SKU
// (~$12-13/mo compute in East US as of mid-2026; see infra/README.md's
// cost table). Fine for a single-user/low-traffic personal tool; bump to
// B2s or a General Purpose SKU if this starts serving real traffic.
resource server 'Microsoft.DBforPostgreSQL/flexibleServers@2024-08-01' = {
  name: name
  location: location
  sku: {
    name: 'Standard_B1ms'
    tier: 'Burstable'
  }
  properties: {
    version: '16'
    administratorLogin: adminUsername
    administratorLoginPassword: adminPassword
    storage: {
      storageSizeGB: 32
    }
    backup: {
      backupRetentionDays: 7
      geoRedundantBackup: 'Disabled'
    }
    // Public network access is the simplest option for a single small app
    // and avoids the extra cost/complexity of VNet integration (a
    // delegated subnet + private DNS zone). Locked down to Azure services
    // plus whichever IPs you explicitly allow below — see the
    // allowedIpRanges param and infra/README.md's network notes for the
    // private-networking alternative.
    network: {
      publicNetworkAccess: 'Enabled'
    }
    highAvailability: {
      mode: 'Disabled'
    }
  }
}

resource database 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: server
  name: databaseName
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
}

// Lets Container Apps / App Service (and any other Azure-hosted client)
// reach the server without enumerating individual outbound IPs, which
// Container Apps' Consumption plan doesn't expose stable ones for anyway.
resource allowAzureServices 'Microsoft.DBforPostgreSQL/flexibleServers/firewallRules@2024-08-01' = {
  parent: server
  name: 'AllowAllAzureServices'
  properties: {
    startIpAddress: '0.0.0.0'
    endIpAddress: '0.0.0.0'
  }
}

@description('Extra firewall rules, e.g. your own IP for running `prisma migrate deploy` locally. Each entry: {name, startIp, endIp}.')
param allowedIpRanges array = []

resource extraFirewallRules 'Microsoft.DBforPostgreSQL/flexibleServers/firewallRules@2024-08-01' = [
  for rule in allowedIpRanges: {
    parent: server
    name: rule.name
    properties: {
      startIpAddress: rule.startIp
      endIpAddress: rule.endIp
    }
  }
]

output serverFqdn string = server.properties.fullyQualifiedDomainName
output databaseName string = database.name
