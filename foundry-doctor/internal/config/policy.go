package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Policy holds the organisation values defined by ADR-007. A nil pointer or nil
// slice means the key is absent; rules that need an absent key are skipped with
// a profile-key-missing reason rather than guessing.
type Policy struct {
	ResourceScope         *string            `yaml:"resourceScope,omitempty"`
	AllowedExternalScopes []string           `yaml:"allowedExternalScopes,omitempty"`
	Identity              *PolicyIdentity    `yaml:"identity,omitempty"`
	Environments          *PolicyEnvs        `yaml:"environments,omitempty"`
	Tags                  *PolicyTags        `yaml:"tags,omitempty"`
	LogRetention          *PolicyRetention   `yaml:"logRetention,omitempty"`
	Models                *PolicyModels      `yaml:"models,omitempty"`
	DataResidency         *PolicyResidency   `yaml:"dataResidency,omitempty"`
	DisasterRecovery      *PolicyDR          `yaml:"disasterRecovery,omitempty"`
	Network               *PolicyNetwork     `yaml:"network,omitempty"`
	Monitoring            *PolicyMonitor     `yaml:"monitoring,omitempty"`
	ManagedByAzurePolicy  []string           `yaml:"managedByAzurePolicy,omitempty"`
	Knowledge             *PolicyKnowledge   `yaml:"knowledge,omitempty"`
	Cost                  *PolicyCost        `yaml:"cost,omitempty"`
	Preflight             *PolicyPreflight   `yaml:"preflight,omitempty"`
	Reliability           *PolicyReliability `yaml:"reliability,omitempty"`
	Runtime               *PolicyRuntime     `yaml:"runtime,omitempty"`
	AzurePolicy           *PolicyAzurePolicy `yaml:"azurePolicy,omitempty"`
	Encryption            *PolicyEncryption  `yaml:"encryption,omitempty"`
}

// PolicyAzurePolicy lists the Azure Policy definitions that FND-SEC-009 treats as covering the baseline.
type PolicyAzurePolicy struct {
	MappedDefinitionIDs []string `yaml:"mappedDefinitionIds,omitempty"`
}

// PolicyEncryption declares an encryption requirement read by FND-SEC-010.
type PolicyEncryption struct {
	RequireCustomerManagedKeys *bool `yaml:"requireCustomerManagedKeys,omitempty"`
}

// PolicyReliability configures reliability expectations read by FND-REL-004 and FND-REL-007.
type PolicyReliability struct {
	// LocksManagedOutsideTemplate says management locks are applied outside the template, so a missing lock is uncertain.
	LocksManagedOutsideTemplate *bool `yaml:"locksManagedOutsideTemplate,omitempty"`
	// DisasterRecovery is the recovery objective: single-region, required, multi-region, hot-hot, hot-warm or hot-cold.
	DisasterRecovery *string `yaml:"disasterRecovery,omitempty"`
}

// PolicyRuntime configures runtime diagnosis thresholds (FND-RUN-004).
type PolicyRuntime struct {
	Throttling *PolicyThrottling `yaml:"throttling,omitempty"`
}

// PolicyThrottling sets when sustained HTTP 429 responses fail FND-RUN-004.
type PolicyThrottling struct {
	MaxRate429    *float64 `yaml:"maxRate429,omitempty"`
	WindowMinutes *int     `yaml:"windowMinutes,omitempty"`
	MinRequests   *int     `yaml:"minRequests,omitempty"`
}

// PolicyIdentity configures identity-specific policy inputs.
type PolicyIdentity struct {
	DeploymentPrincipalIDs []string `yaml:"deploymentPrincipalIds,omitempty"`
}

// PolicyEnvs classifies environment names.
type PolicyEnvs struct {
	Production    []string          `yaml:"production,omitempty"`
	NonProduction []string          `yaml:"nonProduction,omitempty"`
	Development   []string          `yaml:"development,omitempty"`
	Tiers         map[string]string `yaml:"tiers,omitempty"`
}

// TagRequirement is a required tag with an optional value format regex.
type TagRequirement struct {
	Name   string `yaml:"name"`
	Format string `yaml:"format,omitempty"`
}

// PolicyTags configures tag requirements.
type PolicyTags struct {
	Required      []TagRequirement `yaml:"required,omitempty"`
	ResourceTypes []string         `yaml:"resourceTypes,omitempty"`
}

// PolicyRetention configures log retention.
type PolicyRetention struct {
	MinimumDays *int `yaml:"minimumDays,omitempty"`
}

// PolicyModels holds model allow and deny lists (format/name[/version]).
type PolicyModels struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// PolicyResidency configures data residency expectations.
type PolicyResidency struct {
	Scope          *string  `yaml:"scope,omitempty"`
	Regions        []string `yaml:"regions,omitempty"`
	DeploymentSkus []string `yaml:"deploymentSkus,omitempty"`
}

// PolicyDR declares disaster recovery handling.
type PolicyDR struct {
	Declared  *bool   `yaml:"declared,omitempty"`
	Reference *string `yaml:"reference,omitempty"`
}

// PolicyNetwork configures the public access stance.
type PolicyNetwork struct {
	PublicAccess       *string `yaml:"publicAccess,omitempty"`
	DNSManagedByPolicy *bool   `yaml:"dnsManagedByPolicy,omitempty"`
	CentralDNS         *bool   `yaml:"centralDns,omitempty"`
	Cloud              *string `yaml:"cloud,omitempty"`
}

// PolicyMonitor configures monitoring expectations.
type PolicyMonitor struct {
	PublicTelemetry *bool `yaml:"publicTelemetry,omitempty"`
}

// PolicyKnowledge configures knowledge source expectations.
type PolicyKnowledge struct {
	RequireDocumentLevelAccess *bool `yaml:"requireDocumentLevelAccess,omitempty"`
}

// PolicyCost configures cost expectations.
type PolicyCost struct {
	DevMaxCosmosThroughput       *int     `yaml:"devMaxCosmosThroughput,omitempty"`
	ProductionSizedSkuExemptions []string `yaml:"productionSizedSkuExemptions,omitempty"`
}

// PolicyPreflight configures product-decision thresholds for DEP rules.
type PolicyPreflight struct {
	DeploymentHistoryMargin   *int `yaml:"deploymentHistoryMargin,omitempty"`
	RegionMatrixStalenessDays *int `yaml:"regionMatrixStalenessDays,omitempty"`
}

// Accepted enum values.
var (
	resourceScopes   = []string{"same-resource-group", "same-subscription", "any"}
	publicAccesses   = []string{"forbidden", "entra-only", "allowed"}
	cloudKinds       = []string{"public", "usgov", "china"}
	residencyScope   = []string{"global", "datazone-us", "datazone-eu", "datazone-apac", "geography"}
	azurePolicyMgd   = []string{"private-dns-zone-group", "diagnostic-settings"}
	environmentTiers = []string{"dev", "test", "prod"}
	drObjectives     = []string{"single-region", "required", "multi-region", "hot-hot", "hot-warm", "hot-cold"}
)

// Policy defaults from ADR-007. Only keys with a documented default appear.
const (
	DefaultResourceScope             = "same-resource-group"
	DefaultPublicAccess              = "forbidden"
	DefaultDeploymentHistoryMargin   = 80
	DefaultRegionMatrixStalenessDays = 365
)

// PolicyKeys returns every supported dotted policy key, sorted.
func PolicyKeys() []string {
	keys := []string{
		"resourceScope", "allowedExternalScopes",
		"identity.deploymentPrincipalIds",
		"environments.production", "environments.nonProduction", "environments.development", "environments.tiers",
		"tags.required", "tags.resourceTypes",
		"logRetention.minimumDays",
		"models.allow", "models.deny",
		"dataResidency.scope", "dataResidency.regions", "dataResidency.deploymentSkus",
		"disasterRecovery.declared", "disasterRecovery.reference",
		"network.publicAccess", "network.dnsManagedByPolicy", "network.centralDns", "network.cloud",
		"monitoring.publicTelemetry",
		"managedByAzurePolicy",
		"knowledge.requireDocumentLevelAccess",
		"cost.devMaxCosmosThroughput", "cost.productionSizedSkuExemptions",
		"preflight.deploymentHistoryMargin", "preflight.regionMatrixStalenessDays",
		"reliability.locksManagedOutsideTemplate", "reliability.disasterRecovery",
		"azurePolicy.mappedDefinitionIds", "encryption.requireCustomerManagedKeys",
		"runtime.throttling.maxRate429", "runtime.throttling.windowMinutes", "runtime.throttling.minRequests",
	}
	slices.Sort(keys)
	return keys
}

// flatten returns the keys this layer sets. Empty-but-present lists count.
func (p Policy) flatten() map[string]any {
	m := map[string]any{}
	setS := func(k string, v *string) {
		if v != nil {
			m[k] = *v
		}
	}
	setB := func(k string, v *bool) {
		if v != nil {
			m[k] = *v
		}
	}
	setI := func(k string, v *int) {
		if v != nil {
			m[k] = *v
		}
	}
	setF := func(k string, v *float64) {
		if v != nil {
			m[k] = *v
		}
	}
	setL := func(k string, v []string) {
		if v != nil {
			m[k] = slices.Clone(v)
		}
	}
	setS("resourceScope", p.ResourceScope)
	setL("allowedExternalScopes", p.AllowedExternalScopes)
	if i := p.Identity; i != nil {
		setL("identity.deploymentPrincipalIds", i.DeploymentPrincipalIDs)
	}
	if e := p.Environments; e != nil {
		setL("environments.production", e.Production)
		setL("environments.nonProduction", e.NonProduction)
		setL("environments.development", e.Development)
		if e.Tiers != nil {
			cp := map[string]string{}
			for k, v := range e.Tiers {
				cp[k] = v
			}
			m["environments.tiers"] = cp
		}
	}
	if t := p.Tags; t != nil {
		if t.Required != nil {
			m["tags.required"] = slices.Clone(t.Required)
		}
		setL("tags.resourceTypes", t.ResourceTypes)
	}
	if l := p.LogRetention; l != nil {
		setI("logRetention.minimumDays", l.MinimumDays)
	}
	if md := p.Models; md != nil {
		setL("models.allow", md.Allow)
		setL("models.deny", md.Deny)
	}
	if d := p.DataResidency; d != nil {
		setS("dataResidency.scope", d.Scope)
		setL("dataResidency.regions", d.Regions)
		setL("dataResidency.deploymentSkus", d.DeploymentSkus)
	}
	if d := p.DisasterRecovery; d != nil {
		setB("disasterRecovery.declared", d.Declared)
		setS("disasterRecovery.reference", d.Reference)
	}
	if n := p.Network; n != nil {
		setS("network.publicAccess", n.PublicAccess)
		setB("network.dnsManagedByPolicy", n.DNSManagedByPolicy)
		setB("network.centralDns", n.CentralDNS)
		setS("network.cloud", n.Cloud)
	}
	if mo := p.Monitoring; mo != nil {
		setB("monitoring.publicTelemetry", mo.PublicTelemetry)
	}
	setL("managedByAzurePolicy", p.ManagedByAzurePolicy)
	if k := p.Knowledge; k != nil {
		setB("knowledge.requireDocumentLevelAccess", k.RequireDocumentLevelAccess)
	}
	if c := p.Cost; c != nil {
		setI("cost.devMaxCosmosThroughput", c.DevMaxCosmosThroughput)
		setL("cost.productionSizedSkuExemptions", c.ProductionSizedSkuExemptions)
	}
	if pf := p.Preflight; pf != nil {
		setI("preflight.deploymentHistoryMargin", pf.DeploymentHistoryMargin)
		setI("preflight.regionMatrixStalenessDays", pf.RegionMatrixStalenessDays)
	}
	if r := p.Reliability; r != nil {
		setB("reliability.locksManagedOutsideTemplate", r.LocksManagedOutsideTemplate)
		setS("reliability.disasterRecovery", r.DisasterRecovery)
	}
	if a := p.AzurePolicy; a != nil {
		setL("azurePolicy.mappedDefinitionIds", a.MappedDefinitionIDs)
	}
	if e := p.Encryption; e != nil {
		setB("encryption.requireCustomerManagedKeys", e.RequireCustomerManagedKeys)
	}
	if r := p.Runtime; r != nil && r.Throttling != nil {
		setF("runtime.throttling.maxRate429", r.Throttling.MaxRate429)
		setI("runtime.throttling.windowMinutes", r.Throttling.WindowMinutes)
		setI("runtime.throttling.minRequests", r.Throttling.MinRequests)
	}
	return m
}

var (
	modelRe = regexp.MustCompile(`^[^/\s]+/[^/\s]+(/[^/\s]+)?$`)
	guidRe  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func (p Policy) validate(prefix string) []error {
	var probs []error
	add := func(key, format string, a ...any) {
		probs = append(probs, fmt.Errorf("%s.%s: %s", prefix, key, fmt.Sprintf(format, a...)))
	}
	enum := func(key string, v *string, allowed []string) {
		if v != nil && !slices.Contains(allowed, *v) {
			add(key, "%q must be one of %s", *v, strings.Join(allowed, ", "))
		}
	}
	nonEmpty := func(key string, vs []string) {
		for _, v := range vs {
			if strings.TrimSpace(v) == "" {
				add(key, "entries must not be empty")
				return
			}
		}
	}
	enum("resourceScope", p.ResourceScope, resourceScopes)
	for _, s := range p.AllowedExternalScopes {
		if !strings.HasPrefix(strings.ToLower(s), "/subscriptions/") {
			add("allowedExternalScopes", "%q must be an Azure resource ID starting with /subscriptions/", s)
		}
	}
	if i := p.Identity; i != nil {
		nonEmpty("identity.deploymentPrincipalIds", i.DeploymentPrincipalIDs)
		for _, v := range i.DeploymentPrincipalIDs {
			if strings.TrimSpace(v) != "" && !guidRe.MatchString(v) {
				add("identity.deploymentPrincipalIds", "%q must be a GUID principal ID", v)
			}
		}
	}
	if e := p.Environments; e != nil {
		nonEmpty("environments.production", e.Production)
		nonEmpty("environments.nonProduction", e.NonProduction)
		nonEmpty("environments.development", e.Development)
		for name, tier := range e.Tiers {
			if !envNameRe.MatchString(name) {
				add("environments.tiers", "%q is not a valid environment name", name)
			}
			if !slices.Contains(environmentTiers, strings.ToLower(strings.TrimSpace(tier))) {
				add("environments.tiers", "%q must be one of %s", tier, strings.Join(environmentTiers, ", "))
			}
		}
		seen := map[string]string{}
		for k, l := range map[string][]string{"production": e.Production, "nonProduction": e.NonProduction, "development": e.Development} {
			for _, n := range l {
				if o, dup := seen[strings.ToLower(n)]; dup && o != k {
					add("environments", "%q is listed under both %s and %s", n, o, k)
				}
				seen[strings.ToLower(n)] = k
			}
		}
	}
	if t := p.Tags; t != nil {
		for _, r := range t.Required {
			if strings.TrimSpace(r.Name) == "" {
				add("tags.required", "name is required")
			}
			if r.Format != "" {
				if _, err := regexp.Compile(r.Format); err != nil {
					add("tags.required", "format for %q is not a valid regular expression", r.Name)
				}
			}
		}
		nonEmpty("tags.resourceTypes", t.ResourceTypes)
	}
	if l := p.LogRetention; l != nil && l.MinimumDays != nil && *l.MinimumDays < 1 {
		add("logRetention.minimumDays", "must be at least 1")
	}
	if md := p.Models; md != nil {
		for _, v := range append(slices.Clone(md.Allow), md.Deny...) {
			if !modelRe.MatchString(v) {
				add("models", "%q must be format/name or format/name/version", v)
			}
		}
	}
	if d := p.DataResidency; d != nil {
		enum("dataResidency.scope", d.Scope, residencyScope)
		nonEmpty("dataResidency.regions", d.Regions)
		nonEmpty("dataResidency.deploymentSkus", d.DeploymentSkus)
	}
	if n := p.Network; n != nil {
		enum("network.publicAccess", n.PublicAccess, publicAccesses)
		enum("network.cloud", n.Cloud, cloudKinds)
	}
	for _, v := range p.ManagedByAzurePolicy {
		if !slices.Contains(azurePolicyMgd, v) {
			add("managedByAzurePolicy", "%q must be one of %s", v, strings.Join(azurePolicyMgd, ", "))
		}
	}
	if c := p.Cost; c != nil {
		if c.DevMaxCosmosThroughput != nil && *c.DevMaxCosmosThroughput < 1 {
			add("cost.devMaxCosmosThroughput", "must be at least 1")
		}
		nonEmpty("cost.productionSizedSkuExemptions", c.ProductionSizedSkuExemptions)
	}
	if a := p.AzurePolicy; a != nil {
		nonEmpty("azurePolicy.mappedDefinitionIds", a.MappedDefinitionIDs)
		for _, id := range a.MappedDefinitionIDs {
			if !guidRe.MatchString(id) {
				add("azurePolicy.mappedDefinitionIds", "%q must be a GUID", id)
			}
		}
	}
	if r := p.Reliability; r != nil && r.DisasterRecovery != nil && !slices.Contains(drObjectives, *r.DisasterRecovery) {
		add("reliability.disasterRecovery", "%q must be one of %s", *r.DisasterRecovery, strings.Join(drObjectives, ", "))
	}
	if r := p.Runtime; r != nil && r.Throttling != nil {
		t := r.Throttling
		if t.MaxRate429 != nil && (*t.MaxRate429 <= 0 || *t.MaxRate429 > 1) {
			add("runtime.throttling.maxRate429", "must be a ratio above 0 and at most 1")
		}
		if t.WindowMinutes != nil && (*t.WindowMinutes < 5 || *t.WindowMinutes > 1440) {
			add("runtime.throttling.windowMinutes", "must be from 5 to 1440")
		}
		if t.MinRequests != nil && *t.MinRequests < 1 {
			add("runtime.throttling.minRequests", "must be at least 1")
		}
	}
	if pf := p.Preflight; pf != nil {
		if pf.DeploymentHistoryMargin != nil && *pf.DeploymentHistoryMargin < 0 {
			add("preflight.deploymentHistoryMargin", "must be at least 0")
		}
		if pf.RegionMatrixStalenessDays != nil && *pf.RegionMatrixStalenessDays < 1 {
			add("preflight.regionMatrixStalenessDays", "must be at least 1")
		}
	}
	return probs
}

// EffectivePolicy is the resolved, flattened policy. It implements sdk.Policy.
type EffectivePolicy map[string]any

// Get returns the value for a dotted key relative to policy:. Lists are
// returned as copies so callers cannot mutate the policy. An absent key
// reports false, which makes dependent rules skip.
func (e EffectivePolicy) Get(key string) (any, bool) {
	v, ok := e[key]
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case []string:
		return slices.Clone(t), true
	case []TagRequirement:
		return slices.Clone(t), true
	}
	return v, true
}

// Keys returns the keys that are set, sorted.
func (e EffectivePolicy) Keys() []string {
	ks := make([]string, 0, len(e))
	for k := range e {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// Dump renders the effective policy as deterministic YAML (sorted keys) for the
// report header.
func (e EffectivePolicy) Dump() (string, error) {
	b, err := yaml.Marshal(map[string]any(e))
	if err != nil {
		return "", fmt.Errorf("render effective policy: %w", err)
	}
	return string(b), nil
}
