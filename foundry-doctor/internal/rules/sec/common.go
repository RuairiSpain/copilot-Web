package sec

import (
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Resource types read by the SEC rules. Every type is named in a verified catalogue entry.
const (
	typAccount     = "Microsoft.CognitiveServices/accounts"
	typProject     = "Microsoft.CognitiveServices/accounts/projects"
	typDeployment  = "Microsoft.CognitiveServices/accounts/deployments"
	typConnection  = "Microsoft.CognitiveServices/accounts/connections"
	typProjConn    = "Microsoft.CognitiveServices/accounts/projects/connections"
	typCapHost     = "Microsoft.CognitiveServices/accounts/capabilityHosts"
	typProjCapHost = "Microsoft.CognitiveServices/accounts/projects/capabilityHosts"
	typSearch      = "Microsoft.Search/searchServices"
	typCosmos      = "Microsoft.DocumentDB/databaseAccounts"
	typStorage     = "Microsoft.Storage/storageAccounts"
	typKeyVault    = "Microsoft.KeyVault/vaults"
)

// armGuard returns a skip result when the compiled ARM template is not available.
func armGuard(r rules.Rule, in *model.Input) ([]rules.Result, bool) {
	if in != nil && in.ARM != nil {
		return nil, true
	}
	if in != nil && in.Project.HasARM() {
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("compiled-arm-unavailable"),
			"the ARM template could not be compiled", "compiled ARM template", sdk.ResourceRef{})}, false
	}
	detail := "no compiled ARM template"
	if in != nil {
		detail = "iac: " + string(in.Project.IaC)
	}
	return []rules.Result{rules.SkipFor(r, rules.ReasonSyntheticInfrastructure, detail, "compiled ARM template", sdk.ResourceRef{})}, false
}

// liveSkip is the single, by-design skip for the live (control-plane) half of a rule.
func liveSkip(r rules.Rule) rules.Result {
	return rules.SkipFor(r, rules.ReasonControlPlaneUnavailable,
		"the deployed resource is not read in offline analysis; only the compiled template was checked",
		"Reader access to the deployed resource (ARM control plane)", sdk.ResourceRef{})
}

func refOf(r model.Resource) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "arm-resource", Type: r.Type, Name: r.Name, Pointer: r.Meta.Pointer}
}

// locOf locates a property of a resource: the entry file, and a JSON pointer into the compiled document.
func locOf(r model.Resource, prop string) sdk.Location {
	ptr := r.Meta.Pointer
	if ptr != "" && prop != "" {
		segs := strings.Split(strings.NewReplacer("[", ".", "]", "").Replace(prop), ".")
		for _, s := range segs {
			if s != "" {
				ptr += "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
			}
		}
	}
	return sdk.Location{File: r.File, Line: r.Location.Line, Column: r.Location.Column, Pointer: ptr}
}

// doubt says whether the facts about a set of resources are firm enough for a failure.
type doubt struct {
	uncertain bool
	why       string
	conf      sdk.Confidence
}

// assess derives doubt from the deployment flags of the resources a result depends on.
func assess(rs ...model.Resource) doubt {
	d := doubt{conf: sdk.ConfidenceCertain}
	for _, r := range rs {
		m := r.Meta
		if len(m.ModuleChain) > 0 || m.Confidence == string(sdk.ConfidenceUncertain) {
			d.conf = sdk.ConfidenceLikely
		}
		switch {
		case m.MultiInstance && !d.uncertain:
			d.uncertain, d.why = true, fmt.Sprintf("%s is looped, so the number and values of instances are not known", shortType(r))
		case m.MayNotDeploy && !d.uncertain:
			if m.Conditional && len(m.ModuleChain) == 0 && m.ConditionValue.State == model.Literal {
				if b, ok := m.ConditionValue.Bool(); ok && b {
					continue
				}
			}
			d.uncertain, d.why = true, fmt.Sprintf("%s is conditional, so it may not be deployed", shortType(r))
		}
	}
	if d.uncertain {
		d.conf = sdk.ConfidenceUncertain
	}
	return d
}

// notDeployed reports a resource whose own condition is the literal false.
func notDeployed(r model.Resource) bool {
	if !r.Meta.Conditional || r.Meta.ConditionValue.State != model.Literal {
		return false
	}
	b, ok := r.Meta.ConditionValue.Bool()
	return ok && !b
}

func shortType(r model.Resource) string { return r.Type }

// finding builds a failing or uncertain result according to d.
func finding(r rules.Rule, res model.Resource, d doubt, key, prop, evidence, rec, fix string) rules.Result {
	f := rules.NewFinding(r, refOf(res), evidence, rec)
	f.Location = locOf(res, prop)
	f.Key = key
	f.Fix = fix
	f.Confidence = d.conf
	if d.uncertain {
		f.Evidence += "; deployment is uncertain: " + d.why
		return rules.Uncertain(f)
	}
	return rules.Fail(f)
}

// skip builds a skipped result for a resource.
func skip(r rules.Rule, res model.Resource, reason, detail, missing string) rules.Result {
	return rules.SkipFor(r, reason, detail, missing, refOf(res))
}

// unresolvedSkip skips a check whose input is an unresolved template value.
func unresolvedSkip(r rules.Rule, res model.Resource, prop string, v model.Value) rules.Result {
	why := v.Reason
	if why == "" {
		why = "unknown"
	}
	return skip(r, res, rules.ReasonUnresolvedValue(why),
		fmt.Sprintf("%s depends on a value that is unknown at compile time (%s)", prop, why),
		"a literal value or a supplied parameter value for "+prop)
}

// notApplicable skips a resource that the rule does not cover.
func notApplicable(r rules.Rule, res model.Resource, detail string) rules.Result {
	return skip(r, res, rules.ReasonNotApplicable, detail, "")
}

// boolOf reads a bool property. state is "true", "false", "absent", "unresolved" or "invalid".
func boolOf(res model.Resource, prop string) (state string, v model.Value) {
	v = res.Get(prop)
	switch v.State {
	case model.Absent:
		return "absent", v
	case model.Unresolved:
		return "unresolved", v
	}
	b, ok := v.Bool()
	switch {
	case !ok:
		return "invalid", v
	case b:
		return "true", v
	}
	return "false", v
}

// invalidSkip skips a check whose literal has an unexpected type or value.
func invalidSkip(r rules.Rule, res model.Resource, prop string) rules.Result {
	return skip(r, res, rules.ReasonUnresolvedValue("unexpected-value"),
		prop+" has a literal of an unexpected type or value", "a boolean or documented enum value for "+prop)
}
