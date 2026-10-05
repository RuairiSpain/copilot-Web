package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Sources of an effective value.
const (
	SourceFlag       = "flag"
	SourceRepository = "repository"
	SourceProfile    = "profile"
	SourceDefault    = "default"
	SourceBaseline   = "baseline" // a rule's own documented behaviour (ADR-007 decision 3), not a configured value
	SourceUnset      = "unset"    // not configured; dependent rules are skipped
)

// SourceEnvironment is the source label of a value set under environments.<name>.policy.
func SourceEnvironment(name string) string { return "environment:" + name }

// PolicyValue is one line of the "effective policy" report header (ADR-007 decision 3).
type PolicyValue struct {
	Key      string // policy.* key as in profile-key-missing:<key>
	Value    string // rendered value; for an unset key without baseline, empty
	Source   string // flag-less: repository, environment:<name>, baseline or unset
	Baseline bool   // Value is the rule's own baseline, not a configured value
	Note     string // for unset keys: what happens
}

type policyKey struct {
	key      string
	isSet    func(*model.Policy) bool
	assign   func(dst, src *model.Policy) // copies (cloning slices) the field from src to dst
	render   func(*model.Policy) string
	baseline string // rendered baseline when absent; "" when the key has none
	absent   string // note for an absent key without baseline
}

func fieldKey[T any](key string, get func(*model.Policy) *T, baseline, absent string) policyKey {
	return policyKey{
		key:   key,
		isSet: func(p *model.Policy) bool { return !isNilValue(*get(p)) },
		assign: func(dst, src *model.Policy) {
			*get(dst) = cloneValue(*get(src))
		},
		render:   func(p *model.Policy) string { return renderValue(reflect.ValueOf(*get(p))) },
		baseline: baseline,
		absent:   absent,
	}
}

func isNilValue[T any](v T) bool {
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		return rv.IsNil()
	}
	return false
}

func cloneValue[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Slice:
		if !rv.IsNil() {
			c := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
			reflect.Copy(c, rv)
			rv.Set(c)
		}
	case reflect.Pointer:
		if !rv.IsNil() {
			c := reflect.New(rv.Type().Elem())
			c.Elem().Set(rv.Elem())
			rv.Set(c)
		}
	}
	return v
}

func renderValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return ""
		}
		return renderValue(v.Elem())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = renderValue(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Struct:
		if t, ok := v.Interface().(model.TagRequirement); ok {
			if t.Format != "" {
				return t.Name + " =~ " + t.Format
			}
			return t.Name
		}
	}
	return fmt.Sprint(v.Interface())
}

const (
	skipNote    = "not set; dependent rules are skipped with profile-key-missing"
	ruleOwnList = "the rule's own list"
)

// policyKeys is the ADR-007 key table, in report order. TestPolicyKeysCoverModel keeps it equal to model.Policy.
var policyKeys = []policyKey{
	fieldKey(model.KeyResourceScope, func(p *model.Policy) **model.ResourceScope { return &p.ResourceScope }, string(model.ScopeSameResourceGroup), ""),
	fieldKey(model.KeyAllowedExternalScopes, func(p *model.Policy) *[]string { return &p.AllowedExternalScopes }, "[]", ""),
	fieldKey(model.KeyEnvironmentsProduction, func(p *model.Policy) *[]string { return &p.Environments.Production }, "", skipNote),
	fieldKey(model.KeyEnvironmentsNonProduction, func(p *model.Policy) *[]string { return &p.Environments.NonProduction }, "", skipNote),
	fieldKey(model.KeyEnvironmentsDevelopment, func(p *model.Policy) *[]string { return &p.Environments.Development }, "", skipNote),
	fieldKey(model.KeyTagsRequired, func(p *model.Policy) *[]model.TagRequirement { return &p.Tags.Required }, "", skipNote),
	fieldKey(model.KeyTagsResourceTypes, func(p *model.Policy) *[]string { return &p.Tags.ResourceTypes }, ruleOwnList, ""),
	fieldKey(model.KeyLogRetentionMinimumDays, func(p *model.Policy) **int { return &p.LogRetention.MinimumDays }, "", skipNote),
	fieldKey(model.KeyModelsAllow, func(p *model.Policy) *[]string { return &p.Models.Allow }, "", skipNote),
	fieldKey(model.KeyModelsDeny, func(p *model.Policy) *[]string { return &p.Models.Deny }, "", skipNote),
	fieldKey(model.KeyDataResidencyScope, func(p *model.Policy) **model.DataResidencyScope { return &p.DataResidency.Scope }, "", skipNote),
	fieldKey(model.KeyDataResidencyRegions, func(p *model.Policy) *[]string { return &p.DataResidency.Regions }, "", skipNote),
	fieldKey(model.KeyDataResidencyDeploymentSkus, func(p *model.Policy) *[]string { return &p.DataResidency.DeploymentSkus }, "the SKUs whose documented processing scope satisfies the scope", ""),
	fieldKey(model.KeyDisasterRecoveryDeclared, func(p *model.Policy) **bool { return &p.DisasterRecovery.Declared }, "", "not set; informational question only"),
	fieldKey(model.KeyNetworkPublicAccess, func(p *model.Policy) **model.PublicAccess { return &p.Network.PublicAccess }, string(model.PublicForbidden), ""),
	fieldKey(model.KeyMonitoringPublicTelemetry, func(p *model.Policy) **bool { return &p.Monitoring.PublicTelemetry }, "false", ""),
	fieldKey(model.KeyManagedByAzurePolicy, func(p *model.Policy) *[]model.ManagedBy { return &p.ManagedByAzurePolicy }, "[]", ""),
	fieldKey(model.KeyKnowledgeRequireDocLevelAccess, func(p *model.Policy) **bool { return &p.Knowledge.RequireDocumentLevelAccess }, "", skipNote),
	fieldKey(model.KeyCostDevMaxCosmosThroughput, func(p *model.Policy) **int { return &p.Cost.DevMaxCosmosThroughput }, "", "not set; the Cosmos sub-check is skipped"),
	fieldKey(model.KeyCostProductionSizedSkuExempt, func(p *model.Policy) *[]string { return &p.Cost.ProductionSizedSkuExemptions }, "[]", ""),
}

// mergePolicy applies over onto base key by key. disasterRecovery.reference has no key of its own in the
// ADR table; it travels with policy.disasterRecovery.declared.
func mergePolicy(base *model.Policy, over model.Policy, srcs map[string]string, label string) {
	for _, k := range policyKeys {
		if k.isSet(&over) {
			k.assign(base, &over)
			srcs[k.key] = label
		}
	}
	// reference is part of policy.disasterRecovery.declared's entry
	if over.DisasterRecovery.Reference != nil {
		base.DisasterRecovery.Reference = cloneValue(over.DisasterRecovery.Reference)
		srcs[model.KeyDisasterRecoveryDeclared] = label
	}
}

// effectiveEntries renders the policy keys in table order with their source.
func effectiveEntries(p model.Policy, srcs map[string]string) []PolicyValue {
	out := make([]PolicyValue, 0, len(policyKeys))
	for _, k := range policyKeys {
		pv := PolicyValue{Key: k.key}
		switch {
		case k.isSet(&p):
			pv.Value = k.render(&p)
			pv.Source = srcs[k.key]
			if k.key == model.KeyDisasterRecoveryDeclared && p.DisasterRecovery.Reference != nil {
				// The reference may be a URL; never echo it into the report header.
				pv.Value += " (reference set)"
			}
		case k.baseline != "":
			pv.Value, pv.Source, pv.Baseline = k.baseline, SourceBaseline, true
		default:
			pv.Source, pv.Note = SourceUnset, k.absent
		}
		out = append(out, pv)
	}
	return out
}
