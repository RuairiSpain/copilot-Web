package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// skuScope is the documented processing scope of a deployment SKU (Learn, deployment types,
// ms.date 08/06/2026). A larger value is a broader processing scope.
type skuScope int

const (
	scopeGeography skuScope = iota + 1 // Standard, ProvisionedManaged
	scopeDataZone                      // DataZone*
	scopeGlobal                        // Global*
	scopeNone                          // DeveloperTier: no data residency guarantee
)

var skuScopes = map[string]skuScope{
	"GlobalStandard": scopeGlobal, "GlobalProvisionedManaged": scopeGlobal, "GlobalBatch": scopeGlobal,
	"DataZoneStandard": scopeDataZone, "DataZoneProvisionedManaged": scopeDataZone, "DataZoneBatch": scopeDataZone,
	"Standard": scopeGeography, "ProvisionedManaged": scopeGeography,
	"DeveloperTier": scopeNone,
}

// required is the broadest scope the declared requirement tolerates.
func required(s model.DataResidencyScope) skuScope {
	switch s {
	case model.ResidencyGlobal:
		return scopeGlobal
	case model.ResidencyGeography:
		return scopeGeography
	}
	return scopeDataZone
}

func scopeName(s skuScope) string {
	switch s {
	case scopeGlobal:
		return "global"
	case scopeDataZone:
		return "data zone"
	case scopeGeography:
		return "geography"
	}
	return "none"
}

// locationTypes are the resources whose location is compared with the declared regions.
var locationTypes = []string{typeAccount, typeSearch, typeStorage, typeCosmos}

type ops009 struct{}

func (ops009) ID() string   { return "FND-OPS-009" }
func (ops009) Version() int { return 1 }

func (r ops009) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	ref := projectRef(in)
	pol := in.Policy.DataResidency
	if pol.Scope == nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyDataResidencyScope),
			"no data-residency requirement is declared, so the rule is not evaluated", "policy.dataResidency.scope in the profile", ref)}, nil
	}
	if len(pol.Regions) == 0 {
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyDataResidencyRegions),
			"the region membership of data zones and geographies is not published, so the profile must list the allowed regions", "policy.dataResidency.regions in the profile", ref)}, nil
	}
	var out []rules.Result
	checked := false
	for _, d := range allDeployments(in) {
		res, ran := r.checkSKU(in, d)
		out = append(out, res...)
		checked = checked || ran
	}
	if in.ARM == nil {
		s, _ := needARM(r, in, "resource locations and ARM deployments")
		out = append(out, s)
	} else {
		locs, ran := r.checkLocations(in)
		out = append(out, locs...)
		checked = checked || ran
	}
	out = append(out, r.checkEnvLocation(in, &checked)...)
	out = append(out, rules.SkipFor(r, rules.ReasonControlPlaneUnavailable,
		"the deployed SKU and account location are confirmed with the deployments API in phase 2", "live Azure access", ref))
	if checked && !slices.ContainsFunc(out, func(x rules.Result) bool { return x.Status == sdk.StatusFail || x.Status == sdk.StatusUncertain }) {
		out = append(out, rules.Pass())
	}
	return out, nil
}

func (r ops009) finding(d deployment, key, evidence string, conf sdk.Confidence) sdk.Finding {
	f := rules.NewFinding(r, d.Ref, evidence, recommendation009)
	f.Key = key
	f.Location = d.Loc
	f.Fix = fix009
	f.Confidence = lowered(conf, d.Meta)
	return f
}

// checkSKU judges one deployment's SKU against the declared scope or the profile's SKU list.
func (r ops009) checkSKU(in *model.Input, d deployment) (out []rules.Result, ran bool) {
	pol := in.Policy.DataResidency
	switch d.SKU.State {
	case model.Unresolved:
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("sku-"+d.SKU.Reason),
			"the SKU of deployment "+d.label()+" is not known offline", "resolved deployment SKU", d.Ref)}, false
	case model.Absent:
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("sku-absent"),
			"deployment "+d.label()+" declares no SKU name", "deployment SKU", d.Ref)}, false
	}
	sku, ok := d.SKU.String()
	if !ok {
		return nil, false
	}
	if pol.DeploymentSkus != nil {
		if !contains(pol.DeploymentSkus, sku) {
			return []rules.Result{rules.Fail(r.finding(d, "sku-not-allowed", fmt.Sprintf("Deployment %q uses SKU %q, which is not in policy.dataResidency.deploymentSkus (compared exactly).", d.label(), sku), sdk.ConfidenceCertain))}, true
		}
		return nil, true
	}
	scope, known := skuScopes[sku]
	if !known {
		return []rules.Result{rules.Uncertain(r.finding(d, "sku-unknown", fmt.Sprintf("Deployment %q uses SKU %q, which is not in the documented SKU table; the specification types sku.name as a free string, so this is not asserted as a violation.", d.label(), sku), sdk.ConfidenceUncertain))}, true
	}
	switch {
	case scope == scopeNone:
		return []rules.Result{rules.Fail(r.finding(d, "developer-tier", fmt.Sprintf("Deployment %q uses SKU DeveloperTier, which has no data-residency guarantee, while the profile declares the requirement %q.", d.label(), *pol.Scope), sdk.ConfidenceCertain))}, true
	case scope > required(*pol.Scope):
		return []rules.Result{rules.Fail(r.finding(d, "sku-scope", fmt.Sprintf("Deployment %q uses SKU %q with %s processing scope, which is broader than the declared requirement %q.", d.label(), sku, scopeName(scope), *pol.Scope), sdk.ConfidenceCertain))}, true
	}
	return nil, true
}

// checkLocations compares the location of the Foundry stack resources with the declared regions.
func (r ops009) checkLocations(in *model.Input) (out []rules.Result, ran bool) {
	regions := in.Policy.DataResidency.Regions
	for _, res := range in.ARM.Resources {
		if !inList(locationTypes, res.Type) {
			continue
		}
		loc := res.Get("location")
		switch loc.State {
		case model.Absent:
			continue
		case model.Unresolved:
			out = append(out, rules.SkipFor(r, rules.ReasonUnresolvedValue("location-"+loc.Reason),
				"the location of "+res.Type+" is not known offline", "resolved resource location", armRef(res)))
			continue
		}
		s, ok := loc.String()
		if !ok {
			continue
		}
		ran = true
		if contains(regions, s) {
			continue
		}
		f := rules.NewFinding(r, armRef(res), fmt.Sprintf("%s %q is deployed to %q, which is not in policy.dataResidency.regions.", res.Type, res.Name, s), recommendation009)
		f.Key = "location"
		f.Location = armLoc(res)
		f.Fix = fix009
		f.Confidence = lowered(sdk.ConfidenceCertain, res.Meta)
		out = append(out, rules.Fail(f))
	}
	return out, ran
}

// checkEnvLocation compares AZURE_LOCATION of the selected azd environment with the declared regions.
func (r ops009) checkEnvLocation(in *model.Input, checked *bool) []rules.Result {
	if in.Environment == nil {
		return nil
	}
	v, ok := in.Environment.Get("AZURE_LOCATION")
	if !ok {
		return nil
	}
	*checked = true
	loc := strings.TrimSpace(v.Reveal())
	if contains(in.Policy.DataResidency.Regions, loc) {
		return nil
	}
	f := rules.NewFinding(r, sdk.ResourceRef{Kind: "env-key", Name: "AZURE_LOCATION"},
		fmt.Sprintf("AZURE_LOCATION of azd environment %q is not in policy.dataResidency.regions (the value is not echoed).", in.Environment.Name), recommendation009)
	f.Key = "env-location"
	f.Fix = fix009
	f.Confidence = sdk.ConfidenceCertain
	return []rules.Result{rules.Fail(f)}
}

const (
	recommendation009 = "Choose a deployment SKU that matches the residency requirement - Data Zone Standard or Data Zone Provisioned for a data zone, Standard or Regional Provisioned for a geography - in a region that belongs to that zone or geography, and keep the region list in the profile current."
	fix009            = "# azure.yaml\nservices:\n  ai-project:\n    host: azure.ai.project\n    deployments:\n      - name: chat\n        model: { format: OpenAI, name: gpt-5.4-mini, version: \"2026-03-17\" }\n        sku: { name: DataZoneStandard, capacity: 10 }"
)
