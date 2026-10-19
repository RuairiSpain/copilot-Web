package sec

import (
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Policy keys read by the SEC rules. Neither key is part of ADR-007 yet: the
// configuration loader, the JSON schema and docs/configuration.md must add them
// before an organisation can set them. Until then the rules use their defaults
// (the catalogue list for SEC-009, no declared CMK requirement for SEC-010).
const (
	// PolicyKeyMappedDefinitions (list of policy definition GUIDs) replaces the
	// built-in policy definition list FND-SEC-009 looks for.
	PolicyKeyMappedDefinitions = "azurePolicy.mappedDefinitionIds"
	// PolicyKeyRequireCMK (boolean) declares that the organisation requires
	// customer-managed keys, which makes platform-managed keys a FND-SEC-010
	// finding.
	PolicyKeyRequireCMK = "encryption.requireCustomerManagedKeys"
)

// azureYAMLPoliciesMinAzd is the first azd version whose azure.yaml schema has
// the hosted-agent policies entry (catalogue FND-SEC-007 notes).
const azureYAMLPoliciesMinAzd = ">=1.36.0-beta.1"

// schemaGate says whether the azure.yaml hosted-agent policies field exists for
// the azd version in use.
type schemaGate int

const (
	gateUnknown schemaGate = iota // azd version unknown or not a valid version
	gateOld                       // azd older than the first schema with the field
	gateNew                       // the field exists in the schema
)

func policiesFieldGate(azd string) schemaGate {
	azd = strings.TrimSpace(azd)
	if azd == "" {
		return gateUnknown
	}
	var min catalog.VersionRange
	if err := yaml.Unmarshal([]byte("'"+azureYAMLPoliciesMinAzd+"'"), &min); err != nil {
		return gateUnknown
	}
	if min.Contains(azd) {
		return gateNew
	}
	// A well-formed version outside the range is older than the first release
	// that has the field (the range has no upper bound).
	if looksLikeVersion(azd) {
		return gateOld
	}
	return gateUnknown
}

func looksLikeVersion(v string) bool {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) < 3 {
		return false
	}
	for _, p := range parts[:2] {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// accountParent reduces a child resource name (acct/child[/...]) to the parent
// account reference.
func accountParent(r sdk.ARMResource) (armexpr.Ref, bool) {
	ref := armexpr.ParseName(r.Type, r.Name)
	if ref.Opaque || len(ref.Segments) < 2 {
		return armexpr.Ref{}, false
	}
	return armexpr.Ref{Type: strings.ToLower(typeAccounts), Segments: ref.Segments[:1]}, true
}

type lookup int

const (
	lookNone lookup = iota
	lookYes
	lookMaybe
)

// accountInTemplate reports whether the account named by parent is a deployed
// resource of the template: lookYes only when one account provably matches.
func accountInTemplate(in *sdk.Input, parent armexpr.Ref) lookup {
	res := lookNone
	for _, acct := range ofType(in, typeAccounts) {
		switch armexpr.Compare(parent, armexpr.ParseName(acct.Type, acct.Name)) {
		case armexpr.Same:
			if presence(acct) == armexpr.Deployed {
				return lookYes
			}
			res = lookMaybe
		case armexpr.Unknown:
			res = lookMaybe
		}
	}
	return res
}

// ---- FND-SEC-007 -----------------------------------------------------------

func evalSEC007(in *sdk.Input) sdk.Result {
	var a acc
	seen := false
	policies := ofType(in, typeRaiPolicy)
	for _, p := range policies {
		seen = true
		if a.maybe(p) {
			continue
		}
		allOff, unknown := contentFiltersAllDisabled(p)
		switch {
		case unknown:
			a.unresolved = true
			a.note(p, "contentFilters enabled flag is an unresolved expression or absent")
		case allOff:
			a.addKey(p, "contentFilters", "RAI policy declares content filters but every content filter has enabled=false, so the policy filters nothing")
		}
	}
	for _, r := range ofType(in, typeAcctDeploy) {
		seen = true
		if a.maybe(r) {
			continue
		}
		name, st := str(r.Properties, "raiPolicyName")
		trimmed := strings.TrimSpace(name)
		switch {
		case st == expr:
			a.unresolved = true
			a.note(r, "raiPolicyName is an unresolved expression")
		case st == other:
			a.unresolved = true
			a.note(r, "raiPolicyName is not a string")
		case st == absent || trimmed == "":
			a.addKey(r, "raiPolicyName", "deployment has no properties.raiPolicyName; the platform assigns Microsoft.DefaultV2 by default but the guardrail binding is not explicit in the template")
			a.fs[len(a.fs)-1].Severity = sdk.SeverityInfo
		case strings.EqualFold(trimmed, "Microsoft.DefaultV2"):
		case strings.HasPrefix(strings.ToLower(trimmed), "microsoft."):
			// Only Microsoft.DefaultV2 is verified as a built-in name.
			a.unresolved = true
			a.note(r, "raiPolicyName "+trimmed+" is a Microsoft-managed name that is not verified")
		default:
			classifyLocalPolicy(in, &a, r, trimmed, policies)
		}
	}
	armSeen := seen
	gated := gateUnknown
	yamlSeen := false
	if y, ok := in.AzureYAML.(yamlAny); ok {
		gated = policiesFieldGate(in.AzdVersion)
		yamlSeen = sec007AzureYAML(in, y, &a, policies, gated)
	}
	switch {
	case len(a.fs) > 0:
		return a.resultDetailed()
	case a.scopeUncertain || a.conditional || a.unresolved:
		return a.resultDetailed()
	case !armSeen && !yamlSeen:
		switch gated {
		case gateOld:
			if hostedAgentCount(in) > 0 {
				return skip(sdk.SkipNotApplicable + ": azure.yaml hosted-agent policies field does not exist for azd " + in.AzdVersion + " (first present in 1.36.0-beta.1)")
			}
		case gateUnknown:
			if hostedAgentCount(in) > 0 {
				return skip("uncertain: azd version is unknown, so the azure.yaml hosted-agent policies field cannot be required")
			}
		}
		return notApplicable()
	}
	return a.result()
}

// classifyLocalPolicy judges a raiPolicyName that is not a Microsoft built-in:
// it must be declared as a raiPolicies resource of the same account.
func classifyLocalPolicy(in *sdk.Input, a *acc, r sdk.ARMResource, name string, policies []sdk.ARMResource) {
	parent, ok := accountParent(r)
	if !ok {
		a.unresolved = true
		a.note(r, "the parent account of the deployment cannot be determined")
		return
	}
	want := armexpr.Ref{
		Type:     strings.ToLower(typeRaiPolicy),
		Segments: []armexpr.Segment{parent.Segments[0], {Literal: true, Text: strings.ToLower(name)}},
	}
	maybe := false
	for _, p := range policies {
		switch armexpr.Compare(want, armexpr.ParseName(p.Type, p.Name)) {
		case armexpr.Same:
			return
		case armexpr.Unknown:
			maybe = true
		}
	}
	if maybe {
		a.unresolved = true
		a.note(r, "raiPolicyName "+name+" may match a raiPolicies resource whose name is an expression")
		return
	}
	switch accountInTemplate(in, parent) {
	case lookYes:
		a.addKey(r, "raiPolicyName", fmt.Sprintf("raiPolicyName %q is not declared as a raiPolicies resource of the account in the template, so the deployment would reference a policy that does not exist", name))
	default:
		a.unresolved = true
		a.note(r, "raiPolicyName "+name+" names a policy that may exist in an account outside the scanned template")
	}
}

// contentFiltersAllDisabled reports allOff when the policy lists content
// filters and every one has the literal enabled=false (RaiPolicyContentFilter
// in the CognitiveServices 2026-09-01 spec). unknown is true when an enabled
// flag is an expression or missing. A policy without contentFilters is not
// judged (it may inherit from basePolicyName).
func contentFiltersAllDisabled(p sdk.ARMResource) (allOff, unknown bool) {
	v, ok := get(p.Properties, "contentFilters")
	if !ok || v == nil {
		return false, false
	}
	if unresolved(v) {
		return false, true
	}
	list, _ := v.([]any)
	if len(list) == 0 {
		return false, false
	}
	anyOn := false
	for _, e := range list {
		m, _ := e.(map[string]any)
		switch boolState(m, "enabled") {
		case isTrue:
			anyOn = true
		case isFalse:
		default:
			unknown = true
		}
	}
	return !anyOn && !unknown, unknown && !anyOn
}

func hostedAgentCount(in *sdk.Input) int {
	y, ok := in.AzureYAML.(yamlAny)
	if !ok {
		return 0
	}
	n := 0
	for _, svc := range y.ServiceNames() {
		if host, _, ok := y.Lookup("services", svc, "host"); ok && host == "azure.ai.agent" {
			switch kind, _, _ := y.Lookup("services", svc, "kind"); strings.ToLower(kind) {
			case "prompt", "voice", "prompt-voice":
			default:
				n++
			}
		}
	}
	return n
}

// sec007AzureYAML evaluates the hosted-agent leg. The policies field exists only
// in newer azure.yaml schemas, so a missing field is a finding only when the azd
// version in use has it; a present field is always evaluated. It reports
// whether any hosted agent was evaluated.
func sec007AzureYAML(in *sdk.Input, y yamlAny, a *acc, policies []sdk.ARMResource, gate schemaGate) bool {
	declared := map[string]bool{}
	for _, p := range policies {
		declared[strings.ToLower(lastSegment(p.Name))] = true
	}
	evaluated := false
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
		ref := sdk.ResourceRef{Type: host, Name: svc}
		raw, loc, present := y.LookupAny("services", svc, "policies")
		if !present {
			switch gate {
			case gateNew:
				evaluated = true
				a.fs = append(a.fs, sdk.Finding{Resource: ref, Location: loc, Evidence: "hosted agent has no policies entry of type rai_policy"})
			case gateOld:
				a.infos = append(a.infos, sdk.SkipNotApplicable+": "+svc+": azure.yaml policies field does not exist for azd "+in.AzdVersion+" (first present in 1.36.0-beta.1)")
			default:
				a.infos = append(a.infos, "uncertain: "+svc+": azd version is unknown, so the azure.yaml policies field cannot be required")
			}
			continue
		}
		evaluated = true
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
				a.fs = append(a.fs, sdk.Finding{Resource: ref, Location: loc, Evidence: "hosted agent rai_policy entry has no raiPolicyName"})
			case !strings.HasPrefix(strings.ToLower(id), "/subscriptions/"):
				a.fs = append(a.fs, sdk.Finding{Resource: ref, Location: loc, Evidence: "hosted agent raiPolicyName is not a full ARM resource ID"})
			case !declared[strings.ToLower(lastSegment(id))]:
				a.unresolved = true
				a.notes = append(a.notes, "hosted agent "+svc+": raiPolicyName names a policy that may exist only outside the scanned template")
			}
		}
		if !found {
			a.fs = append(a.fs, sdk.Finding{Resource: ref, Location: loc, Evidence: "hosted agent has no policies entry of type rai_policy"})
		}
	}
	return evaluated
}

// ---- FND-SEC-008 -----------------------------------------------------------

// defenderCapability names what is missing when pricings are not in the template.
const defenderCapability = "subscription-scope Microsoft.Security/pricings read (live Defender for Cloud check)"

const defenderMissingReason = sdk.SkipInputUnavailable + ": subscription-scope Defender state not in template"

func evalSEC008(in *sdk.Input) sdk.Result {
	relevant := relevantDefenderPlans(in)
	if len(relevant) == 0 {
		return notApplicable()
	}
	pricings := map[string]sdk.ARMResource{}
	for _, r := range ofType(in, typePricing) {
		pricings[strings.ToLower(lastSegment(r.Name))] = r
	}
	var a acc
	var missing []string
	for _, name := range relevant {
		r, ok := pricings[strings.ToLower(name)]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if a.maybe(r) {
			continue
		}
		tier, st := str(r.Properties, "pricingTier")
		switch {
		case st == expr:
			a.unresolved = true
			a.note(r, "Defender plan "+name+" pricingTier is an unresolved expression")
		case !strings.EqualFold(strings.TrimSpace(tier), "Standard"):
			a.addKey(r, "pricingTier", fmt.Sprintf("Defender plan %s pricingTier is %q", name, tier))
		}
	}
	if len(missing) > 0 {
		a.notes = append(a.notes, defenderMissingReason+": "+strings.Join(missing, ", "))
	}
	switch {
	case len(a.fs) > 0:
		return a.result()
	case len(missing) > 0:
		return sdk.Result{Skipped: &sdk.Skip{
			Reason:            defenderMissingReason + ": " + strings.Join(missing, ", "),
			MissingCapability: defenderCapability,
		}}
	}
	return a.result()
}

// relevantDefenderPlans derives the Defender plans that matter for the resource
// types in the template. The plan names AI, StorageAccounts, KeyVaults and
// CosmosDbs come from the Azure Policy and PSRule sources in the catalogue;
// Arm and AppServices are the pricingName values the PSRule rules
// Azure.Defender.Arm and Azure.Defender.AppServices test.
func relevantDefenderPlans(in *sdk.Input) []string {
	if in == nil || in.ARM == nil {
		return nil
	}
	set := map[string]bool{}
	if accounts, _ := foundryAccountsSec(in.ARM); len(accounts) > 0 {
		set["AI"] = true
		set["Arm"] = true
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
	if len(ofType(in, typeWebSite)) > 0 {
		set["AppServices"] = true
	}
	var out []string
	for _, name := range []string{"AI", "Arm", "StorageAccounts", "KeyVaults", "CosmosDbs", "AppServices"} {
		if set[name] {
			out = append(out, name)
		}
	}
	return out
}

// ---- FND-SEC-009 -----------------------------------------------------------

// mappedPolicyDefinitions are the Azure Policy built-in definition IDs behind
// the SEC rules. Every ID was read in the azure-policy repository
// (built-in-policies/policyDefinitions) and is listed with its display name in
// the FND-SEC-009 catalogue sources.
//
//	71ef260a  Azure AI Services resources should have key access disabled (SEC-001)
//	1e66c121  Key vaults should have soft delete enabled (SEC-005)
//	0b60c0b2  Key vaults should have deletion protection enabled (SEC-005)
//	12d4fa5e  Azure Key Vault should use RBAC permission model (SEC-005)
//	67121cc7  Azure AI Services resources should encrypt data at rest with a CMK (SEC-010)
var mappedPolicyDefinitions = []string{
	"71ef260a-8f18-47b7-abcb-62d0673d94dc",
	"1e66c121-a66a-4b1f-9b83-0fd99bf0fc2d",
	"0b60c0b2-2dc2-4e1c-b5c9-abbed971de53",
	"12d4fa5e-1f9f-4c21-97a9-b99b3c6611b5",
	"67121cc7-ff39-4ab8-b7e3-95b84dab487d",
}

func mappedSecurityPolicies() map[string]bool {
	out := map[string]bool{}
	for _, id := range mappedPolicyDefinitions {
		out[id] = true
	}
	return out
}

// policyIDList reads a list-valued policy key.
func policyIDList(in *sdk.Input, key string) []string {
	if in == nil || in.Policy == nil {
		return nil
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return nil
	}
	var out []string
	switch ids := v.(type) {
	case []string:
		out = append(out, ids...)
	case []any:
		for _, id := range ids {
			if s, ok := id.(string); ok {
				out = append(out, s)
			}
		}
	}
	var clean []string
	for _, id := range out {
		if s := strings.ToLower(strings.TrimSpace(id)); s != "" {
			clean = append(clean, s)
		}
	}
	return clean
}

// effectiveMappedPolicies is the policy-key override when set, else the
// catalogue list.
func effectiveMappedPolicies(in *sdk.Input) map[string]bool {
	if ids := policyIDList(in, PolicyKeyMappedDefinitions); len(ids) > 0 {
		out := map[string]bool{}
		for _, id := range ids {
			out[id] = true
		}
		return out
	}
	return mappedSecurityPolicies()
}

const policyMissingCapability = "Microsoft.Authorization/policyAssignments read (Policy Reader, live check)"

func evalSEC009(in *sdk.Input) sdk.Result {
	assignments := ofType(in, typePolicyAssignment)
	if len(assignments) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{
			Reason:            sdk.SkipInputUnavailable + ": policy assignments live outside the scanned template",
			MissingCapability: policyMissingCapability,
		}}
	}
	mapped := effectiveMappedPolicies(in)
	enforced := map[string]bool{}
	var a acc
	var uncertain []string
	for _, r := range assignments {
		if a.maybe(r) {
			uncertain = append(uncertain, armexpr.StripScope(r.Name)+": assignment is conditional")
			continue
		}
		id, st := str(r.Properties, "policyDefinitionId")
		switch {
		case st == expr:
			uncertain = append(uncertain, armexpr.StripScope(r.Name)+": policyDefinitionId is an unresolved expression")
			continue
		case st != isTrue:
			continue
		case strings.Contains(strings.ToLower(id), "/policysetdefinitions/"):
			uncertain = append(uncertain, armexpr.StripScope(r.Name)+": initiative membership cannot be expanded from the template")
			continue
		}
		guid := strings.ToLower(lastSegment(id))
		if !mapped[guid] {
			continue
		}
		mode, mst := str(r.Properties, "enforcementMode")
		switch {
		case mst == expr:
			uncertain = append(uncertain, armexpr.StripScope(r.Name)+": enforcementMode is an unresolved expression")
		case mst == absent || strings.EqualFold(mode, "Default"):
			enforced[guid] = true
		case strings.EqualFold(mode, "DoNotEnforce"):
			a.addKey(r, "enforcementMode:"+guid, "mapped policy assignment uses enforcementMode DoNotEnforce")
		default:
			uncertain = append(uncertain, armexpr.StripScope(r.Name)+": enforcementMode "+mode+" has an effect that is not verified")
		}
	}
	if len(a.fs) > 0 {
		return a.result()
	}
	var missing []string
	for _, g := range sortedKeys(mapped) {
		if !enforced[g] {
			missing = append(missing, g)
		}
	}
	if len(uncertain) > 0 || len(missing) > 0 {
		sort.Strings(uncertain)
		reason := "uncertain: policy coverage cannot be proven from the template"
		if len(uncertain) > 0 {
			reason += "; " + strings.Join(uncertain, "; ")
		}
		if len(missing) > 0 {
			reason += "; mapped definitions with no enforced assignment in the template (they may be assigned at a parent scope): " + strings.Join(missing, ", ")
		}
		return skip(reason)
	}
	return sdk.Result{}
}

// ---- FND-SEC-010 -----------------------------------------------------------

// cmkPolicyDefinitions are built-in Azure Policy definitions that audit or deny
// resources without a customer-managed key (azure-policy repository, listed in
// the FND-SEC-010 catalogue sources). An enforced assignment of one of them
// declares a CMK requirement.
var cmkPolicyDefinitions = map[string]bool{
	"67121cc7-ff39-4ab8-b7e3-95b84dab487d": true, // Cognitive Services CMK audit
	"76a56461-9dc0-40f0-82f5-2453283afa2f": true, // Search CMK audit
	"356da939-f20a-4bb9-86f8-5db445b0e354": true, // Search CMK deny
	"1f905d99-2ab7-462c-a6b0-f709acca6c8f": true, // Cosmos DB CMK
	"6fac406b-40ca-413b-bf8e-0bf964659c25": true, // Storage CMK audit
}

// cmkRequirement says whether the project declares a CMK requirement: the
// policy key, or an enforced assignment of a CMK built-in in the template.
func cmkRequirement(in *sdk.Input) (bool, string) {
	if in.Policy != nil {
		if v, ok := in.Policy.Get(PolicyKeyRequireCMK); ok {
			if b, isBool := v.(bool); isBool && b {
				return true, "policy " + PolicyKeyRequireCMK + " is true"
			}
		}
	}
	for _, r := range ofType(in, typePolicyAssignment) {
		if presence(r) != armexpr.Deployed {
			continue
		}
		id, st := str(r.Properties, "policyDefinitionId")
		if st != isTrue || !cmkPolicyDefinitions[strings.ToLower(lastSegment(id))] {
			continue
		}
		if mode, mst := str(r.Properties, "enforcementMode"); mst == absent || strings.EqualFold(mode, "Default") {
			return true, "the template assigns the customer-managed-key built-in policy " + lastSegment(id) + " in enforced mode"
		}
	}
	return false, ""
}

type cmkState int

const (
	cmkPlatform cmkState = iota
	cmkCustomer
	cmkUnknown
)

func evalSEC010(in *sdk.Input) sdk.Result {
	var a acc
	required, why := cmkRequirement(in)
	seen := false
	judge := func(r sdk.ARMResource, state cmkState, detail string) {
		seen = true
		switch state {
		case cmkCustomer:
		case cmkUnknown:
			a.unresolved = true
			a.note(r, detail)
		default:
			if required {
				a.add(r, "CMK posture: platform-managed (a customer-managed key is required because "+why+")")
				a.fs[len(a.fs)-1].Severity = sdk.SeverityInfo
			} else {
				a.info(r, sdk.SkipNotApplicable, "platform-managed keys; no customer-managed key requirement is declared")
			}
		}
	}
	accounts, uncertain := foundryAccountsSec(in.ARM)
	a.conditional = uncertain
	for _, r := range accounts {
		judge(r, accountCMK(r), "encryption.keySource is an unresolved expression or lacks keyVaultProperties")
	}
	set := foundrylink.Collect(in.ARM)
	for _, typ := range []string{typeSearch, typeCosmos, typeStorage} {
		for _, r := range ofType(in, typ) {
			if !linkOf(&a, set, r) {
				continue
			}
			judge(r, resourceCMK(r), "the customer-managed key setting is an unresolved expression")
		}
	}
	for _, r := range ofType(in, typeRegistry) {
		if a.maybe(r) {
			continue
		}
		judge(r, resourceCMK(r), "the customer-managed key setting is an unresolved expression")
	}
	if !seen && len(a.fs) == 0 {
		if a.scopeUncertain || a.conditional {
			return a.result()
		}
		return notApplicable()
	}
	return a.resultDetailed()
}

func accountCMK(r sdk.ARMResource) cmkState {
	if _, ok := get(r.Properties, "encryption"); !ok {
		return cmkPlatform
	}
	src, st := str(r.Properties, "encryption", "keySource")
	switch {
	case st == expr || st == other:
		return cmkUnknown
	case st == absent:
		// The spec default of keySource is Microsoft.KeyVault, so an encryption
		// block without keySource cannot be classified.
		return cmkUnknown
	case strings.EqualFold(src, "Microsoft.KeyVault"):
		if _, ok := get(r.Properties, "encryption", "keyVaultProperties"); ok {
			return cmkCustomer
		}
		return cmkUnknown
	}
	return cmkPlatform
}

// resourceCMK classifies Search, Cosmos DB, Storage and Container Registry
// resources by the property names in the catalogue sources.
func resourceCMK(r sdk.ARMResource) cmkState {
	switch strings.ToLower(r.Type) {
	case strings.ToLower(typeSearch):
		v, st := str(r.Properties, "encryptionWithCmk", "enforcement")
		switch {
		case st == expr:
			return cmkUnknown
		case strings.EqualFold(v, "Enabled"):
			return cmkCustomer
		}
	case strings.ToLower(typeCosmos):
		v, st := str(r.Properties, "keyVaultKeyUri")
		switch {
		case st == expr:
			return cmkUnknown
		case strings.TrimSpace(v) != "":
			return cmkCustomer
		}
	case strings.ToLower(typeStorage):
		v, st := str(r.Properties, "encryption", "keySource")
		switch {
		case st == expr:
			return cmkUnknown
		case strings.EqualFold(v, "Microsoft.Keyvault"):
			return cmkCustomer
		}
	case strings.ToLower(typeRegistry):
		status, st := str(r.Properties, "encryption", "status")
		key, kst := str(r.Properties, "encryption", "keyVaultProperties", "keyIdentifier")
		switch {
		case st == expr || kst == expr:
			return cmkUnknown
		case strings.EqualFold(status, "enabled") && strings.TrimSpace(key) != "":
			return cmkCustomer
		}
	}
	return cmkPlatform
}

// ---- FND-SEC-011 -----------------------------------------------------------

func evalSEC011(in *sdk.Input) sdk.Result {
	var a acc
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
				a.fs = append(a.fs, sdk.Finding{
					Resource: sdk.ResourceRef{Type: host, Name: svc},
					Location: loc,
					Evidence: "network.isolationMode is " + mode + "; outbound egress is not limited to approved destinations",
					Severity: sdk.SeverityInfo,
				})
			}
		}
	}
	accounts, uncertain := foundryAccountsSec(in.ARM)
	networks := ofType(in, typeManagedNetwork)
	for _, r := range accounts {
		egressOfAccount(in, &a, r, networks)
	}
	if len(a.fs) == 0 && len(accounts) == 0 && len(a.infos) == 0 {
		if uncertain {
			return skip(foundrylink.SkipConditional)
		}
		return notApplicable()
	}
	return a.resultDetailed()
}

// egressOfAccount reports at most one finding for an account. Property names
// come from the CognitiveServices 2026-09-01 spec: the managed network is the
// child resource accounts/managedNetworks (properties.managedNetwork.
// isolationMode); the account has restrictOutboundNetworkAccess, allowedFqdnList,
// publicNetworkAccess and networkInjections. An account-level
// managedNetworkSettings property does not exist in that spec and is ignored.
func egressOfAccount(in *sdk.Input, a *acc, r sdk.ARMResource, networks []sdk.ARMResource) {
	self := armexpr.ParseName(r.Type, r.Name)
	for _, n := range networks {
		parent, ok := accountParent(n)
		if !ok {
			a.unresolved = true
			a.note(n, "managed network parent account cannot be determined")
			continue
		}
		switch armexpr.Compare(parent, self) {
		case armexpr.Unknown:
			a.unresolved = true
			a.note(n, "managed network may belong to account "+armexpr.StripScope(r.Name))
		case armexpr.Same:
			if a.maybe(n) {
				return
			}
			mode, st := str(n.Properties, "managedNetwork", "isolationMode")
			switch {
			case st == expr:
				a.unresolved = true
				a.note(n, "managedNetwork.isolationMode is an unresolved expression")
				return
			case st == absent:
			case strings.EqualFold(mode, "AllowOnlyApprovedOutbound"):
				return
			default:
				a.addKey(n, "managedNetwork.isolationMode", "outbound network posture: managed network isolationMode is "+mode+"; egress is not limited to approved destinations")
				a.fs[len(a.fs)-1].Severity = sdk.SeverityInfo
				return
			}
		}
	}
	switch boolState(r.Properties, "restrictOutboundNetworkAccess") {
	case isTrue:
		list, _ := get(r.Properties, "allowedFqdnList")
		if l, ok := list.([]any); ok && len(l) > 0 {
			return
		}
		a.info(r, "uncertain", "restrictOutboundNetworkAccess is true with an empty allowedFqdnList; the effect is not documented in the spec")
		return
	case expr:
		a.unresolved = true
		a.note(r, "restrictOutboundNetworkAccess is an unresolved expression")
		return
	}
	pna, st := str(r.Properties, "publicNetworkAccess")
	if st == expr {
		a.unresolved = true
		a.note(r, "publicNetworkAccess is an unresolved expression")
		return
	}
	inj, hasInj := get(r.Properties, "networkInjections")
	injected := false
	if hasInj {
		if unresolved(inj) {
			a.unresolved = true
			a.note(r, "networkInjections is an unresolved expression")
			return
		}
		l, _ := inj.([]any)
		injected = len(l) > 0
	}
	if strings.EqualFold(pna, "Disabled") || injected {
		a.info(r, "uncertain", "the account is private or network-injected, so outbound control lives in the virtual network and is not visible in the account resource")
		return
	}
	a.add(r, "outbound network posture does not show an approved-only egress configuration (public account with no managedNetworks isolationMode and no restrictOutboundNetworkAccess)")
	a.fs[len(a.fs)-1].Severity = sdk.SeverityInfo
}
