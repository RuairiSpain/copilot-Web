package run

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FND-RUN-008 compares an allow-list of security- and availability-relevant
// properties of the compiled ARM template with the deployed control-plane
// state the runtime client already reads. The allow-list is a product opinion;
// every property below is a documented property of the 2026-09-01 Cognitive
// Services spec (publicNetworkAccess, identity.type, sku.name).
//
// Not compared yet, because the runtime client does not expose the deployed
// value (a gap in internal/azure, not a design decision): account
// disableLocalAuth and networkAcls.defaultAction, account identity type,
// deployment model version and sku.capacity. sku.capacity is also ambiguous:
// the client folds properties.currentCapacity and sku.capacity together
// (UNVERIFIED mapping), so a comparison could report false drift.

const (
	typeAccount    = "Microsoft.CognitiveServices/accounts"
	typeProject    = "Microsoft.CognitiveServices/accounts/projects"
	typeDeployment = "Microsoft.CognitiveServices/accounts/deployments"

	// maxOneSidedNotes bounds the per-resource "only in one side" notes.
	maxOneSidedNotes = 10
)

// templateRes is one compiled-template resource of an inspected type.
type templateRes struct {
	res      sdk.ARMResource
	segments []armexpr.Segment
	literal  bool
}

func (t templateRes) display() string { return armexpr.StripScope(t.res.Name) }

// presence classifies how sure the doctor is that the template deploys r.
// "never" = literal false condition; "maybe" = a conditional or looped resource.
func presence(r sdk.ARMResource) string {
	if r.Conditional {
		switch strings.ToLower(strings.TrimSpace(strings.Trim(r.Condition, "[]"))) {
		case "false":
			return "never"
		case "true":
		default:
			return "maybe"
		}
	}
	if r.Looped {
		return "maybe"
	}
	return "yes"
}

func templateOf(in *sdk.Input, typ string) []templateRes {
	var out []templateRes
	for _, r := range in.ARM.Resources() {
		if !strings.EqualFold(r.Type, typ) {
			continue
		}
		ref := armexpr.ParseName(typ, armexpr.StripScope(r.Name))
		out = append(out, templateRes{res: r, segments: ref.Segments, literal: ref.Literal()})
	}
	return out
}

// driftProp compares one property. tv is the template value; ok is false when
// the template does not set the property (the service default applies, so
// nothing is compared).
type driftProp struct {
	path     string
	template any
	set      bool
	deployed string
	// normalize prepares both sides for comparison (default: trim + lower-case).
	normalize func(string) string
}

func lowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// identitySet reduces an ARM identity type ("SystemAssigned, UserAssigned") to
// a sorted, lower-cased set.
func identitySet(s string) string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = lowerTrim(p); p != "" {
			parts = append(parts, p)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// scalarText renders a template scalar. Non-scalars (objects, arrays, null)
// are reported as not comparable, and a secret-shaped value is never produced:
// only strings, booleans and numbers reach evidence, and only for the
// allow-listed properties above.
func scalarText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return fmt.Sprintf("%t", x), true
	case float64:
		return fmt.Sprintf("%g", x), true
	case int:
		return fmt.Sprintf("%d", x), true
	}
	return "", false
}

func (e *env) run008(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, false); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if in == nil || in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: compiled ARM model unavailable, so there is no template to compare the deployed state with"}}, nil
	}
	if e.d.FoundryMgmt == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": deployed control-plane reads (capability Foundry management metadata) are not available"}}, nil
	}
	t := e.d.Target
	accounts := templateOf(in, typeAccount)
	projects := templateOf(in, typeProject)
	deployments := templateOf(in, typeDeployment)
	if len(accounts)+len(projects)+len(deployments) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipNotApplicable + ": the compiled template declares no account, project or model deployment to compare"}}, nil
	}
	corr := e.correlator(in)
	a := &acc{}
	oneSided := 0
	oneSidedNote := func(s string) {
		oneSided++
		if oneSided <= maxOneSidedNotes {
			a.note(s)
		}
	}

	// compare reports one drifted property or a note and returns nothing.
	compare := func(rt, name, id string, tr templateRes, props []driftProp) {
		rc := corr.Resource(rt, name, id)
		if tr.res.Location.File != "" {
			rc.Source = tr.res.Location
		}
		switch presence(tr.res) {
		case "maybe":
			a.note(rt + " " + name + " is conditional or looped in the template; whether it is deployed is not known, so it was not compared")
			return
		case "never":
			oneSidedNote(rt + " " + name + " is deployed but the template never deploys it (literal false condition)")
			return
		}
		for _, p := range props {
			if !p.set {
				continue // the template leaves it to the service default
			}
			tv, ok := scalarText(p.template)
			switch {
			case !ok:
				a.note(rt + " " + name + " " + p.path + " is not a scalar in the template and was not compared")
				continue
			case armexpr.IsExpression(tv) || strings.Contains(tv, "${"):
				a.note(rt + " " + name + " " + p.path + " is an unresolved expression in the template and was not compared")
				continue
			case strings.TrimSpace(p.deployed) == "":
				a.note(rt + " " + name + " " + p.path + " is not reported by the deployed resource and was not compared")
				continue
			}
			norm := p.normalize
			if norm == nil {
				norm = lowerTrim
			}
			if norm(tv) == norm(p.deployed) {
				continue
			}
			a.addKeyed(rc, "drift:"+p.path,
				fmt.Sprintf("%s drifted on %s %s: the compiled template sets %s, the deployed resource reports %s", p.path, rt, name, tv, p.deployed), false)
		}
	}

	// Account.
	for _, tr := range accounts {
		switch {
		case !tr.literal || len(tr.segments) != 1:
			a.note("template account " + tr.display() + " has a name that is not a literal, so it cannot be matched to the deployed account")
		case !strings.EqualFold(tr.segments[0].Text, t.Account):
			oneSidedNote("template account " + tr.display() + " is not the target account " + t.Account + " and was not compared")
		default:
			net, err := e.accountNetwork(ctx)
			if err != nil {
				return sdk.Result{}, err
			}
			pna, set := tr.res.Properties["publicNetworkAccess"]
			compare(typeAccount, t.Account, accountID(t), tr, []driftProp{
				{path: "publicNetworkAccess", template: pna, set: set, deployed: net.PublicNetworkAccess},
			})
		}
	}

	// Project (identity type).
	for _, tr := range projects {
		switch {
		case t.Project == "":
			oneSidedNote("template project " + tr.display() + " was not compared because no target project is set (use --project)")
		case !tr.literal || len(tr.segments) != 2:
			a.note("template project " + tr.display() + " has a name that is not a literal, so it cannot be matched to the deployed project")
		case !strings.EqualFold(tr.segments[0].Text, t.Account) || !strings.EqualFold(tr.segments[1].Text, t.Project):
			oneSidedNote("template project " + tr.display() + " is not the target project " + t.Account + "/" + t.Project + " and was not compared")
		default:
			proj, err := e.projectInfo(ctx)
			if err != nil {
				return sdk.Result{}, err
			}
			var idv any
			var idSet bool
			if tr.res.Identity != nil {
				idv, idSet = tr.res.Identity["type"]
			}
			compare(typeProject, t.Project, proj.ID, tr, []driftProp{
				{path: "identity.type", template: idv, set: idSet, deployed: proj.IdentityType, normalize: identitySet},
			})
		}
	}

	// Deployments (sku.name).
	if len(deployments) > 0 {
		var deployed []azure.AccountDeployment
		fetched := false
		seen := map[string]bool{}
		for _, tr := range deployments {
			switch {
			case !tr.literal || len(tr.segments) != 2:
				a.note("template deployment " + tr.display() + " has a name that is not a literal, so it cannot be matched to a deployed model deployment")
				continue
			case !strings.EqualFold(tr.segments[0].Text, t.Account):
				oneSidedNote("template deployment " + tr.display() + " belongs to another account than the target " + t.Account + " and was not compared")
				continue
			}
			if !fetched {
				var err error
				if deployed, err = e.deployments(ctx); err != nil {
					return sdk.Result{}, err
				}
				fetched = true
			}
			want := tr.segments[1].Text
			var got *azure.AccountDeployment
			for i := range deployed {
				if strings.EqualFold(deployed[i].Name, want) {
					got = &deployed[i]
				}
			}
			if got == nil {
				oneSidedNote("template deployment " + tr.display() + " is not deployed on the target account")
				continue
			}
			seen[strings.ToLower(got.Name)] = true
			var skuv any
			var skuSet bool
			if tr.res.SKU != nil {
				skuv, skuSet = tr.res.SKU["name"]
			}
			compare(typeDeployment, got.Name, accountID(t)+"/deployments/"+got.Name, tr, []driftProp{
				{path: "sku.name", template: skuv, set: skuSet, deployed: got.SKUName},
			})
		}
		if fetched {
			names := make([]string, 0)
			for _, d := range deployed {
				if !seen[strings.ToLower(d.Name)] {
					names = append(names, d.Name)
				}
			}
			sort.Strings(names)
			for _, n := range names {
				oneSidedNote("deployed model deployment " + n + " is not declared in the compiled template")
			}
		}
	}
	if oneSided > maxOneSidedNotes {
		a.note(fmt.Sprintf("%d further resources exist on one side only (template or deployment)", oneSided-maxOneSidedNotes))
	}
	return a.result(), nil
}
