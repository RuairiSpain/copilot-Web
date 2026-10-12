package idn

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Resource types read by this group. They come from the catalogue entries FND-IDN-001..006.
const (
	typeAccount     = "Microsoft.CognitiveServices/accounts"
	typeProject     = "Microsoft.CognitiveServices/accounts/projects"
	typeConnection  = "Microsoft.CognitiveServices/accounts/projects/connections"
	typeContainerAp = "Microsoft.App/containerApps"
	typeSite        = "Microsoft.Web/sites"
	typeSlot        = "Microsoft.Web/sites/slots"
	typeRoleAssign  = "Microsoft.Authorization/roleAssignments"
	typeUAMI        = "Microsoft.ManagedIdentity/userAssignedIdentities"
	typeFIC         = "Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials"
	typeCosmosRole  = "Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments"
)

// meta holds the catalogue identity of a rule.
type meta struct {
	id  string
	ver int
}

func (m meta) ID() string   { return m.id }
func (m meta) Version() int { return m.ver }

// armGuard returns the synthetic-infrastructure skip when the input carries no compiled template (ADR-012).
// The engine short-circuits the same case; this is the guard for direct callers.
func armGuard(r rules.Rule, in *model.Input) ([]rules.Result, bool) {
	if in != nil && in.ARM != nil {
		return nil, false
	}
	detail := "no compiled ARM template is available"
	if in != nil && in.Project.IaC != "" {
		detail = "iac: " + string(in.Project.IaC)
	}
	return []rules.Result{rules.SkipFor(r, rules.ReasonSyntheticInfrastructure, detail, "compiled ARM template", sdk.ResourceRef{})}, true
}

// liveSkip is the skipped result for a check that needs live role assignments (Phase 2).
func liveSkip(r rules.Rule, detail string) rules.Result {
	return rules.SkipFor(r, rules.ReasonControlPlaneUnavailable, detail, "live Azure access (role assignment read)", sdk.ResourceRef{})
}

func refOf(r model.Resource, pointer string) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "arm-resource", Type: r.Type, Name: r.Name, Pointer: r.Meta.Pointer + pointer}
}

func locationOf(r model.Resource, pointer string) sdk.Location {
	return sdk.Location{File: r.File, Line: r.Location.Line, Column: r.Location.Column, Pointer: r.Meta.Pointer + pointer}
}

// lower lowers a confidence by one step.
func lower(c sdk.Confidence) sdk.Confidence {
	switch c {
	case sdk.ConfidenceCertain:
		return sdk.ConfidenceLikely
	default:
		return sdk.ConfidenceUncertain
	}
}

// confidenceFor applies the resource meta to a base confidence: a resource that may not deploy, may be deployed
// several times, or sits in a module whose file cannot be proven lowers it one step.
func confidenceFor(r model.Resource, base sdk.Confidence) sdk.Confidence {
	if r.Meta.MayNotDeploy || r.Meta.MultiInstance || r.Meta.Confidence == string(sdk.ConfidenceUncertain) {
		return lower(base)
	}
	return base
}

// finding builds a finding for a resource. pointer is a JSON pointer suffix below the resource.
func finding(r rules.Rule, res model.Resource, key, pointer, evidence, recommendation, fix string, base sdk.Confidence) sdk.Finding {
	f := rules.NewFinding(r, refOf(res, pointer), evidence, recommendation)
	f.Key = key
	f.Fix = fix
	f.Location = locationOf(res, pointer)
	f.Confidence = confidenceFor(res, base)
	return f
}

func fail(r rules.Rule, res model.Resource, key, pointer, evidence, rec, fix string, base sdk.Confidence) rules.Result {
	return rules.Fail(finding(r, res, key, pointer, evidence, rec, fix, base))
}

func uncertain(r rules.Rule, res model.Resource, key, pointer, evidence, rec, fix string) rules.Result {
	return rules.Uncertain(finding(r, res, key, pointer, evidence, rec, fix, sdk.ConfidenceUncertain))
}

func skipRes(r rules.Rule, res model.Resource, reason, detail, missing string) rules.Result {
	return rules.SkipFor(r, reason, detail, missing, refOf(res, ""))
}

// ---- identity ----

type identKind int

const (
	identAbsent identKind = iota
	identNone
	identSystem
	identUser
	identBoth
	identParam      // identity.type depends on a parameter
	identModule     // identity.type is read from a module output
	identUnresolved // another expression
)

func exprKind(s string) identKind {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "outputs"):
		return identModule
	case strings.Contains(l, "parameters("):
		return identParam
	}
	return identUnresolved
}

// identityOf classifies identity.type of a resource body.
func identityOf(r model.Resource) identKind {
	raw, ok := r.Body["identity"]
	if !ok || raw == nil {
		return identAbsent
	}
	if s, ok := raw.(string); ok {
		if model.IsARMExpression(s) {
			return exprKind(s)
		}
		return identUnresolved
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return identUnresolved
	}
	t, ok := m["type"]
	if !ok || t == nil {
		return identAbsent
	}
	s, ok := t.(string)
	if !ok {
		return identUnresolved
	}
	if model.IsARMExpression(s) {
		return exprKind(s)
	}
	l := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	sys, user := strings.Contains(l, "systemassigned"), strings.Contains(l, "userassigned")
	switch {
	case sys && user:
		return identBoth
	case sys:
		return identSystem
	case user:
		return identUser
	case l == "none":
		return identNone
	}
	return identUnresolved
}

func (k identKind) hasSystem() bool { return k == identSystem || k == identBoth }
func (k identKind) hasUser() bool   { return k == identUser || k == identBoth }

// userIdentityKeys returns the keys of identity.userAssignedIdentities. unresolved is true when the map is an
// expression and its keys are therefore unknown.
func userIdentityKeys(r model.Resource) (keys []string, unresolved bool) {
	raw, ok := rawAt(r.Body, "identity", "userAssignedIdentities")
	if !ok || raw == nil {
		return nil, false
	}
	switch m := raw.(type) {
	case map[string]any:
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return keys, false
	case string:
		return nil, true
	}
	return nil, false
}

// attachedIdentities returns the user-assigned identities of the template that r attaches.
func attachedIdentities(t *model.ARMTemplate, r model.Resource) []model.Resource {
	keys, _ := userIdentityKeys(r)
	var out []model.Resource
	for _, k := range keys {
		ref := parseID(k)
		if ref.kind != idResource || !strings.EqualFold(ref.typ, typeUAMI) {
			continue
		}
		for _, u := range match(t, ref) {
			if !containsRes(out, u) {
				out = append(out, u)
			}
		}
	}
	return out
}

// ---- principals ----

type principalKind int

const (
	principalAbsent    principalKind = iota
	principalLiteral                 // a GUID literal
	principalBadLit                  // a literal that is not a GUID
	principalParam                   // [parameters('x')]
	principalModule                  // read from a module (deployment) output
	principalRef                     // reference(...) of a resource, with a property chain
	principalUnknown                 // another expression
	principalNotString               // a non-string value
)

type fieldKind int

const (
	fieldUnknown fieldKind = iota
	fieldObjectID
	fieldClientID
)

// principal is the lexical reading of a role assignment principalId.
type principal struct {
	kind   principalKind
	param  string
	field  fieldKind
	system bool // the chain goes through .identity
	ref    idRef
	cands  []model.Resource // template resources the reference can denote
	chain  string
}

func resolvePrincipal(t *model.ARMTemplate, ra model.Resource) principal {
	raw, ok := rawAt(ra.Body, "properties", "principalId")
	if !ok || raw == nil {
		return principal{kind: principalAbsent}
	}
	s, ok := raw.(string)
	if !ok {
		return principal{kind: principalNotString}
	}
	if !model.IsARMExpression(s) {
		if isGUID(s) {
			return principal{kind: principalLiteral}
		}
		return principal{kind: principalBadLit}
	}
	e, _ := inner(s)
	fn, args, rest, ok := parseCall(e)
	if !ok {
		return principal{kind: principalUnknown}
	}
	switch strings.ToLower(fn) {
	case "parameters":
		if rest == "" && len(args) == 1 {
			name, _ := literalArg(args[0])
			return principal{kind: principalParam, param: name}
		}
	case "reference":
		if len(args) == 0 {
			return principal{kind: principalUnknown}
		}
		ref := parseIDInner(args[0])
		if strings.Contains(strings.ToLower(rest), ".outputs") || (ref.kind == idResource && strings.EqualFold(ref.typ, "Microsoft.Resources/deployments")) {
			return principal{kind: principalModule}
		}
		segs := strings.Split(strings.TrimPrefix(rest, "."), ".")
		p := principal{kind: principalRef, ref: ref, chain: rest}
		if rest == "" {
			return principal{kind: principalUnknown}
		}
		for _, sg := range segs {
			if !identRe.MatchString(sg) {
				return principal{kind: principalUnknown}
			}
			if strings.EqualFold(sg, "identity") {
				p.system = true
			}
		}
		switch strings.ToLower(segs[len(segs)-1]) {
		case "principalid", "objectid":
			p.field = fieldObjectID
		case "clientid", "applicationid", "appid":
			p.field = fieldClientID
		}
		p.cands = match(t, ref)
		return p
	}
	return principal{kind: principalUnknown}
}

// unique returns the single template resource the principal denotes.
func (p principal) unique() (model.Resource, bool) {
	if p.kind == principalRef && len(p.cands) == 1 {
		return p.cands[0], true
	}
	return model.Resource{}, false
}

// ---- scope ----

// scopeOf reads the top-level scope of a role assignment. present is false for a resource without one, which
// deploys at the deployment scope (a resource group or wider).
func scopeOf(ra model.Resource) (ref idRef, present bool) {
	raw, ok := ra.Body["scope"]
	if !ok || raw == nil {
		return idRef{}, false
	}
	s, ok := raw.(string)
	if !ok {
		return idRef{}, true
	}
	return parseID(s), true
}

// broad reports whether the scope kind is a subscription, management group, tenant or resource group.
func (r idRef) broad() bool {
	switch r.kind {
	case idSub, idMG, idTenant, idRG:
		return true
	}
	return false
}

func (r idRef) describe() string {
	switch r.kind {
	case idSub:
		return "subscription"
	case idMG:
		return "management group"
	case idTenant:
		return "tenant"
	case idRG:
		return "resource group"
	case idResource:
		return "resource " + r.typ
	}
	return "unknown scope"
}

// keyName is the stable scope part of a finding key.
func (r idRef) keyName() string {
	switch r.kind {
	case idSub:
		return "subscription"
	case idMG:
		return "management-group"
	case idTenant:
		return "tenant"
	case idRG:
		return "resource-group"
	case idResource:
		return "resource"
	}
	return "unknown"
}
