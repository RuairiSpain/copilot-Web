package cfg

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule012 is FND-CFG-012 (product opinion): region values are well-formed location names and consistent.
// Only the lexical form is checked offline. Membership in the subscription's location list needs the
// control plane, so it is always reported as skipped.
type rule012 struct{}

func (rule012) ID() string   { return "FND-CFG-012" }
func (rule012) Version() int { return 1 }

const (
	rec012 = "Use a location name returned by az account list-locations for your subscription (the name field, such as eastus2), and set it with azd env set AZURE_LOCATION. Check model and resource availability for the region before provisioning."
	fix012 = "azd env set AZURE_LOCATION eastus2\n"
)

// locationProblem describes why v is not in the lower-case, space-free form azd stores; "" means well formed.
func locationProblem(v string) string {
	var problems []string
	if v == "" {
		return "empty"
	}
	if v != strings.ToLower(v) {
		problems = append(problems, "uppercase letters")
	}
	if strings.ContainsAny(v, " \t") {
		problems = append(problems, "whitespace")
	}
	for _, c := range strings.ToLower(v) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != ' ' && c != '\t' {
			problems = append(problems, "characters other than letters and digits")
			break
		}
	}
	return strings.Join(problems, " and ")
}

func (r rule012) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if in == nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable, "no input", "input", sdk.ResourceRef{})}, nil
	}
	x := newRun(r, in)
	checked := false
	envRef := sdk.ResourceRef{Kind: "env-key", Name: "AZURE_LOCATION"}
	envLoc := ""

	if in.Environment != nil {
		if v, ok := in.Environment.Get("AZURE_LOCATION"); ok {
			raw := strings.TrimSpace(v.Reveal())
			switch {
			case hasRef(raw):
				x.skip(rules.ReasonUnresolvedValue("location-is-env-reference"), "expanded ${VAR} values", "AZURE_LOCATION")
			default:
				checked = true
				if p := locationProblem(v.Reveal()); p != "" {
					x.fail(envRef, sdk.Location{}, "format:AZURE_LOCATION", sdk.ConfidenceLikely,
						"AZURE_LOCATION is not a location name in the form azd stores: it has "+p, rec012, fix012)
				} else {
					envLoc = raw
				}
			}
		}
	}

	var deployed []string
	if in.ARM != nil {
		for _, res := range in.ARM.Resources {
			deployed = r.resource(x, res, deployed, &checked)
		}
	}

	slices.Sort(deployed)
	deployed = slices.Compact(deployed)
	if envLoc != "" && len(deployed) > 0 && !slices.Contains(deployed, envLoc) {
		x.fail(envRef, sdk.Location{}, "location-mismatch", sdk.ConfidenceLikely,
			fmt.Sprintf("AZURE_LOCATION is %s but no deployed template resource uses it; literal resource locations are %s", envLoc, strings.Join(deployed, ", ")),
			rec012, fix012)
	}

	if !checked && len(x.skips) == 0 && len(x.out) == 0 {
		x.skip(rules.ReasonUnresolvedValue("no-location-source"), "AZURE_LOCATION or a template location", "")
	}
	if checked {
		x.skip(rules.ReasonControlPlaneUnavailable, "Subscriptions_ListLocations result", "")
	}
	return x.done(), nil
}

// resource checks one ARM resource location and appends deployed literal locations to deployed.
func (rule012) resource(x *run, res model.Resource, deployed []string, checked *bool) []string {
	v := res.Get("location")
	switch v.State {
	case model.Unresolved:
		x.skip(rules.ReasonUnresolvedValue(v.Reason), "evaluated template expression", res.Name)
		return deployed
	case model.Absent:
		return deployed
	}
	loc, ok := v.String()
	if !ok {
		return deployed
	}
	*checked = true
	ref := sdk.ResourceRef{Kind: "arm-resource", Type: res.Type, Name: res.Name, Pointer: "/location"}
	where := sdk.Location{File: res.File, Pointer: res.Meta.Pointer}
	uncertain := res.Meta.MayNotDeploy || res.Meta.Confidence == "uncertain"
	if p := locationProblem(loc); p != "" {
		ev := fmt.Sprintf("location of %s %s is not a location name in the form azd stores: it has %s", res.Type, res.Name, p)
		key := "format:" + res.Type + ":" + res.Name
		if uncertain {
			x.uncertain(ref, where, key, ev+" (the resource is conditional or inside a module)", rec012, fix012)
		} else {
			x.fail(ref, where, key, sdk.ConfidenceLikely, ev, rec012, fix012)
		}
		return deployed
	}
	if !res.Meta.MayNotDeploy && loc != "global" {
		deployed = append(deployed, loc)
	}
	return deployed
}
