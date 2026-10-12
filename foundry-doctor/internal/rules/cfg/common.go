package cfg

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Hosts of the azure.ai.* extensions that the rules of this group read.
const (
	hostProject       = "azure.ai.project"
	hostAgent         = "azure.ai.agent"
	hostConnection    = "azure.ai.connection"
	hostToolbox       = "azure.ai.toolbox"
	hostRoutine       = "azure.ai.routine"
	hostLegacyFoundry = "microsoft.foundry"
)

// Named capabilities a skipped check reports.
const (
	capParsedYAML = "parsed azure.yaml"
	capRefFile    = "$ref file contents"
	capFileTree   = "project file tree"
)

// run collects the results of one Evaluate call.
type run struct {
	r     rules.Rule
	in    *model.Input
	out   []rules.Result
	skips map[skipKey][]string
}

type skipKey struct{ reason, missing string }

func newRun(r rules.Rule, in *model.Input) *run {
	return &run{r: r, in: in, skips: map[skipKey][]string{}}
}

// yamlMissing reports whether azure.yaml was not parsed.
func yamlMissing(in *model.Input) bool { return in == nil || in.AzureYAML == nil }

// skipNoYAML is the single result of a rule whose input azure.yaml is unavailable.
func skipNoYAML(r rules.Rule) []rules.Result {
	return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable, "azure.yaml was not parsed; see the engine diagnostics", capParsedYAML, sdk.ResourceRef{})}
}

func (x *run) file() string {
	if x.in != nil && x.in.AzureYAML != nil {
		return x.in.AzureYAML.File
	}
	return ""
}

func (x *run) loc(p model.Pos) sdk.Location {
	return sdk.Location{File: x.file(), Line: p.Line, Column: p.Column}
}

// finding adds a fail (or uncertain) result.
func (x *run) finding(status sdk.Status, res sdk.ResourceRef, loc sdk.Location, key string, conf sdk.Confidence, evidence, rec, fix string) {
	f := rules.NewFinding(x.r, res, evidence, rec)
	f.Location, f.Key, f.Fix, f.Confidence = loc, key, fix, conf
	if status == sdk.StatusUncertain {
		x.out = append(x.out, rules.Uncertain(f))
		return
	}
	x.out = append(x.out, rules.Fail(f))
}

func (x *run) fail(res sdk.ResourceRef, loc sdk.Location, key string, conf sdk.Confidence, evidence, rec, fix string) {
	x.finding(sdk.StatusFail, res, loc, key, conf, evidence, rec, fix)
}

func (x *run) uncertain(res sdk.ResourceRef, loc sdk.Location, key, evidence, rec, fix string) {
	x.finding(sdk.StatusUncertain, res, loc, key, sdk.ConfidenceUncertain, evidence, rec, fix)
}

// skip records a skipped check. Skips with the same reason and capability are merged into one result
// that lists the affected names.
func (x *run) skip(reason, missing, name string) {
	k := skipKey{reason, missing}
	if !slices.Contains(x.skips[k], name) {
		x.skips[k] = append(x.skips[k], name)
	}
}

// done returns the results in a deterministic order. With no finding and no skip it returns one pass.
func (x *run) done() []rules.Result {
	res := slices.Clone(x.out)
	keys := make([]skipKey, 0, len(x.skips))
	for k := range x.skips {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b skipKey) int {
		return strings.Compare(a.reason+"|"+a.missing, b.reason+"|"+b.missing)
	})
	for _, k := range keys {
		names := slices.Clone(x.skips[k])
		slices.Sort(names)
		detail := fmt.Sprintf("%d item(s)", len(names))
		shown := names
		if len(shown) > 10 {
			shown = shown[:10]
		}
		if len(shown) > 0 && shown[0] != "" {
			detail += ": " + strings.Join(shown, ", ")
		}
		res = append(res, rules.SkipFor(x.r, k.reason, detail, k.missing, sdk.ResourceRef{}))
	}
	if len(res) == 0 {
		return []rules.Result{rules.Pass()}
	}
	slices.SortStableFunc(res, func(a, b rules.Result) int { return strings.Compare(sortKey(a), sortKey(b)) })
	// A finding repeated for the same resource and key is reported once.
	out := make([]rules.Result, 0, len(res))
	for i, r := range res {
		if i > 0 && r.Finding != nil && sortKey(res[i-1]) == sortKey(r) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sortKey(r rules.Result) string {
	switch {
	case r.Finding != nil:
		f := r.Finding
		return "0|" + f.Key + "|" + f.Resource.Kind + "|" + f.Resource.Name + "|" + f.Resource.Pointer
	case r.Skip != nil:
		return "1|" + r.Skip.Reason + "|" + r.Skip.MissingCapability
	}
	return "2"
}

// ---- azure.yaml helpers ----

func svcRef(s model.Service) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "service", Type: s.Host, Name: s.Name}
}

func projectRef(ay *model.AzureYAML) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "project", Name: ay.Name}
}

func sortedServices(ay *model.AzureYAML) []model.Service {
	out := slices.Clone(ay.Services)
	slices.SortStableFunc(out, func(a, b model.Service) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// scalar returns the text of a scalar node.
func scalar(n *model.Node) (string, bool) {
	if n == nil || n.Kind != model.NodeScalar {
		return "", false
	}
	return n.Value, true
}

// field returns the scalar text at path below the service body, and the node.
func field(s model.Service, path ...string) (string, *model.Node) {
	n := s.Node.Lookup(path...)
	v, _ := scalar(n)
	return v, n
}

func hasField(s model.Service, path ...string) bool { return s.Node.Lookup(path...) != nil }

// hasRefFile reports whether the service body is (partly) held in a $ref file the reader does not follow.
func hasRefFile(s model.Service) bool { return hasField(s, "$ref") }

func kindOf(s model.Service) string { v, _ := field(s, "kind"); return v }

func blank(s string) bool { return strings.TrimSpace(s) == "" }

// hasRef reports whether s holds a ${...} reference, so its final value is known only after expansion.
func hasRef(s string) bool { return strings.Contains(s, "${") }

// seqItems returns the items of a sequence node.
func seqItems(n *model.Node) []*model.Node {
	if n == nil || n.Kind != model.NodeSequence {
		return nil
	}
	return n.Children
}

// nameOf reads an item that is either a scalar name or a mapping with a name key.
func nameOf(n *model.Node) (string, bool) {
	if v, ok := scalar(n); ok {
		return v, true
	}
	if n != nil && n.Kind == model.NodeMapping {
		return scalar(n.Lookup("name"))
	}
	return "", false
}

func posOf(n *model.Node, fallback model.Pos) model.Pos {
	if n == nil {
		return fallback
	}
	if n.KeyPos.Line > 0 {
		return n.KeyPos
	}
	if n.Pos.Line > 0 {
		return n.Pos
	}
	return fallback
}

func serviceByName(ay *model.AzureYAML) map[string]model.Service {
	m := make(map[string]model.Service, len(ay.Services))
	for _, s := range ay.Services {
		if _, dup := m[s.Name]; !dup {
			m[s.Name] = s
		}
	}
	return m
}

func usesName(s model.Service, name string) bool {
	for _, u := range s.Uses {
		if u.Name == name {
			return true
		}
	}
	return false
}

// usedProjects returns the azure.ai.project services the service lists in uses, sorted by name.
func usedProjects(by map[string]model.Service, s model.Service) []model.Service {
	var out []model.Service
	for _, u := range s.Uses {
		if p, ok := by[u.Name]; ok && p.Host == hostProject {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b model.Service) int { return strings.Compare(a.Name, b.Name) })
	return slices.CompactFunc(out, func(a, b model.Service) bool { return a.Name == b.Name })
}

// deployments returns the deployment names declared by the projects an agent uses. why is non-empty when
// the absence of a name cannot be proven offline.
func deployments(by map[string]model.Service, s model.Service) (names map[string]bool, why string) {
	names = map[string]bool{}
	projects := usedProjects(by, s)
	if len(projects) == 0 {
		return names, "no-project-in-uses"
	}
	for _, p := range projects {
		if v, _ := field(p, "endpoint"); !blank(v) {
			why = "existing-project-endpoint"
		}
		for _, it := range seqItems(p.Node.Lookup("deployments")) {
			if it.Kind == model.NodeMapping && it.Lookup("$ref") != nil {
				why = "deployments-ref-file"
				continue
			}
			n, ok := scalar(it.Lookup("name"))
			switch {
			case !ok:
			case hasRef(n):
				why = "deployment-name-reference"
			default:
				names[n] = true
			}
		}
	}
	return names, why
}

// reservedEnv reports whether an environment key uses a prefix the Foundry extensions own.
func reservedEnv(key string) bool {
	return strings.HasPrefix(key, "FOUNDRY_") || strings.HasPrefix(key, "AGENT_")
}
