// Optional addon: Azure Front Door (CDN + global edge + TLS) with a Web
// Application Firewall in front of the Container App from infra/bicep/.
// Deploy infra/bicep/main.bicep first, then this, pointing
// `originHostname` at that deployment's `containerAppFqdn` output.
//
// Standard tier (default) gets you the CDN/edge + WAF with your own
// custom rules (e.g. rate limiting). Premium adds Microsoft-managed WAF
// rule sets (OWASP) and bot protection, for a much higher base price —
// see infra/README.md's cost table before switching skuName to Premium.
targetScope = 'resourceGroup'

// No location param: Front Door profiles, endpoints, routes, and WAF
// policies are all global resources (declared with location: 'global'
// below) — there's nothing regional to parameterize here.

@description('Short, unique-ish prefix for resource names.')
@minLength(3)
@maxLength(12)
param namePrefix string

@description('Standard_AzureFrontDoor (~$35/mo base + usage, custom WAF rules only) or Premium_AzureFrontDoor (~$330/mo base, adds managed WAF rule sets + bot protection).')
@allowed(['Standard_AzureFrontDoor', 'Premium_AzureFrontDoor'])
param skuName string = 'Standard_AzureFrontDoor'

@description('The Container App\'s FQDN, e.g. from infra/bicep/main.bicep\'s containerAppFqdn output.')
param originHostname string

@description('Requests per client IP per minute before the WAF starts blocking. Applies on both tiers.')
param rateLimitThreshold int = 300

var profileName = '${namePrefix}-afd'
var endpointName = '${namePrefix}-endpoint'
var originGroupName = 'default-origin-group'
var originName = 'container-app-origin'
var routeName = 'default-route'
var wafPolicyName = replace('${namePrefix}wafpolicy', '-', '')
var securityPolicyName = '${namePrefix}-security-policy'

resource profile 'Microsoft.Cdn/profiles@2024-02-01' = {
  name: profileName
  location: 'global'
  sku: {
    name: skuName
  }
}

resource endpoint 'Microsoft.Cdn/profiles/afdEndpoints@2024-02-01' = {
  parent: profile
  name: endpointName
  location: 'global'
  properties: {
    enabledState: 'Enabled'
  }
}

resource originGroup 'Microsoft.Cdn/profiles/originGroups@2024-02-01' = {
  parent: profile
  name: originGroupName
  properties: {
    loadBalancingSettings: {
      sampleSize: 4
      successfulSamplesRequired: 3
    }
    healthProbeSettings: {
      probePath: '/manifest.webmanifest'
      probeRequestType: 'GET'
      probeProtocol: 'Https'
      probeIntervalInSeconds: 60
    }
  }
}

resource origin 'Microsoft.Cdn/profiles/originGroups/origins@2024-02-01' = {
  parent: originGroup
  name: originName
  properties: {
    hostName: originHostname
    originHostHeader: originHostname
    httpPort: 80
    httpsPort: 443
    priority: 1
    weight: 1000
    enabledState: 'Enabled'
  }
}

resource route 'Microsoft.Cdn/profiles/afdEndpoints/routes@2024-02-01' = {
  parent: endpoint
  name: routeName
  dependsOn: [origin]
  properties: {
    originGroup: {
      id: originGroup.id
    }
    supportedProtocols: ['Http', 'Https']
    patternsToMatch: ['/*']
    forwardingProtocol: 'HttpsOnly'
    linkToDefaultDomain: 'Enabled'
    httpsRedirect: 'Enabled'
    // The app is entirely dynamic (SSR pages, API routes, WebSocket) —
    // nothing here is safe to cache at the edge.
    cacheConfiguration: null
  }
}

resource wafPolicy 'Microsoft.Network/frontdoorWebApplicationFirewallPolicies@2024-02-01' = {
  name: wafPolicyName
  location: 'global'
  sku: {
    name: skuName
  }
  properties: {
    policySettings: {
      enabledState: 'Enabled'
      mode: 'Prevention'
    }
    customRules: {
      rules: [
        {
          name: 'RateLimitPerIP'
          priority: 1
          enabledState: 'Enabled'
          ruleType: 'RateLimitRule'
          rateLimitDurationInMinutes: 1
          rateLimitThreshold: rateLimitThreshold
          matchConditions: [
            {
              matchVariable: 'RequestUri'
              operator: 'Any'
              matchValue: []
            }
          ]
          action: 'Block'
        }
      ]
    }
    // Premium-only: Microsoft-managed OWASP rule set. Harmless to declare
    // on Standard too since it's simply ignored there, but called out
    // explicitly rather than silently no-op'd.
    managedRules: skuName == 'Premium_AzureFrontDoor' ? {
      managedRuleSets: [
        {
          ruleSetType: 'Microsoft_DefaultRuleSet'
          ruleSetVersion: '2.1'
          ruleSetAction: 'Block'
        }
      ]
    } : null
  }
}

resource securityPolicy 'Microsoft.Cdn/profiles/securityPolicies@2024-02-01' = {
  parent: profile
  name: securityPolicyName
  properties: {
    parameters: {
      type: 'WebApplicationFirewall'
      wafPolicy: {
        id: wafPolicy.id
      }
      associations: [
        {
          domains: [
            {
              id: endpoint.id
            }
          ]
          patternsToMatch: ['/*']
        }
      ]
    }
  }
}

output frontDoorEndpointHostname string = endpoint.properties.hostName
output wafPolicyId string = wafPolicy.id
