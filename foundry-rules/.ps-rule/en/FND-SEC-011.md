# FND-SEC-011

## SYNOPSIS

Controlled outbound egress informational

## DESCRIPTION

Informational report on the outbound network posture of a Foundry deployment: managed network isolationMode (Disabled | AllowInternetOutbound | AllowOnlyApprovedOutbound), account restrictOutboundNetworkAccess with allowedFqdnList, and azure.yaml network.isolationMode on the azure.ai.project service. It surfaces whether outbound egress is controlled; it never fails.

## RECOMMENDATION

If you need to control data exfiltration, set the managed network to AllowOnlyApprovedOutbound or restrict outbound access with an allowed FQDN list, and confirm required dependencies are reachable. Otherwise record the decision.

## EXAMPLES

```
# azure.yaml (azure.ai.project service)
network:
  peSubnet:
    vnet: ${VNET_RESOURCE_ID}
    name: private-endpoint-subnet
  isolationMode: AllowOnlyApprovedOutbound
```

## NOTES

- Severity dev/test/prod: info/info/info (PSRule: Information/Information/Information).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Informational. A fail result at level Information means properties.restrictOutboundNetworkAccess is not true on an AIServices account. The catalogue defines no fail state and notes the property is configuration evidence only. The azure.yaml network.isolationMode side is not evaluated.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
