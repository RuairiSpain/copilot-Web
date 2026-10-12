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

const (
	typePolicyAssignment = "Microsoft.Authorization/policyAssignments"
	// approvedModelsPolicy is the built-in "Foundry model deployments should only use approved models".
	approvedModelsPolicy = "aafe3651-cb78-4f68-9f81-e7e41509110f"
	// modelRouterName is the model name of a model router deployment. Its use as an identifier is
	// not verified by the catalogue sources; it only decides that the deployment is skipped.
	modelRouterName = "model-router"

	recommendation010 = "Keep one source of truth for approved models, mirror it in the policy assignment parameters allowedPublishers and allowedAssetIds, and remove any model from azure.yaml that the lists exclude. Assign the policy with Audit first and move to Deny after the compliance dashboard is clean."
	fix010            = "# profile excerpt (allow list) - mirror the same values in the policy assignment parameters\nmodels:\n  allow:\n    publishers: [\"OpenAI\"]\n  deny: []"
)

type ops010 struct{}

func (ops010) ID() string   { return "FND-OPS-010" }
func (ops010) Version() int { return 1 }

// entryParts splits a list entry format[/name[/version]].
func entryParts(e string) []string { return strings.Split(e, "/") }

// matches reports whether the list entry covers the deployed model. Comparison is exact and case-sensitive (ADR-007).
func matches(entry, format, name, version string) bool {
	p := entryParts(entry)
	switch len(p) {
	case 1:
		return p[0] == format
	case 2:
		return p[0] == format && p[1] == name
	case 3:
		return p[0] == format && p[1] == name && p[2] == version
	}
	return false
}

func anyMatch(list []string, format, name, version string) bool {
	return slices.ContainsFunc(list, func(e string) bool { return matches(e, format, name, version) })
}

func (r ops010) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	ref := projectRef(in)
	allow, deny := in.Policy.Models.Allow, in.Policy.Models.Deny
	if len(allow) == 0 && len(deny) == 0 {
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyModelsAllow),
			"neither policy.models.allow nor policy.models.deny is set in the profile", "policy.models.allow or policy.models.deny in the profile", ref)}, nil
	}
	var out []rules.Result
	fail := func(res sdk.ResourceRef, loc sdk.Location, key, evidence string, meta model.ResourceMeta) {
		f := rules.NewFinding(r, res, evidence, recommendation010)
		f.Key = key
		f.Location = loc
		f.Fix = fix010
		f.Confidence = lowered(sdk.ConfidenceLikely, meta)
		out = append(out, rules.Fail(f))
	}
	bad := false
	for _, e := range allow {
		if contains(deny, e) {
			fail(ref, sdk.Location{File: in.Project.AzureYAMLPath}, "allow-deny-conflict:"+e,
				fmt.Sprintf("The model entry %q is in both policy.models.allow and policy.models.deny.", e), model.ResourceMeta{})
			bad = true
		}
	}
	versioned := slices.ContainsFunc(append(slices.Clone(allow), deny...), func(e string) bool { return len(entryParts(e)) == 3 })
	for _, d := range allDeployments(in) {
		res, failed := r.checkDeployment(d, allow, deny, versioned, fail)
		out = append(out, res...)
		bad = bad || failed
	}
	if in.ARM == nil {
		s, _ := needARM(r, in, "policy assignment and ARM model deployments")
		out = append(out, s)
	} else {
		pol, polBad := r.checkPolicy(in, allow, fail)
		out = append(out, pol...)
		bad = bad || polBad
	}
	out = append(out, rules.SkipFor(r, rules.ReasonControlPlaneUnavailable,
		"the policy assignment and its compliance state are read from the control plane in phase 8", "live Azure access", ref))
	if !bad && !slices.ContainsFunc(out, func(x rules.Result) bool {
		return x.Status == sdk.StatusSkipped && x.Skip.Reason != rules.ReasonControlPlaneUnavailable
	}) {
		out = append(out, rules.Pass())
	}
	return out, nil
}

type failFunc func(res sdk.ResourceRef, loc sdk.Location, key, evidence string, meta model.ResourceMeta)

func (r ops010) checkDeployment(d deployment, allow, deny []string, versioned bool, fail failFunc) (skips []rules.Result, failed bool) {
	skip := func(reason, detail string) []rules.Result {
		return []rules.Result{rules.SkipFor(r, reason, detail, "resolved deployment model", d.Ref)}
	}
	format, fok := d.Format.String()
	name, nok := d.Model.String()
	if !fok || !nok {
		return skip(rules.ReasonUnresolvedValue("deployment-model"), "the model format or name of deployment "+d.label()+" is not known offline"), false
	}
	if strings.EqualFold(name, modelRouterName) {
		return skip(rules.ReasonNotApplicable, "model router deployments are evaluated from phase 8"), false
	}
	version, vok := d.Version.String()
	if versioned && !vok {
		return skip(rules.ReasonUnresolvedValue("deployment-model-version"), "a list entry names a model version but the version of deployment "+d.label()+" is not known offline"), false
	}
	id := format + "/" + name
	switch {
	case anyMatch(deny, format, name, version):
		fail(d.Ref, d.Loc, "denied:"+d.label(), fmt.Sprintf("Deployment %q uses model %s, which matches policy.models.deny.", d.label(), id), d.Meta)
		return nil, true
	case len(allow) > 0 && !anyMatch(allow, format, name, version):
		fail(d.Ref, d.Loc, "not-allowed:"+d.label(), fmt.Sprintf("Deployment %q uses model %s, which no policy.models.allow entry matches.", d.label(), id), d.Meta)
		return nil, true
	}
	return nil, false
}

// policyAssignments returns the approved-models assignments of the template.
func policyAssignments(t *model.ARMTemplate) []model.Resource {
	var out []model.Resource
	for _, res := range t.ResourcesOfType(typePolicyAssignment) {
		id := res.Get("properties.policyDefinitionId")
		s, ok := id.String()
		if !ok {
			if raw, isStr := bodyString(res, "policyDefinitionId"); id.State == model.Unresolved && isStr {
				s = raw
			}
		}
		if strings.Contains(strings.ToLower(s), approvedModelsPolicy) {
			out = append(out, res)
		}
	}
	return out
}

func bodyString(res model.Resource, key string) (string, bool) {
	props, _ := res.Body["properties"].(map[string]any)
	s, ok := props[key].(string)
	return s, ok
}

// stringList reads a literal array of strings.
func stringList(v model.Value) ([]string, bool) {
	if v.State == model.Absent {
		return nil, true
	}
	arr, ok := v.Lit.([]any)
	if !ok || v.State != model.Literal {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok || model.IsARMExpression(s) {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (r ops010) checkPolicy(in *model.Input, allow []string, fail failFunc) (out []rules.Result, bad bool) {
	assigns := policyAssignments(in.ARM)
	if len(assigns) == 0 {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"no assignment of the approved-models policy is declared in the repository", "", projectRef(in))}, false
	}
	var profilePublishers []string
	for _, e := range allow {
		if len(entryParts(e)) == 1 {
			profilePublishers = append(profilePublishers, e)
		}
	}
	for _, a := range assigns {
		pubs, ok1 := stringList(a.Get("properties.parameters.allowedPublishers.value"))
		ids, ok2 := stringList(a.Get("properties.parameters.allowedAssetIds.value"))
		if !ok1 || !ok2 {
			out = append(out, rules.SkipFor(r, rules.ReasonUnresolvedValue("policy-parameters"),
				"the allowedPublishers or allowedAssetIds parameter of the policy assignment is not known offline", "resolved policy parameters", armRef(a)))
			continue
		}
		if eff, ok := a.Get("properties.parameters.effect.value").String(); ok && eff == "Deny" && len(pubs) == 0 && len(ids) == 0 {
			fail(armRef(a), armLoc(a), "policy-deny-empty",
				"The approved-models policy assignment has effect Deny while allowedPublishers and allowedAssetIds are both empty, so it blocks every model.", a.Meta)
			bad = true
		}
		if len(profilePublishers) == 0 {
			continue
		}
		for _, p := range profilePublishers {
			if contains(pubs, p) {
				continue
			}
			if i := slices.IndexFunc(pubs, func(x string) bool { return strings.EqualFold(x, p) }); i >= 0 {
				fail(armRef(a), armLoc(a), "publisher-case:"+p,
					fmt.Sprintf("Profile allow entry %q differs only by case from policy publisher %q; the policy compares case-sensitively.", p, pubs[i]), a.Meta)
			} else {
				fail(armRef(a), armLoc(a), "profile-not-in-policy:"+p,
					fmt.Sprintf("Profile allow entry %q is not in the policy allowedPublishers parameter.", p), a.Meta)
			}
			bad = true
		}
		for _, p := range pubs {
			if contains(profilePublishers, p) || slices.ContainsFunc(profilePublishers, func(x string) bool { return strings.EqualFold(x, p) }) {
				continue
			}
			fail(armRef(a), armLoc(a), "policy-not-in-profile:"+p,
				fmt.Sprintf("Policy publisher %q is not in policy.models.allow.", p), a.Meta)
			bad = true
		}
	}
	return out, bad
}
