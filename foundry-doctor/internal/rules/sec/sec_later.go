package sec

import (
	"encoding/xml"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeRaiPolicy            = typeAccounts + "/raiPolicies"
	typeManagedNetwork       = typeAccounts + "/managedNetworks"
	typeRegistry             = "Microsoft.ContainerRegistry/registries"
	typeWebSite              = "Microsoft.Web/sites"
	typePricing              = "Microsoft.Security/pricings"
	typePolicyAssignment     = "Microsoft.Authorization/policyAssignments"
	typeAPIMService          = "Microsoft.ApiManagement/service"
	typeAPIMAPI              = "Microsoft.ApiManagement/service/apis"
	typeAPIMServiceDiag      = "Microsoft.ApiManagement/service/diagnostics"
	typeAPIMAPIDiag          = "Microsoft.ApiManagement/service/apis/diagnostics"
	typeAPIMServicePolicy    = "Microsoft.ApiManagement/service/policies"
	typeAPIMAPIPolicy        = "Microsoft.ApiManagement/service/apis/policies"
	typeAPIMOperationPolicy  = "Microsoft.ApiManagement/service/apis/operations/policies"
	typeAMPLS                = "Microsoft.Insights/privateLinkScopes"
	typeAMPLSScopedResource  = "Microsoft.Insights/privateLinkScopes/scopedResources"
	typeAppInsights          = "Microsoft.Insights/components"
	typeWorkspace            = "Microsoft.OperationalInsights/workspaces"
	typeAPIMPublicIP         = "Microsoft.Network/publicIPAddresses"
	typeNetworkSecurityGroup = "Microsoft.Network/networkSecurityGroups"
)

type yamlAny interface {
	sdk.AzureYAMLView
	LookupAny(path ...string) (any, sdk.Location, bool)
}

func evalSEC005(in *sdk.Input) sdk.Result {
	vaults := ofType(in, typeKeyVault)
	if len(vaults) == 0 {
		return notApplicable()
	}
	set := foundrylink.Collect(in.ARM)
	var a acc
	seen := false
	for _, r := range vaults {
		if !linkOf(&a, set, r) {
			continue
		}
		seen = true
		if st := boolState(r.Properties, "enableSoftDelete"); st == isFalse {
			a.addKey(r, "enableSoftDelete", "enableSoftDelete is false")
		}
		if st := boolState(r.Properties, "enablePurgeProtection"); st != isTrue {
			if st == expr {
				a.unresolved = true
			} else {
				a.addKey(r, "enablePurgeProtection", "enablePurgeProtection is not true")
			}
		}
		if st := boolState(r.Properties, "enableRbacAuthorization"); st != isTrue {
			if st == expr {
				a.unresolved = true
			} else {
				a.addKey(r, "enableRbacAuthorization", "enableRbacAuthorization is not true")
			}
		}
	}
	if !seen && len(a.fs) == 0 && !a.scopeUncertain && !a.conditional {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC012(in *sdk.Input) sdk.Result {
	llmAPIs := llmAPIResources(in)
	var a acc
	policies := append(ofType(in, typeAPIMServiceDiag), ofType(in, typeAPIMAPIDiag)...)
	for _, r := range policies {
		if hasLLMMessageLogging(r) {
			a.addKey(r, "messageLogging", "APIM diagnostic enables raw LLM request or response message logging")
			continue
		}
		if strings.EqualFold(r.Type, typeAPIMAPIDiag) && diagnosticBodyLogging(r) {
			apiName := apiNameFromDiag(r.Name)
			if llmAPIs[strings.ToLower(apiName)] {
				a.addKey(r, "bodyLogging", "APIM diagnostic enables request or response body logging for an LLM API")
			} else {
				a.unresolved = true
			}
		}
	}
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	if a.unresolved {
		return skip("uncertain: diagnostic body logging could not be tied to an LLM API")
	}
	return a.result()
}

func evalSEC013(in *sdk.Input) sdk.Result {
	llmAPIs := llmAPIResources(in)
	policies := append(append(ofType(in, typeAPIMServicePolicy), ofType(in, typeAPIMAPIPolicy)...), ofType(in, typeAPIMOperationPolicy)...)
	if len(policies) == 0 {
		if len(llmAPIs) == 0 {
			return notApplicable()
		}
		return skip("uncertain: APIM policy XML is not available in the scanned template")
	}
	var a acc
	seenLLMPolicy := map[string]bool{}
	for _, r := range policies {
		xmlText, _ := str(r.Properties, "value")
		if strings.Contains(xmlText, "{{") || strings.Contains(xmlText, "@(") {
			a.unresolved = true
			continue
		}
		scopeAPI := apiNameFromPolicy(r.Name)
		if scopeAPI != "" && !llmAPIs[strings.ToLower(scopeAPI)] {
			continue
		}
		if scopeAPI != "" {
			seenLLMPolicy[strings.ToLower(scopeAPI)] = true
		}
		finding := validateTenantPolicy(r, xmlText)
		if finding != "" {
			a.addKey(r, "tenantPolicy", finding)
		}
	}
	for apiName, ok := range llmAPIs {
		if ok && !seenLLMPolicy[apiName] {
			a.fs = append(a.fs, sdk.Finding{
				Resource: sdk.ResourceRef{Type: typeAPIMAPI, Name: apiName},
				Evidence: "LLM API has no validate-azure-ad-token or validate-jwt policy in scope",
			})
		}
	}
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	if a.unresolved {
		return skip("uncertain: APIM token-validation policy uses unresolved expressions")
	}
	return a.result()
}

func llmAPIResources(in *sdk.Input) map[string]bool {
	out := map[string]bool{}
	for _, r := range ofType(in, typeAPIMAPI) {
		if url, _ := str(r.Properties, "serviceUrl"); strings.Contains(strings.ToLower(url), "openai") || strings.Contains(strings.ToLower(url), "services.ai.azure.com") {
			out[strings.ToLower(lastSegment(r.Name))] = true
		}
	}
	return out
}

func hasLLMMessageLogging(r sdk.ARMResource) bool {
	for _, path := range [][]string{
		{"largeLanguageModel", "requests", "messages"},
		{"largeLanguageModel", "responses", "messages"},
	} {
		if v, st := str(r.Properties, path...); st == isTrue && strings.EqualFold(strings.TrimSpace(v), "all") {
			return true
		}
	}
	return false
}

func diagnosticBodyLogging(r sdk.ARMResource) bool {
	for _, path := range [][]string{
		{"frontend", "request", "body", "bytes"},
		{"frontend", "response", "body", "bytes"},
		{"backend", "request", "body", "bytes"},
		{"backend", "response", "body", "bytes"},
	} {
		if v, ok := get(r.Properties, path...); ok {
			switch n := v.(type) {
			case float64:
				if n > 0 {
					return true
				}
			case int:
				if n > 0 {
					return true
				}
			}
		}
	}
	return false
}

func apiNameFromDiag(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return ""
}

func apiNameFromPolicy(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 {
		for i := 0; i < len(parts)-1; i++ {
			if strings.EqualFold(parts[i], "apis") && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

type policyXML struct {
	XMLName  xml.Name       `xml:"policies"`
	Inbound  *policySection `xml:"inbound"`
	Backend  *policySection `xml:"backend"`
	Outbound *policySection `xml:"outbound"`
	OnError  *policySection `xml:"on-error"`
}

type policySection struct {
	AzureAD []*xmlValidateAzureAD `xml:"validate-azure-ad-token"`
	JWT     []*xmlValidateJWT     `xml:"validate-jwt"`
}

type xmlValidateAzureAD struct {
	TenantID string    `xml:"tenant-id,attr"`
	Clients  []xmlText `xml:"client-application-ids>application-id"`
	Audience []xmlText `xml:"audiences>audience"`
}

type xmlValidateJWT struct {
	OpenID []xmlText `xml:"openid-config"`
	Issuer []xmlText `xml:"issuers>issuer"`
}

type xmlText struct {
	Value string `xml:",chardata"`
}

func validateTenantPolicy(r sdk.ARMResource, xmlText string) string {
	var doc policyXML
	if err := xml.Unmarshal([]byte(xmlText), &doc); err != nil {
		return "policy XML could not be parsed deterministically"
	}
	sections := []*policySection{doc.Inbound, doc.Backend, doc.Outbound, doc.OnError}
	found := false
	for _, sec := range sections {
		if sec == nil {
			continue
		}
		for _, v := range sec.AzureAD {
			found = true
			tenant := strings.ToLower(strings.TrimSpace(v.TenantID))
			if tenant == "organizations" || tenant == "common" || strings.HasSuffix(tenant, "/organizations") || strings.HasSuffix(tenant, "/common") {
				return "validate-azure-ad-token uses a multi-tenant tenant-id"
			}
			if len(v.Clients) == 0 && len(v.Audience) == 0 {
				return "validate-azure-ad-token has neither client-application-ids nor audiences"
			}
		}
		for _, v := range sec.JWT {
			found = true
			for _, oidc := range v.OpenID {
				s := strings.ToLower(strings.TrimSpace(oidc.Value))
				if strings.Contains(s, "/organizations") || strings.Contains(s, "/common") {
					return "validate-jwt points at a multi-tenant OpenID configuration"
				}
			}
			for _, iss := range v.Issuer {
				s := strings.ToLower(strings.TrimSpace(iss.Value))
				if strings.Contains(s, "/organizations") || strings.Contains(s, "/common") {
					return "validate-jwt issuer list includes a multi-tenant endpoint"
				}
			}
		}
	}
	if !found {
		return "no validate-azure-ad-token or validate-jwt policy is present"
	}
	return ""
}

func lastSegment(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// foundryAccountsSec returns the deployed accounts that own projects or
// deployments, or all deployed accounts when none does. uncertain is true when
// a conditional account was left out.
func foundryAccountsSec(m sdk.ARMModel) (accounts []sdk.ARMResource, uncertain bool) {
	in := &sdk.Input{ARM: m}
	all := ofType(in, typeAccounts)
	var deployed, owning []sdk.ARMResource
	for _, r := range all {
		if presence(r) != armexpr.Deployed {
			uncertain = true
			continue
		}
		deployed = append(deployed, r)
		if owns(in, r, len(all), typeAcctProject, typeAcctDeploy) == ownsYes {
			owning = append(owning, r)
		}
	}
	if len(owning) > 0 {
		return owning, uncertain
	}
	return deployed, uncertain
}
