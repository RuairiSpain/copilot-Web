// Package ops implements the catalogue OPS rules conservatively from repository,
// azure.yaml and compiled ARM evidence. Any unresolved or unsupported input
// yields skip/uncertain, never a silent pass.
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

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
		Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
		Location: r.Location,
		Evidence: evidence,
	})
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
		return sdk.Result{Findings: o.findings}
	}
	if len(o.skips) > 0 {
		slices.Sort(o.skips)
		return sdk.Result{Skipped: &sdk.Skip{Reason: strings.Join(unique(o.skips), "; ")}}
	}
	if len(o.notes) > 0 {
		slices.Sort(o.notes)
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: " + strings.Join(unique(o.notes), "; ")}}
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

func unresolvedString(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

func byType(in *sdk.Input, typ string) []sdk.ARMResource {
	if in == nil || in.ARM == nil {
		return nil
	}
	var out []sdk.ARMResource
	for _, r := range in.ARM.Resources() {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, r)
		}
	}
	return out
}

func foundryAccounts(in *sdk.Input) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range byType(in, "Microsoft.CognitiveServices/accounts") {
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

func repoFiles(root string, rels ...string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	for _, rel := range rels {
		base := filepath.Join(root, rel)
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err == nil {
				out[filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator)))] = string(b)
			}
			return nil
		})
	}
	return out
}

var supportedLogs = map[string]map[string]bool{
	"Microsoft.CognitiveServices/accounts":           {"audit": true, "requestresponse": true, "trace": true, "azureopenairequestusage": true, "managednetworkevent": true},
	"Microsoft.CognitiveServices/accounts/projects":  {"audit": true, "trace": true},
	"Microsoft.Search/searchServices":                {"operationlogs": true},
	"Microsoft.ApiManagement/service":                {"gatewaylogs": true, "gatewayllmlogs": true, "gatewaymcplogs": true, "websocketconnectionlogs": true, "developerportalauditlogs": true},
	"Microsoft.Storage/storageAccounts/blobServices": {"storageread": true, "storagewrite": true, "storagedelete": true},
	"Microsoft.DocumentDB/databaseAccounts":          {"dataplanerequests": true, "controlplanerequests": true, "queryruntimestatistics": true, "partitionkeystatistics": true, "partitionkeyruconsumption": true},
}

func eval001(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	if managed := policyStrings(in, "managedByAzurePolicy"); slices.Contains(managed, "diagnostic-settings") {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "policy-managed"}}
	}
	o := &outcome{}
	targets := []sdk.ARMResource{}
	for _, typ := range []string{
		"Microsoft.CognitiveServices/accounts",
		"Microsoft.CognitiveServices/accounts/projects",
		"Microsoft.Search/searchServices",
		"Microsoft.ApiManagement/service",
		"Microsoft.Storage/storageAccounts/blobServices",
		"Microsoft.DocumentDB/databaseAccounts",
	} {
		targets = append(targets, byType(in, typ)...)
	}
	settings := byType(in, "Microsoft.Insights/diagnosticSettings")
	for _, r := range targets {
		matched := 0
		for _, ds := range settings {
			if !(refMatches(ds.Scope, r.Name) || childOf(ds.Name, r.Name)) {
				continue
			}
			matched++
			ws, _ := getString(ds.Properties, "workspaceId")
			if ws == "" {
				o.add(r, "diagnostic setting has no Log Analytics workspace destination")
				continue
			}
			if unresolvedString(ws) {
				o.skips = append(o.skips, r.Name+": workspaceId is unresolved")
				continue
			}
			logs := getArray(ds.Properties, "logs")
			if len(logs) == 0 {
				o.add(r, "diagnostic setting has metrics only")
				continue
			}
			okLog := false
			for _, raw := range logs {
				m, _ := raw.(map[string]any)
				enabled, _ := m["enabled"].(bool)
				if !enabled {
					continue
				}
				if cg, _ := m["categoryGroup"].(string); strings.EqualFold(cg, "allLogs") || strings.EqualFold(cg, "audit") {
					okLog = true
					continue
				}
				if cat, _ := m["category"].(string); supportedLogs[r.Type][strings.ToLower(cat)] {
					okLog = true
				} else if cat != "" {
					o.add(r, "diagnostic setting uses unsupported log category "+cat)
				}
			}
			if !okLog {
				o.add(r, "diagnostic setting has no enabled supported logs")
			}
		}
		if matched == 0 {
			o.add(r, "resource has no diagnostic setting to Log Analytics")
		} else if matched > 5 {
			o.add(r, "resource has more than five diagnostic settings")
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
	accounts := foundryAccounts(in)
	if len(accounts) == 0 {
		return sdk.Result{}
	}
	o := &outcome{}
	hasServiceHealth := false
	hasMetrics := false
	for _, r := range in.ARM.Resources() {
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
	if !hasServiceHealth {
		o.add(accounts[0], "no Service Health alert definition was found")
	}
	if !hasMetrics {
		o.add(accounts[0], "no Foundry model metric alert or scheduled query rule with an action group was found")
	}
	return o.result()
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
	type tagReq struct {
		Name   string
		Format string
	}
	var reqs []tagReq
	switch xs := reqsAny.(type) {
	case []any:
		for _, x := range xs {
			if m, ok := x.(map[string]any); ok {
				reqs = append(reqs, tagReq{Name: fmt.Sprint(m["Name"]), Format: fmt.Sprint(m["Format"])})
				if reqs[len(reqs)-1].Name == "<nil>" {
					reqs[len(reqs)-1].Name = fmt.Sprint(m["name"])
				}
				if reqs[len(reqs)-1].Format == "<nil>" {
					reqs[len(reqs)-1].Format = fmt.Sprint(m["format"])
				}
			}
		}
	case []struct{ Name, Format string }:
		for _, x := range xs {
			reqs = append(reqs, tagReq{Name: x.Name, Format: x.Format})
		}
	}
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
	for _, r := range in.ARM.Resources() {
		if !scope[strings.ToLower(r.Type)] {
			continue
		}
		if len(r.Tags) == 0 {
			o.skips = append(o.skips, r.Name+": tags are unavailable")
			continue
		}
		lowered := map[string]string{}
		for k, v := range r.Tags {
			lowered[strings.ToLower(k)] = fmt.Sprint(v)
			if len(k) > 512 || (strings.EqualFold(r.Type, "Microsoft.Storage/storageAccounts") && len(k) > 128) {
				o.add(r, "tag name exceeds documented limit")
			}
			if len(fmt.Sprint(v)) > 256 {
				o.add(r, "tag value exceeds documented limit")
			}
		}
		if len(lowered) > 50 {
			o.add(r, "resource has more than fifty tags")
		}
		for _, req := range reqs {
			val, ok := lowered[strings.ToLower(req.Name)]
			if !ok {
				o.add(r, "missing required tag "+req.Name)
				continue
			}
			if req.Format != "" {
				if re, err := regexp.Compile(req.Format); err == nil && !re.MatchString(val) {
					o.add(r, "tag "+req.Name+" value does not match configured format")
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
	for _, r := range in.ARM.Resources() {
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
			o.add(r, bad)
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
	workspaces := byType(in, "Microsoft.OperationalInsights/workspaces")
	if len(workspaces) == 0 {
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
		if purge, _ := getString(ws.Properties, "features", "immediatePurgeDataOn30Days"); strings.EqualFold(purge, "true") && minDays > 30 {
			o.add(ws, "immediate purge on 30 days conflicts with the configured retention minimum")
		}
	}
	return o.result()
}

func eval007(in *sdk.Input, p Providers) sdk.Result {
	if profileTier(in.Profile) == "dev" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "dev-profile"}}
	}
	root := docRoot(in)
	if provider := yScalar(yChild(yChild(root, "pipeline"), "provider")); provider == "github" || provider == "azdo" {
		return sdk.Result{}
	}
	if p.Root == "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "repository-root-not-available"}}
	}
	files := repoFiles(p.Root, ".github/workflows", ".azdo/pipelines", ".azuredevops/pipelines")
	if len(files) == 0 {
		return sdk.Result{Findings: []sdk.Finding{{Evidence: "no supported CI/CD pipeline definition was found in the repository"}}}
	}
	for _, body := range files {
		if strings.Contains(strings.ToLower(body), "azd ") {
			return sdk.Result{}
		}
	}
	return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: ci-system-other-than-github-or-azure-pipelines or repository workflow does not invoke azd explicitly"}}
}

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
	evalPath := filepath.Join(p.Root, "eval.yaml")
	if _, err := os.Stat(evalPath); err != nil {
		o.findings = append(o.findings, sdk.Finding{Evidence: "eval.yaml is missing from the repository root"})
		return o.result()
	}
	body, _ := os.ReadFile(evalPath)
	text := strings.ToLower(string(body))
	if !strings.Contains(text, "evaluators") {
		o.findings = append(o.findings, sdk.Finding{Evidence: "eval.yaml does not declare evaluators"})
	}
	if !strings.Contains(text, "dataset") {
		o.findings = append(o.findings, sdk.Finding{Evidence: "eval.yaml does not declare a dataset reference"})
	}
	files := repoFiles(p.Root, ".github/workflows", ".azdo/pipelines", ".azuredevops/pipelines")
	found := false
	for _, body := range files {
		lower := strings.ToLower(body)
		idxEval := strings.Index(lower, "azd ai agent eval run")
		if idxEval < 0 {
			continue
		}
		found = true
		if idxProd := strings.Index(lower, "production"); idxProd >= 0 && idxEval > idxProd {
			o.findings = append(o.findings, sdk.Finding{Evidence: "pipeline runs azd ai agent eval run only after a production marker"})
		}
	}
	if !found {
		o.findings = append(o.findings, sdk.Finding{Evidence: "no repository pipeline step runs azd ai agent eval run"})
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
	skuAllow := map[string]bool{}
	for _, s := range policyStrings(in, "dataResidency.deploymentSkus") {
		skuAllow[strings.ToLower(s)] = true
	}
	allowedRegion := map[string]bool{}
	for _, s := range scopeList {
		allowedRegion[strings.ToLower(strings.ReplaceAll(s, " ", ""))] = true
	}
	o := &outcome{}
	for _, r := range byType(in, "Microsoft.CognitiveServices/accounts/deployments") {
		sku := strings.ToLower(r.SKUName)
		switch {
		case sku == "":
			o.skips = append(o.skips, r.Name+": sku.name is unresolved")
		case strings.EqualFold(r.SKUName, "DeveloperTier"):
			o.add(r, "DeveloperTier has no documented data residency guarantee")
		case len(skuAllow) > 0 && !skuAllow[sku]:
			o.add(r, "deployment SKU is outside the configured residency SKU allow-list")
		case scope != "global" && strings.HasPrefix(sku, "global"):
			o.add(r, "global deployment SKU is broader than the declared residency requirement")
		case strings.HasPrefix(scope, "datazone-") && !strings.HasPrefix(sku, "datazone"):
			o.add(r, "deployment SKU is not a data-zone SKU under a declared data-zone requirement")
		}
	}
	for _, r := range in.ARM.Resources() {
		switch r.Type {
		case "Microsoft.CognitiveServices/accounts", "Microsoft.Search/searchServices", "Microsoft.Storage/storageAccounts", "Microsoft.DocumentDB/databaseAccounts":
			if r.Region == "" || unresolvedString(r.Region) {
				o.skips = append(o.skips, r.Name+": resource location is unresolved")
				continue
			}
			if len(allowedRegion) > 0 && !allowedRegion[strings.ToLower(strings.ReplaceAll(r.Region, " ", ""))] {
				o.add(r, "resource location is outside the declared residency region list")
			}
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
	for _, r := range byType(in, "Microsoft.CognitiveServices/accounts/deployments") {
		props := r.Properties
		model, _ := props["model"].(map[string]any)
		id := modelID(fmt.Sprint(model["format"]), fmt.Sprint(model["name"]), fmt.Sprint(model["version"]))
		if unresolvedString(id) || id == "/" {
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
	for _, r := range byType(in, "Microsoft.Authorization/policyAssignments") {
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
