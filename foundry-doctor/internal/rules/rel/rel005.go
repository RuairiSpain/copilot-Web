package rel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeSearch = "Microsoft.Search/searchServices"

	maxReplicas      = 12
	maxReplicasBasic = 3
	maxSearchUnits   = 36
	maxHighDensity   = 3

	recommendation005 = "Choose a partitionCount from 1, 2, 3, 4, 6 or 12 and a replicaCount of 12 or fewer (3 or fewer on basic), keep replicas x partitions at or below 36 search units, and use hostingMode HighDensity only with standard3 and at most 3 partitions. See the partition and replica combinations table in the capacity planning article."
	fix005            = "resource search 'Microsoft.Search/searchServices@2025-05-01' = {\n  name: searchName\n  location: location\n  sku: { name: 'standard' }\n  properties: {\n    replicaCount: 3\n    partitionCount: 2   // 6 search units, within the 36 SU limit\n  }\n}"
)

var validPartitions = []int{1, 2, 3, 4, 6, 12}

type rel005 struct{}

func (rel005) ID() string   { return "FND-REL-005" }
func (rel005) Version() int { return 1 }

func lowered(c sdk.Confidence, m model.ResourceMeta) sdk.Confidence {
	if !m.MayNotDeploy && !m.MultiInstance {
		return c
	}
	if c == sdk.ConfidenceCertain {
		return sdk.ConfidenceLikely
	}
	return sdk.ConfidenceUncertain
}

// intValue reads a whole-number literal.
func intValue(v model.Value) (int, bool) {
	f, ok := v.Number()
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

func (r rel005) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if in.ARM == nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonSyntheticInfrastructure, "iac: "+string(in.Project.IaC),
			"compiled ARM template", sdk.ResourceRef{Kind: "project", Name: in.Project.Name})}, nil
	}
	var out []rules.Result
	for _, res := range in.ARM.ResourcesOfType(typeSearch) {
		out = append(out, r.check(res)...)
	}
	return out, nil
}

func (r rel005) check(res model.Resource) []rules.Result {
	ref := sdk.ResourceRef{Kind: "arm-resource", Type: res.Type, Name: res.Name, Pointer: res.Meta.Pointer}
	loc := sdk.Location{File: res.File, Line: res.Location.Line, Column: res.Location.Column, Pointer: res.Meta.Pointer}
	skuV, repV, parV, hostV := res.Get("sku.name"), res.Get("properties.replicaCount"), res.Get("properties.partitionCount"), res.Get("properties.hostingMode")

	if sku, ok := skuV.String(); ok {
		switch strings.ToLower(sku) {
		case "free", "serverless":
			return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
				"replica and partition values for the "+sku+" SKU are documented as not applicable", "", ref)}
		}
	}
	if repV.State == model.Absent && parV.State == model.Absent {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"replicaCount and partitionCount are absent: the platform defaults apply, nothing to check", "", ref)}
	}

	sku := ""
	if s, ok := skuV.String(); ok {
		sku = strings.ToLower(s)
	}
	reps, repOK := intValue(repV)
	pars, parOK := intValue(parV)
	host, hostOK := hostV.String()
	highDensity := hostOK && strings.EqualFold(host, "HighDensity")

	var out []rules.Result
	add := func(res sdk.Finding) { out = append(out, rules.Fail(res)) }
	mk := func(key, evidence string, c sdk.Confidence) sdk.Finding {
		f := rules.NewFinding(r, ref, evidence, recommendation005)
		f.Key = key
		f.Location = loc
		f.Fix = fix005
		f.Confidence = lowered(c, res.Meta)
		return f
	}

	if parOK && !slices.Contains(validPartitions, pars) {
		add(mk("partition-count", fmt.Sprintf("partitionCount %d is not one of 1, 2, 3, 4, 6 or 12.", pars), sdk.ConfidenceCertain))
	}
	if repOK {
		switch {
		case reps < 1 || reps > maxReplicas:
			add(mk("replica-count", fmt.Sprintf("replicaCount %d is outside 1 to %d.", reps, maxReplicas), sdk.ConfidenceCertain))
		case sku == "basic" && reps > maxReplicasBasic:
			add(mk("replica-count-basic", fmt.Sprintf("replicaCount %d exceeds %d, the maximum on the basic SKU.", reps, maxReplicasBasic), sdk.ConfidenceCertain))
		}
	}
	if repOK && parOK && sku != "" && sku != "basic" && reps*pars > maxSearchUnits {
		add(mk("search-units", fmt.Sprintf("replicaCount %d x partitionCount %d = %d search units, above the maximum of %d.", reps, pars, reps*pars, maxSearchUnits), sdk.ConfidenceCertain))
	}
	if highDensity {
		if sku != "" && sku != "standard3" {
			add(mk("high-density-sku", fmt.Sprintf("hostingMode HighDensity is only valid with the standard3 SKU, not %q.", sku), sdk.ConfidenceCertain))
		}
		if parOK && pars > maxHighDensity {
			add(mk("high-density-partitions", fmt.Sprintf("hostingMode HighDensity allows at most %d partitions, not %d.", maxHighDensity, pars), sdk.ConfidenceCertain))
		}
	}

	var unknown []string
	for _, p := range []struct {
		name string
		v    model.Value
		ok   bool
	}{{"sku.name", skuV, sku != ""}, {"replicaCount", repV, repOK}, {"partitionCount", parV, parOK}, {"hostingMode", hostV, hostOK}} {
		if p.v.State == model.Unresolved || (p.v.State == model.Literal && !p.ok) {
			unknown = append(unknown, p.name)
		}
	}
	if len(unknown) > 0 {
		f := mk("unresolved-values", "The value of "+strings.Join(unknown, ", ")+" is not known offline, so the sizing limits cannot be fully checked.", sdk.ConfidenceUncertain)
		out = append(out, rules.Uncertain(f))
	}
	if sku == "basic" && parOK && pars > 1 {
		f := mk("basic-partitions", fmt.Sprintf("partitionCount %d on the basic SKU: the specification allows values above 1 only for standard SKUs, but the limits article lists 3 partitions for basic services created after April 3, 2024. The creation date and region are not visible in compiled ARM.", pars), sdk.ConfidenceUncertain)
		out = append(out, rules.Uncertain(f))
	}
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}
