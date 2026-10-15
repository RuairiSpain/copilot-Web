package sec

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeRaiPolicy            = typeAccounts + "/raiPolicies"
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
		return unavailable()
	}
	refs, anyRef := foundryRefs(in)
	var a acc
	seen := false
	for _, r := range vaults {
		if anyRef && !inFoundryScope(r, refs, anyRef) {
			continue
		}
		seen = true
		if st := boolState(r.Properties, "enableSoftDelete"); st == isFalse {
			a.add(r, "enableSoftDelete is false")
		}
		if st := boolState(r.Properties, "enablePurgeProtection"); st != isTrue {
			if st == expr {
				a.unresolved = true
			} else {
				a.add(r, "enablePurgeProtection is not true")
			}
		}
		if st := boolState(r.Properties, "enableRbacAuthorization"); st != isTrue {
			if st == expr {
				a.unresolved = true
			} else {
				a.add(r, "enableRbacAuthorization is not true")
			}
		}
	}
	if !seen {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC007(in *sdk.Input) sdk.Result {
	var a acc
	seen := false
	declared := declaredRaiPolicies(in)
	for _, r := range ofType(in, typeAcctDeploy) {
		seen = true
		name, st := str(r.Properties, "raiPolicyName")
		present, isExpr := st != absent, st == expr
		switch {
		case !present || strings.TrimSpace(name) == "":
			continue
		case isExpr:
			a.unresolved = true
			continue
		case strings.EqualFold(name, "Microsoft.DefaultV2"), strings.EqualFold(name, "Microsoft.Default"):
			continue
		case strings.HasPrefix(name, "Microsoft."):
			a.unresolved = true
			continue
		case declared[strings.ToLower(strings.TrimSpace(name))]:
			continue
		default:
			a.unresolved = true
		}
	}
	if y, ok := in.AzureYAML.(yamlAny); ok {
		for _, svc := range y.ServiceNames() {
			host, _, ok := y.Lookup("services", svc, "host")
			if !ok || host != "azure.ai.agent" {
				continue
			}
			kind, _, _ := y.Lookup("services", svc, "kind")
			switch strings.ToLower(kind) {
			case "prompt", "voice", "prompt-voice":
				continue
			}
			seen = true
			raw, loc, ok := y.LookupAny("services", svc, "policies")
			if !ok {
				a.fs = append(a.fs, sdk.Finding{
					Resource: sdk.ResourceRef{Type: host, Name: svc},
					Location: loc,
					Evidence: "hosted agent has no policies entry of type rai_policy",
				})
				continue
			}
			entries, _ := raw.([]any)
			found := false
			for _, entry := range entries {
				m, _ := entry.(map[string]any)
				if !strings.EqualFold(fmt.Sprint(m["type"]), "rai_policy") {
					continue
				}
				found = true
				id := strings.TrimSpace(fmt.Sprint(m["raiPolicyName"]))
				switch {
				case id == "":
					a.fs = append(a.fs, sdk.Finding{Resource: sdk.ResourceRef{Type: host, Name: svc}, Location: loc, Evidence: "hosted agent rai_policy entry has no raiPolicyName"})
				case !strings.HasPrefix(strings.ToLower(id), "/subscriptions/"):
					a.fs = append(a.fs, sdk.Finding{Resource: sdk.ResourceRef{Type: host, Name: svc}, Location: loc, Evidence: "hosted agent raiPolicyName is not a full ARM resource ID"})
				case !declared[strings.ToLower(lastSegment(id))]:
					a.unresolved = true
				}
			}
			if !found {
				a.fs = append(a.fs, sdk.Finding{Resource: sdk.ResourceRef{Type: host, Name: svc}, Location: loc, Evidence: "hosted agent has no policies entry of type rai_policy"})
			}
		}
	}
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	if a.unresolved {
		return skip("uncertain: custom RAI policy exists only outside the scanned template or uses an unresolved name")
	}
	if !seen {
		return unavailable()
	}
	return sdk.Result{}
}

func evalSEC008(in *sdk.Input) sdk.Result {
	relevant := relevantDefenderPlans(in)
	if len(relevant) == 0 {
		return unavailable()
	}
	pricings := map[string]sdk.ARMResource{}
	for _, r := range ofType(in, typePricing) {
		pricings[strings.ToLower(lastSegment(r.Name))] = r
	}
	var a acc
	for _, name := range relevant {
		r, ok := pricings[strings.ToLower(name)]
		if !ok {
			a.unresolved = true
			continue
		}
		tier, _ := str(r.Properties, "pricingTier")
		if !strings.EqualFold(strings.TrimSpace(tier), "Standard") {
			a.add(r, fmt.Sprintf("Defender plan %s pricingTier is %q", name, tier))
		}
	}
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	if a.unresolved {
		return skip(sdk.SkipInputUnavailable + ": subscription Defender pricing state is not fully available in the template")
	}
	return sdk.Result{}
}

func evalSEC009(in *sdk.Input) sdk.Result {
	assignments := ofType(in, typePolicyAssignment)
	if len(assignments) == 0 {
		return skip(sdk.SkipInputUnavailable + ": policy assignments live outside the scanned template")
	}
	mapped := mappedSecurityPolicies()
	var a acc
	for _, r := range assignments {
		id, _ := str(r.Properties, "policyDefinitionId")
		if !mapped[strings.ToLower(lastSegment(id))] {
			continue
		}
		mode, _ := str(r.Properties, "enforcementMode")
		if strings.EqualFold(mode, "DoNotEnforce") {
			a.add(r, "mapped policy assignment uses enforcementMode DoNotEnforce")
		}
	}
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	return skip("uncertain: policy compliance state requires live Policy Reader access")
}

func evalSEC010(in *sdk.Input) sdk.Result {
	var fs []sdk.Finding
	for _, r := range foundryAccountsSec(in.ARM) {
		state := "platform-managed"
		if src, _ := str(r.Properties, "encryption", "keySource"); strings.EqualFold(src, "Microsoft.KeyVault") {
			if _, ok := get(r.Properties, "encryption", "keyVaultProperties"); ok {
				state = "cmk"
			}
		}
		fs = append(fs, sdk.Finding{Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name}, Location: r.Location, Evidence: "CMK posture: " + state})
	}
	refs, anyRef := foundryRefs(in)
	for _, typ := range []string{typeSearch, typeCosmos, typeStorage} {
		for _, r := range ofType(in, typ) {
			if anyRef && !inFoundryScope(r, refs, anyRef) {
				continue
			}
			state := "platform-managed"
			switch r.Type {
			case typeSearch:
				if v, _ := str(r.Properties, "encryptionWithCmk", "enforcement"); strings.EqualFold(v, "Enabled") {
					state = "cmk"
				}
			case typeCosmos:
				if v, _ := str(r.Properties, "keyVaultKeyUri"); strings.TrimSpace(v) != "" {
					state = "cmk"
				}
			case typeStorage:
				if v, _ := str(r.Properties, "encryption", "keySource"); strings.EqualFold(v, "Microsoft.Keyvault") {
					state = "cmk"
				}
			}
			fs = append(fs, sdk.Finding{Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name}, Location: r.Location, Evidence: "CMK posture: " + state})
		}
	}
	if len(fs) == 0 {
		return unavailable()
	}
	return sdk.Result{Findings: fs}
}

func evalSEC011(in *sdk.Input) sdk.Result {
	var fs []sdk.Finding
	if y, ok := in.AzureYAML.(yamlAny); ok {
		for _, svc := range y.ServiceNames() {
			host, _, ok := y.Lookup("services", svc, "host")
			if !ok || host != "azure.ai.project" {
				continue
			}
			raw, loc, ok := y.LookupAny("services", svc, "network", "isolationMode")
			if !ok {
				continue
			}
			mode := strings.TrimSpace(fmt.Sprint(raw))
			if mode != "AllowOnlyApprovedOutbound" {
				fs = append(fs, sdk.Finding{
					Resource: sdk.ResourceRef{Type: host, Name: svc},
					Location: loc,
					Evidence: "network.isolationMode is " + mode + "; outbound egress is not limited to approved destinations",
				})
			}
		}
	}
	for _, r := range foundryAccountsSec(in.ARM) {
		mode, _ := str(r.Properties, "managedNetworkSettings", "isolationMode")
		restrict := boolState(r.Properties, "restrictOutboundNetworkAccess") == isTrue
		if strings.EqualFold(mode, "AllowOnlyApprovedOutbound") || restrict {
			continue
		}
		fs = append(fs, sdk.Finding{
			Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
			Location: r.Location,
			Evidence: "outbound network posture does not show an approved-only egress configuration",
		})
	}
	if len(fs) == 0 {
		return unavailable()
	}
	return sdk.Result{Findings: fs}
}

func evalSEC012(in *sdk.Input) sdk.Result {
	llmAPIs := llmAPIResources(in)
	var a acc
	policies := append(ofType(in, typeAPIMServiceDiag), ofType(in, typeAPIMAPIDiag)...)
	for _, r := range policies {
		if hasLLMMessageLogging(r) {
			a.add(r, "APIM diagnostic enables raw LLM request or response message logging")
			continue
		}
		if strings.EqualFold(r.Type, typeAPIMAPIDiag) && diagnosticBodyLogging(r) {
			apiName := apiNameFromDiag(r.Name)
			if llmAPIs[strings.ToLower(apiName)] {
				a.add(r, "APIM diagnostic enables request or response body logging for an LLM API")
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
			return unavailable()
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
			a.add(r, finding)
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

func evalSEC014(in *sdk.Input) sdk.Result {
	outs, ok := in.ARM.(sdk.ARMOutputs)
	if !ok {
		return skip(sdk.SkipInputUnavailable + ": compiled ARM outputs are not available")
	}
	var fs []sdk.Finding
	for _, out := range outs.Outputs() {
		name := strings.ToLower(strings.TrimSpace(out.Name))
		if strings.Contains(name, "password") || strings.Contains(name, "secret") || strings.Contains(name, "key") || strings.Contains(name, "token") {
			fs = append(fs, sdk.Finding{
				Resource: sdk.ResourceRef{Type: "ARM/output", Name: out.Name},
				Evidence: "output name is secret-shaped and should not emit secret material",
			})
		}
	}
	if len(fs) > 0 {
		return sdk.Result{Findings: fs}
	}
	return skip(sdk.SkipInputUnavailable + ": Bicep output value diagnostics are not available in this build")
}

func declaredRaiPolicies(in *sdk.Input) map[string]bool {
	out := map[string]bool{}
	if in == nil || in.ARM == nil {
		return out
	}
	for _, r := range ofType(in, typeRaiPolicy) {
		out[strings.ToLower(lastSegment(r.Name))] = true
	}
	return out
}

func relevantDefenderPlans(in *sdk.Input) []string {
	if in == nil || in.ARM == nil {
		return nil
	}
	set := map[string]bool{}
	if len(foundryAccountsSec(in.ARM)) > 0 {
		set["AI"] = true
	}
	if len(ofType(in, typeStorage)) > 0 {
		set["StorageAccounts"] = true
	}
	if len(ofType(in, typeKeyVault)) > 0 {
		set["KeyVaults"] = true
	}
	if len(ofType(in, typeCosmos)) > 0 {
		set["CosmosDbs"] = true
	}
	var out []string
	for _, name := range []string{"AI", "StorageAccounts", "KeyVaults", "CosmosDbs"} {
		if set[name] {
			out = append(out, name)
		}
	}
	return out
}

func mappedSecurityPolicies() map[string]bool {
	return map[string]bool{
		"71ef260a-8f18-47b7-abcb-62d0673d94dc": true,
		"1e66c121-a66a-4b1f-9b83-0fd99bf0fc2d": true,
		"0b60c0b2-2dc2-4e1c-b5c9-abbed971de53": true,
		"12d4fa5e-1f9f-4c21-97a9-b99b3c6611b5": true,
		"67121cc7-ff39-4ab8-b7e3-95b84dab487d": true,
	}
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

func foundryAccountsSec(m sdk.ARMModel) []sdk.ARMResource {
	accounts := ofType(&sdk.Input{ARM: m}, typeAccounts)
	var out []sdk.ARMResource
	for _, r := range accounts {
		if ownsChild(&sdk.Input{ARM: m}, r, len(accounts), typeAcctProject, typeAcctDeploy) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return accounts
	}
	return out
}
