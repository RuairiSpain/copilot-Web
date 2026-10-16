// Package ops implements the catalogue OPS rules conservatively from repository,
// azure.yaml and compiled ARM evidence. Any unresolved or unsupported input
// yields skip/uncertain, never a silent pass.
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type Providers struct {
	Root string
}

func RegisterWith(p Providers) []sdk.Rule {
	return []sdk.Rule{
		rule{id: "FND-OPS-001", p: p, fn: eval001},
		rule{id: "FND-OPS-002", p: p, fn: eval002},
		rule{id: "FND-OPS-003", p: p, fn: eval003},
		rule{id: "FND-OPS-004", p: p, fn: eval004},
		rule{id: "FND-OPS-005", p: p, fn: eval005},
		rule{id: "FND-OPS-006", p: p, fn: eval006},
		rule{id: "FND-OPS-007", p: p, fn: eval007},
		rule{id: "FND-OPS-008", p: p, fn: eval008},
		rule{id: "FND-OPS-009", p: p, fn: eval009},
		rule{id: "FND-OPS-010", p: p, fn: eval010},
	}
}

func Register() []sdk.Rule { return RegisterWith(Providers{}) }

type rule struct {
	id string
	p  Providers
	fn func(*sdk.Input, Providers) sdk.Result
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if err := ctx.Err(); err != nil {
		return sdk.Result{}, fmt.Errorf("%s: %w", r.id, err)
	}
	if in == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}, nil
	}
	return r.fn(in, r.p), nil
}

type outcome struct {
	findings []sdk.Finding
	skips    []string
	notes    []string
}

func (o *outcome) add(r sdk.ARMResource, evidence string) {
	o.findings = append(o.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
		Location: r.Location,
		Evidence: armexpr.StripScope(evidence),
	})
}

// addKey adds a finding with a stable Key that separates several findings of
// one rule on one resource (a sub-check or tag name).
func (o *outcome) addKey(r sdk.ARMResource, key, evidence string) {
	o.add(r, evidence)
	o.findings[len(o.findings)-1].Key = key
}

func (o *outcome) result() sdk.Result {
	if len(o.findings) > 0 {
		slices.SortFunc(o.findings, func(a, b sdk.Finding) int {
			if c := strings.Compare(a.Resource.Type, b.Resource.Type); c != 0 {
				return c
			}
			if c := strings.Compare(a.Resource.Name, b.Resource.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Evidence, b.Evidence)
		})
		// sdk.Result cannot carry skips next to findings, so the first finding
		// names the resources that were not evaluated (review M3).
		var unevaluated []string
		unevaluated = append(unevaluated, o.skips...)
		for _, n := range o.notes {
			unevaluated = append(unevaluated, "uncertain: "+n)
		}
		if len(unevaluated) > 0 {
			slices.Sort(unevaluated)
			unevaluated = unique(unevaluated)
			o.findings[0].Evidence += armexpr.StripScope(fmt.Sprintf(" [note: %d resource check(s) not evaluated by this rule: %s]", len(unevaluated), strings.Join(unevaluated, "; ")))
		}
		return sdk.Result{Findings: o.findings}
	}
	if len(o.skips) > 0 {
		slices.Sort(o.skips)
		return sdk.Result{Skipped: &sdk.Skip{Reason: armexpr.StripScope(strings.Join(unique(o.skips), "; "))}}
	}
	if len(o.notes) > 0 {
		slices.Sort(o.notes)
		return sdk.Result{Skipped: &sdk.Skip{Reason: armexpr.StripScope("uncertain: " + strings.Join(unique(o.notes), "; "))}}
	}
	return sdk.Result{}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func profileTier(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "dev", "foundry-dev":
		return "dev"
	case "test", "foundry-test":
		return "test"
	case "prod", "foundry-prod":
		return "prod"
	}
	return ""
}

func docRoot(in *sdk.Input) *yaml.Node {
	type withRoot interface{ Root() *yaml.Node }
	if in == nil || in.AzureYAML == nil {
		return nil
	}
	d, ok := in.AzureYAML.(withRoot)
	if !ok {
		return nil
	}
	return d.Root()
}

func yChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func yPairs(n *yaml.Node) [][2]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([][2]*yaml.Node, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

func yScalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

func policyStrings(in *sdk.Input, key string) []string {
	if in == nil || in.Policy == nil {
		return nil
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return nil
	}
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func policyInt(in *sdk.Input, key string) (int, bool) {
	if in == nil || in.Policy == nil {
		return 0, false
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func unresolvedString(s string) bool { return isExpr(s) }

func foundryAccounts(v *view, o *outcome) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range v.of(o, "Microsoft.CognitiveServices/accounts") {
		if strings.EqualFold(r.Kind, "AIServices") || strings.EqualFold(r.Kind, "OpenAI") {
			out = append(out, r)
		}
	}
	return out
}

func childOf(child, parent string) bool {
	c := strings.ToLower(strings.TrimSpace(child))
	p := strings.ToLower(strings.TrimSpace(parent))
	return strings.HasPrefix(c, p+"/")
}

func refMatches(ref, name string) bool {
	if ref == "" || name == "" {
		return false
	}
	r, n := strings.ToLower(ref), strings.ToLower(name)
	return r == n || strings.HasSuffix(r, "/"+n) || strings.Contains(r, "'"+n+"'")
}

func getString(m map[string]any, path ...string) (string, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = mm[p]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

func getBool(m map[string]any, path ...string) (bool, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		cur, ok = mm[p]
		if !ok {
			return false, false
		}
	}
	b, ok := cur.(bool)
	return b, ok
}

func getArray(m map[string]any, path ...string) []any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[p]
		if !ok {
			return nil
		}
	}
	a, _ := cur.([]any)
	return a
}

// supportedLogs lists the log categories per resource type from the Azure Monitor
// supported-logs reference, for example
// https://learn.microsoft.com/azure/azure-monitor/reference/supported-logs/microsoft-cognitiveservices-accounts-logs
// (companion pages exist per resource type; see the FND-OPS-001 sources, verified
// 2026-10-04). Categories are lower-cased for comparison.
var supportedLogs = map[string]map[string]bool{
	"Microsoft.CognitiveServices/accounts":           {"audit": true, "requestresponse": true, "trace": true, "azureopenairequestusage": true, "managednetworkevent": true},
	"Microsoft.CognitiveServices/accounts/projects":  {"audit": true, "trace": true},
	"Microsoft.Search/searchServices":                {"operationlogs": true},
	"Microsoft.ApiManagement/service":                {"gatewaylogs": true, "gatewayllmlogs": true, "gatewaymcplogs": true, "websocketconnectionlogs": true, "developerportalauditlogs": true},
	"Microsoft.Storage/storageAccounts/blobServices": {"storageread": true, "storagewrite": true, "storagedelete": true},
	"Microsoft.DocumentDB/databaseAccounts":          {"dataplanerequests": true, "controlplanerequests": true, "queryruntimestatistics": true, "partitionkeystatistics": true, "partitionkeyruconsumption": true},
}

type match int

const (
	matchNo match = iota
	matchYes
	matchUnknown
)

// scopeMatch decides whether diagnostic setting ds applies to resource r by
// comparing names. When names are ARM expressions it compares the normalised
// expression text of the last resourceId argument; any comparison that cannot
// be proven either way is matchUnknown, never matchNo.
func scopeMatch(ds, r sdk.ARMResource) match {
	if strings.TrimSpace(ds.Scope) == "" {
		return childMatch(ds.Name, r.Name)
	}
	if !isExpr(ds.Scope) {
		if isExpr(r.Name) {
			return matchUnknown
		}
		if refMatches(ds.Scope, r.Name) {
			return matchYes
		}
		return matchNo
	}
	fn, args, ok := splitCall(exprBody(ds.Scope))
	if !ok || !strings.EqualFold(fn, "resourceid") {
		return matchUnknown
	}
	typeIdx := -1
	for i, a := range args {
		if q, ok := quoted(a); ok && strings.Contains(q, "/") && strings.Contains(strings.SplitN(q, "/", 2)[0], ".") {
			typeIdx = i
			break
		}
	}
	if typeIdx < 0 {
		return matchUnknown
	}
	typ, _ := quoted(args[typeIdx])
	if !strings.EqualFold(typ, r.Type) {
		return matchNo
	}
	names := args[typeIdx+1:]
	if len(names) != strings.Count(r.Type, "/") {
		return matchUnknown
	}
	if !isExpr(r.Name) {
		segs := strings.Split(r.Name, "/")
		if len(segs) != len(names) {
			return matchUnknown
		}
		result := matchYes
		for i, a := range names {
			q, ok := quoted(a)
			switch {
			case !ok:
				result = matchUnknown
			case !strings.EqualFold(q, segs[i]):
				return matchNo
			}
		}
		return result
	}
	if nameExprEquals(r.Name, names) {
		return matchYes
	}
	return matchUnknown
}

// nameExprEquals compares a resource-name expression with the name arguments of a
// resourceId call: a single argument must have identical normalised text; several
// arguments must equal the arguments of format('{0}/{1}', ...).
func nameExprEquals(name string, args []string) bool {
	if len(args) == 1 {
		return normExpr(name) == normExpr(args[0])
	}
	fn, fargs, ok := splitCall(exprBody(name))
	if !ok || !strings.EqualFold(fn, "format") || len(fargs) != len(args)+1 {
		return false
	}
	if q, ok := quoted(fargs[0]); !ok || strings.Count(q, "/") != len(args)-1 {
		return false
	}
	for i, a := range args {
		if normExpr(a) != normExpr(fargs[i+1]) {
			return false
		}
	}
	return true
}

// childMatch handles a diagnostic setting declared as a child ("<parent>/<name>").
func childMatch(dsName, rName string) match {
	if !isExpr(dsName) && !isExpr(rName) {
		if childOf(dsName, rName) {
			return matchYes
		}
		return matchNo
	}
	if isExpr(dsName) && isExpr(rName) {
		if fn, fargs, ok := splitCall(exprBody(dsName)); ok && strings.EqualFold(fn, "format") && len(fargs) > 1 {
			for _, a := range fargs[1:] {
				if normExpr(a) == normExpr(rName) {
					return matchYes
				}
			}
		}
	}
	return matchUnknown
}

// isWorkspaceRef reports whether an expression is a resourceId() call for a
// Microsoft.OperationalInsights/workspaces resource, which is the documented
// workspaceId destination shape.
func isWorkspaceRef(ws string) bool {
	fn, args, ok := splitCall(exprBody(ws))
	if !ok || !strings.EqualFold(fn, "resourceid") {
		return false
	}
	for _, a := range args {
		if q, ok := quoted(a); ok && strings.EqualFold(q, "Microsoft.OperationalInsights/workspaces") {
			return true
		}
	}
	return false
}

func eval001(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	if managed := policyStrings(in, "managedByAzurePolicy"); slices.Contains(managed, "diagnostic-settings") {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "policy-managed"}}
	}
	o := &outcome{}
	v := newView(in)
	targets := v.of(o,
		"Microsoft.CognitiveServices/accounts",
		"Microsoft.CognitiveServices/accounts/projects",
		"Microsoft.Search/searchServices",
		"Microsoft.ApiManagement/service",
		"Microsoft.Storage/storageAccounts/blobServices",
		"Microsoft.DocumentDB/databaseAccounts",
	)
	const dsType = "Microsoft.Insights/diagnosticSettings"
	settings := v.of(o, dsType)
	uncertainSettings := v.hasUncertain(dsType)
	for _, r := range targets {
		matched, unknown, good := 0, 0, 0
		var problems []string
		for _, ds := range settings {
			switch scopeMatch(ds, r) {
			case matchNo:
				continue
			case matchUnknown:
				unknown++
				continue
			}
			matched++
			ws, _ := getString(ds.Properties, "workspaceId")
			if ws == "" {
				problems = append(problems, "diagnostic setting has no Log Analytics workspace destination")
				continue
			}
			if unresolvedString(ws) && !isWorkspaceRef(ws) {
				o.skips = append(o.skips, r.Name+": workspaceId is unresolved")
				good++
				continue
			}
			if s, ok := ds.Properties["logs"].(string); ok && isExpr(s) {
				o.skips = append(o.skips, r.Name+": logs is unresolved")
				good++
				continue
			}
			logs := getArray(ds.Properties, "logs")
			if len(logs) == 0 {
				problems = append(problems, "diagnostic setting has metrics only")
				continue
			}
			okLog, logUnresolved := false, false
			var dsProblems []string
			for _, raw := range logs {
				m, _ := raw.(map[string]any)
				if s, ok := m["enabled"].(string); ok && isExpr(s) {
					logUnresolved = true
					continue
				}
				enabled, _ := m["enabled"].(bool)
				if !enabled {
					continue
				}
				cg, _ := m["categoryGroup"].(string)
				cat, _ := m["category"].(string)
				switch {
				case isExpr(cg) || isExpr(cat):
					logUnresolved = true
				case strings.EqualFold(cg, "allLogs") || strings.EqualFold(cg, "audit"):
					okLog = true
				case supportedLogs[r.Type][strings.ToLower(cat)]:
					okLog = true
				case cat != "":
					dsProblems = append(dsProblems, "diagnostic setting uses unsupported log category "+cat)
				}
			}
			switch {
			case logUnresolved:
				o.skips = append(o.skips, r.Name+": log category or enabled flag is unresolved")
				good++
			case !okLog:
				problems = append(problems, "diagnostic setting has no enabled supported logs")
			default:
				good++
			}
			problems = append(problems, dsProblems...)
		}
		switch {
		case matched == 0 && (unknown > 0 || uncertainSettings):
			o.notes = append(o.notes, r.Name+": diagnostic setting scope cannot be correlated with the resource")
		case matched == 0:
			o.add(r, "resource has no diagnostic setting to Log Analytics")
		case good == 0 && (unknown > 0 || uncertainSettings):
			o.notes = append(o.notes, r.Name+": an uncorrelated diagnostic setting may satisfy the rule")
		default:
			for _, p := range problems {
				o.add(r, p)
			}
			if matched > 5 {
				o.add(r, "resource has more than five diagnostic settings")
			}
		}
	}
	return o.result()
}

func eval002(_ *sdk.Input, _ Providers) sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: "no-explicit-observability-or-server-tracing-requirement"}}
}

func eval003(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	o := &outcome{}
	v := newView(in)
	accounts := foundryAccounts(v, o)
	if len(accounts) == 0 {
		return o.result()
	}
	hasServiceHealth := false
	hasMetrics := false
	alertTypes := []string{"Microsoft.Insights/activityLogAlerts", "Microsoft.Insights/metricAlerts", "Microsoft.Insights/scheduledQueryRules"}
	for _, r := range v.of(o, alertTypes...) {
		b, _ := json.Marshal(r.Properties)
		body := strings.ToLower(string(b))
		switch r.Type {
		case "Microsoft.Insights/activityLogAlerts":
			if strings.Contains(body, "servicehealth") {
				hasServiceHealth = true
			}
		case "Microsoft.Insights/metricAlerts", "Microsoft.Insights/scheduledQueryRules":
			if (strings.Contains(body, "modelavailabilityrate") || strings.Contains(body, "modelrequests") ||
				strings.Contains(body, "timetoresponse") || strings.Contains(body, "normalizedtimebetweentokens")) &&
				(strings.Contains(body, "actiongroupid") || strings.Contains(body, "actiongroups")) {
				hasMetrics = true
			}
		}
	}
	if !hasServiceHealth && !v.hasUncertain("Microsoft.Insights/activityLogAlerts") {
		o.addKey(accounts[0], "serviceHealthAlert", "no Service Health alert definition was found")
	}
	if !hasMetrics && !v.hasUncertain("Microsoft.Insights/metricAlerts", "Microsoft.Insights/scheduledQueryRules") {
		o.addKey(accounts[0], "modelMetricAlert", "no Foundry model metric alert or scheduled query rule with an action group was found")
	}
	return o.result()
}

// tagRequirement is one entry of the tags.required policy key.
type tagRequirement struct {
	Name   string
	Format string
}

// tagRequirements reads tags.required. The effective policy stores
// []config.TagRequirement; maps and anonymous structs are accepted for other
// Policy implementations.
func tagRequirements(v any) []tagRequirement {
	var reqs []tagRequirement
	switch xs := v.(type) {
	case []config.TagRequirement:
		for _, x := range xs {
			reqs = append(reqs, tagRequirement{Name: x.Name, Format: x.Format})
		}
	case []any:
		for _, x := range xs {
			switch t := x.(type) {
			case config.TagRequirement:
				reqs = append(reqs, tagRequirement{Name: t.Name, Format: t.Format})
			case map[string]any:
				name, format := firstString(t, "Name", "name"), firstString(t, "Format", "format")
				reqs = append(reqs, tagRequirement{Name: name, Format: format})
			}
		}
	case []struct{ Name, Format string }:
		for _, x := range xs {
			reqs = append(reqs, tagRequirement{Name: x.Name, Format: x.Format})
		}
	}
	out := reqs[:0]
	for _, r := range reqs {
		if strings.TrimSpace(r.Name) != "" {
			out = append(out, r)
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}

// tagExpressionReporter is implemented by an ARM model that can tell a
// resource whose tags property is an ARM expression (tags: tags) from one that
// declares no tags. The armmodel keeps only object-valued tags, so without this
// the two are indistinguishable and an empty Tags map is reported as uncertain.
type tagExpressionReporter interface {
	TagsAreExpression(sdk.ARMResource) bool
}

func eval004(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	reqsAny, ok := func() (any, bool) {
		if in.Policy == nil {
			return nil, false
		}
		return in.Policy.Get("tags.required")
	}()
	if !ok {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("tags.required")}}
	}
	reqs := tagRequirements(reqsAny)
	if len(reqs) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("tags.required")}}
	}
	o := &outcome{}
	scopeTypes := policyStrings(in, "tags.resourceTypes")
	scope := map[string]bool{}
	for _, s := range scopeTypes {
		scope[strings.ToLower(s)] = true
	}
	if len(scope) == 0 {
		for _, s := range []string{"Microsoft.CognitiveServices/accounts", "Microsoft.CognitiveServices/accounts/projects", "Microsoft.CognitiveServices/accounts/deployments", "Microsoft.Search/searchServices", "Microsoft.ApiManagement/service", "Microsoft.Storage/storageAccounts", "Microsoft.DocumentDB/databaseAccounts", "Microsoft.Insights/components", "Microsoft.OperationalInsights/workspaces"} {
			scope[strings.ToLower(s)] = true
		}
	}
	scopeList := make([]string, 0, len(scope))
	for s := range scope {
		scopeList = append(scopeList, s)
	}
	slices.Sort(scopeList)
	reporter, canTell := in.ARM.(tagExpressionReporter)
	for _, r := range newView(in).of(o, scopeList...) {
		if len(r.Tags) == 0 {
			switch {
			case !canTell:
				o.notes = append(o.notes, r.Name+": tags are absent or expression-valued and the ARM model does not distinguish the two")
			case reporter.TagsAreExpression(r):
				o.notes = append(o.notes, r.Name+": tags is an ARM expression that the template cannot resolve")
			default:
				for _, req := range reqs {
					o.addKey(r, "missing:"+strings.ToLower(req.Name), "resource has no tags; required tag "+req.Name+" is missing")
				}
			}
			continue
		}
		lowered := map[string]string{}
		for k, v := range r.Tags {
			lowered[strings.ToLower(k)] = fmt.Sprint(v)
			if len(k) > 512 || (strings.EqualFold(r.Type, "Microsoft.Storage/storageAccounts") && len(k) > 128) {
				o.addKey(r, "tagNameLimit:"+strings.ToLower(k), "tag name exceeds documented limit")
			}
			if len(fmt.Sprint(v)) > 256 {
				o.addKey(r, "tagValueLimit:"+strings.ToLower(k), "tag value exceeds documented limit")
			}
		}
		if len(lowered) > 50 {
			o.addKey(r, "tagCount", "resource has more than fifty tags")
		}
		for _, req := range reqs {
			val, ok := lowered[strings.ToLower(req.Name)]
			if !ok {
				o.addKey(r, "missing:"+strings.ToLower(req.Name), "missing required tag "+req.Name)
				continue
			}
			if isExpr(val) {
				o.notes = append(o.notes, r.Name+": tag "+req.Name+" value is an ARM expression")
				continue
			}
			if req.Format != "" {
				if re, err := regexp.Compile(req.Format); err == nil && !re.MatchString(val) {
					o.addKey(r, "format:"+strings.ToLower(req.Name), "tag "+req.Name+" value does not match configured format")
				}
			}
		}
	}
	return o.result()
}

var (
	reStorage        = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	reSearch         = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$`)
	reCosmos         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,43}$`)
	reKV             = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,22}[A-Za-z0-9]$`)
	reAPIM           = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,49}$`)
	reACR            = regexp.MustCompile(`^[A-Za-z0-9]{5,50}$`)
	reAppi           = regexp.MustCompile(`^[^%&\\?/][^%&\\?/]{0,258}[^ .]$|^[^%&\\?/]{1}$`)
	reCA             = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	reFoundryProject = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{1,63}$`)
)

func eval005(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	o := &outcome{}
	namedTypes := []string{
		"Microsoft.Storage/storageAccounts", "Microsoft.Search/searchServices", "Microsoft.DocumentDB/databaseAccounts",
		"Microsoft.KeyVault/vaults", "Microsoft.ApiManagement/service", "Microsoft.ContainerRegistry/registries",
		"Microsoft.Insights/components", "Microsoft.App/containerApps", "Microsoft.CognitiveServices/accounts/projects",
		"Microsoft.CognitiveServices/accounts",
	}
	for _, r := range newView(in).of(o, namedTypes...) {
		name := r.Name
		if unresolvedString(name) {
			o.skips = append(o.skips, r.Type+": name is unresolved")
			continue
		}
		last := name
		if i := strings.LastIndex(name, "/"); i >= 0 {
			last = name[i+1:]
		}
		var bad string
		switch r.Type {
		case "Microsoft.Storage/storageAccounts":
			if !reStorage.MatchString(last) {
				bad = "storage account name violates provider rules"
			}
		case "Microsoft.Search/searchServices":
			if !reSearch.MatchString(last) || strings.Contains(last, "--") || strings.HasPrefix(last, "-") || strings.HasSuffix(last, "-") {
				bad = "search service name violates provider rules"
			}
		case "Microsoft.DocumentDB/databaseAccounts":
			if !reCosmos.MatchString(last) {
				bad = "Cosmos DB account name violates provider rules"
			}
		case "Microsoft.KeyVault/vaults":
			if !reKV.MatchString(last) || strings.Contains(last, "--") {
				bad = "Key Vault name violates provider rules"
			}
		case "Microsoft.ApiManagement/service":
			if !reAPIM.MatchString(last) {
				bad = "API Management name violates provider rules"
			}
		case "Microsoft.ContainerRegistry/registries":
			if !reACR.MatchString(last) {
				bad = "Container Registry name violates provider rules"
			}
		case "Microsoft.Insights/components":
			if !reAppi.MatchString(last) {
				bad = "Application Insights component name violates provider rules"
			}
		case "Microsoft.App/containerApps":
			if !reCA.MatchString(last) {
				bad = "Container App name violates provider rules"
			}
		case "Microsoft.CognitiveServices/accounts/projects":
			if !reFoundryProject.MatchString(last) {
				bad = "Foundry project name violates documented rules"
			}
		case "Microsoft.CognitiveServices/accounts":
			if len(last) < 2 || len(last) > 64 || !reFoundryProject.MatchString(last) {
				bad = "Foundry account name violates documented rules"
			}
		}
		if bad != "" {
			o.addKey(r, "name", bad)
		}
	}
	return o.result()
}

func eval006(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	minDays, ok := policyInt(in, "logRetention.minimumDays")
	if !ok {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("logRetention.minimumDays")}}
	}
	o := &outcome{}
	workspaces := newView(in).of(o, "Microsoft.OperationalInsights/workspaces")
	if len(workspaces) == 0 {
		if len(o.notes) > 0 {
			return o.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "workspace-is-existing-resource-not-in-template"}}
	}
	for _, ws := range workspaces {
		val, ok := ws.Properties["retentionInDays"]
		if !ok {
			o.skips = append(o.skips, ws.Name+": retentionInDays is unavailable")
			continue
		}
		ret, rok := val.(float64)
		if !rok {
			if i, ok := val.(int); ok {
				ret = float64(i)
				rok = true
			}
		}
		if !rok {
			o.skips = append(o.skips, ws.Name+": retentionInDays is unresolved")
			continue
		}
		if ret < float64(minDays) {
			o.add(ws, fmt.Sprintf("workspace retention %.0f days is below the configured minimum %d", ret, minDays))
		}
		if ret < 4 || ret > 730 {
			o.add(ws, "workspace retentionInDays is outside the documented analytics range")
		}
		// features.immediatePurgeDataOn30Days is a boolean in the workspace schema.
		purge, purgeOK := getBool(ws.Properties, "features", "immediatePurgeDataOn30Days")
		if s, isStr := getString(ws.Properties, "features", "immediatePurgeDataOn30Days"); isStr && isExpr(s) {
			o.skips = append(o.skips, ws.Name+": immediatePurgeDataOn30Days is unresolved")
		} else if purgeOK && purge && minDays > 30 {
			o.add(ws, "immediate purge on 30 days conflicts with the configured retention minimum")
		}
	}
	return o.result()
}

// azureYAMLLocation anchors repository-level findings on azure.yaml.
func azureYAMLLocation(in *sdk.Input) sdk.Location {
	if in != nil && in.AzureYAML != nil {
		if p := in.AzureYAML.Path(); p != "" {
			return sdk.Location{File: p}
		}
	}
	return sdk.Location{File: "azure.yaml"}
}

const pipelineSearched = ".github/workflows, .azdo/pipelines and .azuredevops/pipelines (.yml and .yaml)"

func eval007(in *sdk.Input, p Providers) sdk.Result {
	if profileTier(in.Profile) == "dev" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "dev-profile"}}
	}
	if p.Root == "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "repository-root-not-available"}}
	}
	// The azure.yaml pipeline: section is configuration, not evidence that a
	// pipeline file exists, so only pipeline files count.
	base, hasGit := gitRoot(p.Root)
	scan := scanPipelines(base)
	unfollowed := false
	for _, f := range scan.Files {
		for _, c := range f.Commands {
			switch scanShellForAzd(c) {
			case shellFound:
				return sdk.Result{}
			case shellUncertain:
				unfollowed = true
			}
		}
	}
	switch {
	case unfollowed:
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: a pipeline run step uses shell constructs that were not followed (heredoc, function, eval or sh -c, computed azd arguments), so it may invoke azd provision, deploy or up"}}
	case len(scan.Files) > 0:
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: no pipeline run step invokes azd provision, deploy or up; other deployment tools are valid and other CI systems are not detected"}}
	case scan.Skipped > 0:
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: pipeline files exist but were not read (symlink, oversize, invalid YAML or file-count cap)"}}
	case !hasGit:
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: no .git directory was found above the project, so only the project directory was searched for pipeline files"}}
	}
	return sdk.Result{Findings: []sdk.Finding{{
		Location: azureYAMLLocation(in),
		Evidence: "no CI/CD pipeline definition was found in " + pipelineSearched + " from the repository root; other CI systems are not detected",
	}}}
}

var reEvalRun = regexp.MustCompile(`(?i)(^|[\s;&|(` + "`" + `])azd\s+ai\s+agent\s+eval\s+run\b`)

func eval008(in *sdk.Input, p Providers) sdk.Result {
	if profileTier(in.Profile) == "dev" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "dev-profile"}}
	}
	root := docRoot(in)
	hasAgent := false
	for _, pair := range yPairs(yChild(root, "services")) {
		if yScalar(yChild(pair[1], "host")) == "azure.ai.agent" {
			hasAgent = true
			break
		}
	}
	if !hasAgent {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "project-has-no-agent-services"}}
	}
	if p.Root == "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "repository-root-not-available"}}
	}
	o := &outcome{}
	loc := azureYAMLLocation(in)
	addF := func(l sdk.Location, ev string) {
		o.findings = append(o.findings, sdk.Finding{Location: l, Evidence: ev})
	}
	body, ok, skipped := readRootFile(p.Root, "eval.yaml")
	switch {
	case skipped:
		o.skips = append(o.skips, "eval.yaml exists but was not read (symlink, oversize or unreadable)")
	case !ok:
		addF(sdk.Location{File: "eval.yaml"}, "eval.yaml is missing from the project directory")
	default:
		var doc yaml.Node
		if err := yaml.Unmarshal(body, &doc); err != nil || len(doc.Content) == 0 {
			o.skips = append(o.skips, "eval.yaml is not valid YAML")
			break
		}
		top := doc.Content[0]
		if ev := yChild(top, "evaluators"); ev == nil || (ev.Kind != yaml.AliasNode && len(ev.Content) == 0 && strings.TrimSpace(ev.Value) == "") {
			addF(sdk.Location{File: "eval.yaml"}, "eval.yaml does not declare evaluators")
		}
		if yChild(top, "dataset") == nil && yChild(top, "dataset_file") == nil {
			addF(sdk.Location{File: "eval.yaml"}, "eval.yaml does not declare a dataset reference")
		}
	}
	base, hasGit := gitRoot(p.Root)
	scan := scanPipelines(base)
	found := false
	for _, f := range scan.Files {
		runs := false
		for _, c := range f.Commands {
			if matchLines(c, reEvalRun) {
				runs = true
				break
			}
		}
		if !runs {
			continue
		}
		found = true
		idxEval := -1
		if m := reEvalRun.FindStringIndex(f.Flat); m != nil {
			idxEval = m[0]
		}
		if idxProd := strings.Index(f.Flat, "production"); idxProd >= 0 && idxEval > idxProd {
			addF(sdk.Location{File: f.Path}, "pipeline runs azd ai agent eval run only after a production marker")
		}
	}
	if !found {
		switch {
		case scan.Skipped > 0:
			o.skips = append(o.skips, "pipeline files exist but were not read (symlink, oversize, invalid YAML or file-count cap)")
		case len(scan.Files) == 0 && !hasGit:
			o.skips = append(o.skips, "no .git directory was found above the project, so only the project directory was searched for pipeline files")
		default:
			addF(loc, "no pipeline run step runs azd ai agent eval run in "+pipelineSearched)
		}
	}
	return o.result()
}

func eval009(in *sdk.Input, _ Providers) sdk.Result {
	scopeList := policyStrings(in, "dataResidency.regions")
	if in.Policy == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("dataResidency.scope")}}
	}
	scopeAny, ok := in.Policy.Get("dataResidency.scope")
	if !ok {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("dataResidency.scope")}}
	}
	scope, _ := scopeAny.(string)
	scope = strings.ToLower(strings.TrimSpace(scope))
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	skuAllow := map[string]bool{}
	for _, s := range policyStrings(in, "dataResidency.deploymentSkus") {
		skuAllow[strings.ToLower(s)] = true
	}
	allowedRegion := map[string]bool{}
	for _, s := range scopeList {
		allowedRegion[strings.ToLower(strings.ReplaceAll(s, " ", ""))] = true
	}
	o := &outcome{}
	v := newView(in)
	for _, r := range v.of(o, "Microsoft.CognitiveServices/accounts/deployments") {
		sku := strings.ToLower(r.SKUName)
		switch {
		case sku == "":
			o.skips = append(o.skips, r.Name+": sku.name is unresolved")
		case sku == "developertier" && scope != "global":
			// DeveloperTier is outside any residency guarantee; with a global
			// scope requirement there is nothing to violate.
			o.add(r, "DeveloperTier has no documented data residency guarantee")
		case len(skuAllow) > 0 && !skuAllow[sku]:
			o.add(r, "deployment SKU is outside the configured residency SKU allow-list")
		case scope != "global" && strings.HasPrefix(sku, "global"):
			o.add(r, "global deployment SKU is broader than the declared residency requirement")
		case strings.HasPrefix(scope, "datazone-") && !strings.HasPrefix(sku, "datazone"):
			o.add(r, "deployment SKU is not a data-zone SKU under a declared data-zone requirement")
		}
	}
	for _, r := range v.of(o, "Microsoft.CognitiveServices/accounts", "Microsoft.Search/searchServices", "Microsoft.Storage/storageAccounts", "Microsoft.DocumentDB/databaseAccounts") {
		if r.Region == "" || unresolvedString(r.Region) {
			o.skips = append(o.skips, r.Name+": resource location is unresolved")
			continue
		}
		if len(allowedRegion) > 0 && !allowedRegion[strings.ToLower(strings.ReplaceAll(r.Region, " ", ""))] {
			o.add(r, "resource location is outside the declared residency region list")
		}
	}
	return o.result()
}

func modelID(format, name, version string) string {
	out := strings.TrimSpace(format) + "/" + strings.TrimSpace(name)
	if strings.TrimSpace(version) != "" {
		out += "/" + strings.TrimSpace(version)
	}
	return out
}

func modelAllowed(list []string, model string) bool {
	for _, item := range list {
		if strings.EqualFold(item, model) || (strings.Count(item, "/") == 1 && strings.HasPrefix(strings.ToLower(model), strings.ToLower(item)+"/")) {
			return true
		}
	}
	return false
}

func eval010(in *sdk.Input, _ Providers) sdk.Result {
	allow := policyStrings(in, "models.allow")
	deny := policyStrings(in, "models.deny")
	if len(allow) == 0 && len(deny) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("models.allow")}}
	}
	o := &outcome{}
	for _, a := range allow {
		if modelAllowed(deny, a) {
			o.findings = append(o.findings, sdk.Finding{Evidence: "model allow and deny lists overlap on " + a})
		}
	}
	v := newView(in)
	for _, r := range v.of(o, "Microsoft.CognitiveServices/accounts/deployments") {
		model, _ := r.Properties["model"].(map[string]any)
		format, _ := model["format"].(string)
		name, _ := model["name"].(string)
		version, _ := model["version"].(string)
		// Compiled Bicep keeps parameters as expressions; any expression in
		// format, name or version makes the model identity unknown.
		if isExpr(format) || isExpr(name) || isExpr(version) {
			o.skips = append(o.skips, r.Name+": deployment model format, name or version is an unresolved expression")
			continue
		}
		id := modelID(format, name, version)
		if id == "/" {
			o.skips = append(o.skips, r.Name+": deployment model is unresolved")
			continue
		}
		if modelAllowed(deny, id) {
			o.add(r, "deployed model is on the deny list")
		}
		if len(allow) > 0 && !modelAllowed(allow, id) {
			o.add(r, "deployed model is not allowed by the configured allow list")
		}
	}
	for _, r := range v.of(o, "Microsoft.Authorization/policyAssignments") {
		id, _ := getString(r.Properties, "policyDefinitionId")
		if !strings.Contains(strings.ToLower(id), "aafe3651-cb78-4f68-9f81-e7e41509110f") {
			continue
		}
		effect, _ := getString(r.Properties, "parameters", "effect", "value")
		pubs := getArray(r.Properties, "parameters", "allowedPublishers", "value")
		assets := getArray(r.Properties, "parameters", "allowedAssetIds", "value")
		if strings.EqualFold(effect, "Deny") && len(pubs) == 0 && len(assets) == 0 {
			o.add(r, "policy assignment uses Deny with empty allowedPublishers and allowedAssetIds")
		}
	}
	return o.result()
}
