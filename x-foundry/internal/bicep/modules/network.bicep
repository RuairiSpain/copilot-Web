// Virtual network with an optional delegated agent subnet and a private endpoint subnet.
param name string
param location string
param tags object = {}
param addressSpace string
param agentSubnetName string = 'agent-subnet'
@description('Empty when the setup is not the standard agent setup.')
param agentSubnetPrefix string = ''
param peSubnetName string = 'pe-subnet'
param peSubnetPrefix string

var agentSubnet = empty(agentSubnetPrefix) ? [] : [
  {
    name: agentSubnetName
    properties: {
      addressPrefix: agentSubnetPrefix
      delegations: [
        {
          name: 'Microsoft.App/environments'
          properties: {
            serviceName: 'Microsoft.App/environments'
          }
        }
      ]
    }
  }
]

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: name
  location: location
  tags: tags
  properties: {
    addressSpace: {
      addressPrefixes: [
        addressSpace
      ]
    }
    subnets: concat(agentSubnet, [
      {
        name: peSubnetName
        properties: {
          addressPrefix: peSubnetPrefix
        }
      }
    ])
  }
}

output id string = vnet.id
output name string = vnet.name
output agentSubnetId string = empty(agentSubnetPrefix) ? '' : '${vnet.id}/subnets/${agentSubnetName}'
output peSubnetId string = '${vnet.id}/subnets/${peSubnetName}'
