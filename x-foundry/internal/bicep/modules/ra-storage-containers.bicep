// Storage Blob Data Owner for a project identity, limited by an ABAC condition to the containers
// the agent service creates for that project (<workspace id>-azureml-agent). It must be assigned
// after the project capability host exists.
param storageName string
param principalId string
@description('The project workspace id: 32 hexadecimal characters.')
param workspaceId string
@description('Shown on the role assignment in the portal.')
param assignmentDescription string = ''

var formattedWorkspaceId = '${substring(workspaceId, 0, 8)}-${substring(workspaceId, 8, 4)}-${substring(workspaceId, 12, 4)}-${substring(workspaceId, 16, 4)}-${substring(workspaceId, 20, 12)}'
var blobDataOwner = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'
var condition = '((!(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/tags/read\'}) AND !(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/filter/action\'}) AND !(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/tags/write\'})) OR (@Resource[Microsoft.Storage/storageAccounts/blobServices/containers:name] StringStartsWithIgnoreCase \'${formattedWorkspaceId}\' AND @Resource[Microsoft.Storage/storageAccounts/blobServices/containers:name] StringLikeIgnoreCase \'*-azureml-agent\'))'

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageName
}

resource roleDefinition 'Microsoft.Authorization/roleDefinitions@2022-04-01' existing = {
  scope: subscription()
  name: blobDataOwner
}

resource assignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, principalId, blobDataOwner, formattedWorkspaceId)
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    description: empty(assignmentDescription) ? null : assignmentDescription
    roleDefinitionId: roleDefinition.id
    conditionVersion: '2.0'
    condition: condition
  }
}
