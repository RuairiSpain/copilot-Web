# FND-SEC-006

## SYNOPSIS

Firewall rules reject private/open ranges

## DESCRIPTION

IP firewall rules on Foundry-related resources must not contain open ranges (0.0.0.0/0, or 0.0.0.0 on Cosmos DB, which means all Azure data centers) or private-network ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 100.64.0.0/10), which Storage and Cosmos DB document as not enforceable in IP rules.

## RECOMMENDATION

Remove open or private ranges from IP rules. Use private endpoints, virtual network rules or service endpoints for private traffic, and list only the specific public egress addresses that need access.

## EXAMPLES

```
networkAcls: {
  defaultAction: 'Deny'
  ipRules: [
    { value: '203.0.113.10' } // specific public address, not 0.0.0.0/0 or 10.0.0.0/8
  ]
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: ipRules values on Storage, Cosmos DB, Key Vault, Search and Cognitive Services accounts equal to 0.0.0.0/0 (and 0.0.0.0 on Cosmos DB), and RFC 1918 address prefixes on Storage and Cosmos DB (plus 100.64.0.0/10 on Cosmos DB). Private ranges are matched by address prefix, not by CIDR containment.

## LINKS

- [https://learn.microsoft.com/azure/storage/common/storage-network-security-limitations](https://learn.microsoft.com/azure/storage/common/storage-network-security-limitations) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
