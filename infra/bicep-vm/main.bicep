// The rock-bottom-cost alternative to infra/bicep/ (Container Apps +
// managed Postgres): one small VM running the app and Postgres together
// via Docker Compose, with Caddy for automatic free HTTPS. Cheaper, but
// you own OS patching, backups, and uptime — see infra/README.md's cost
// table and tradeoffs before picking this over the default.
//
// This template provisions the VM and networking only. It deliberately
// does NOT bake app secrets into VM customData (Azure doesn't fully
// protect customData at rest from anyone with Reader RBAC on the VM) —
// run infra/scripts/deploy-vm.sh after this deploys, which pushes the
// .env file and docker-compose.yml over `az vm run-command` instead.
targetScope = 'subscription'

@description('Azure region.')
param location string = 'eastus'

@description('Short, unique-ish prefix for resource names.')
@minLength(3)
@maxLength(12)
param namePrefix string

@description('VM admin username.')
param adminUsername string = 'copilotadmin'

@description('SSH public key (contents of e.g. ~/.ssh/id_ed25519.pub) — password auth is disabled.')
param sshPublicKey string

@description('CIDR allowed to reach SSH (port 22), e.g. "203.0.113.4/32" for just your own IP. Required — there is no safe default, so leave this unset and the deployment fails loudly rather than opening SSH to the internet.')
param allowedSshSourceCidr string

@description('B1s is the cheapest burstable size that runs this comfortably; B2s if you want headroom for more concurrent sessions.')
param vmSize string = 'Standard_B1s'

var resourceGroupName = '${namePrefix}-vm-rg'
var vnetName = '${namePrefix}-vnet'
var nsgName = '${namePrefix}-nsg'
var pipName = '${namePrefix}-pip'
var nicName = '${namePrefix}-nic'
var vmName = '${namePrefix}-vm'

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: resourceGroupName
  location: location
}

module network 'network.bicep' = {
  name: 'network'
  scope: rg
  params: {
    location: location
    vnetName: vnetName
    nsgName: nsgName
    pipName: pipName
    nicName: nicName
    allowedSshSourceCidr: allowedSshSourceCidr
  }
}

module vm 'vm.bicep' = {
  name: 'vm'
  scope: rg
  params: {
    location: location
    vmName: vmName
    vmSize: vmSize
    adminUsername: adminUsername
    sshPublicKey: sshPublicKey
    nicId: network.outputs.nicId
  }
}

output resourceGroupName string = rg.name
output publicIpAddress string = network.outputs.publicIpAddress
output sshCommand string = 'ssh ${adminUsername}@${network.outputs.publicIpAddress}'
