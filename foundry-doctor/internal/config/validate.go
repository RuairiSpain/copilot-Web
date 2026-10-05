package config

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Issue is one value problem, located by key path.
type Issue struct {
	Path string
	Msg  string
}

type issues []Issue

func (is *issues) add(path, format string, a ...any) {
	*is = append(*is, Issue{Path: path, Msg: fmt.Sprintf(format, a...)})
}

// Validate checks every value of the file and returns an *InvalidError listing all problems, or nil.
// Parse calls it; call it again only on a File built in code.
func (f *File) Validate() error {
	var is issues
	if f.Version != SchemaVersion {
		if f.Version == 0 {
			is.add("version", "required; the only supported value is %d", SchemaVersion)
		} else {
			is.add("version", "unsupported version %d; the only supported value is %d", f.Version, SchemaVersion)
		}
	}
	if f.Profile != "" {
		if _, err := NormalizeProfile(f.Profile); err != nil {
			is.add("profile", "%v", err)
		}
	}
	f.Inputs.validate("inputs", &is, true)
	f.Rules.validate("rules", &is)
	f.Validation.validate("validation", &is)
	validatePolicy(f.Policy, "policy", &is)
	for _, name := range sortedKeys(f.Environments) {
		p := "environments." + name
		if !envNameRe.MatchString(name) {
			is.add(p, "environment name %q must match %s", name, envNameRe)
		}
		validatePolicy(f.Environments[name].Policy, p+".policy", &is)
	}
	f.Adoption.validate("adoption", &is)
	f.Outputs.validate("outputs", &is, true)
	f.Advanced.validate("advanced", &is)

	ps := make([]*Error, 0, len(is))
	for _, i := range is {
		ps = append(ps, &Error{File: f.name, Line: f.lineOf(i.Path), Path: i.Path, Msg: i.Msg})
	}
	return newInvalid(ps)
}

// lineOf finds the line of path, falling back to the longest known prefix.
func (f *File) lineOf(p string) int {
	for p != "" {
		if l, ok := f.lines[p]; ok {
			return l
		}
		cut := max(strings.LastIndexAny(p, ".["), 0)
		p = p[:cut]
	}
	return 0
}

var (
	envNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	selectorRe = regexp.MustCompile(`^[A-Za-z0-9*][A-Za-z0-9*._-]*$`)
	resTypeRe  = regexp.MustCompile(`^[A-Za-z0-9.]+(/[A-Za-z0-9.]+)+$`)
	regionRe   = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	skuRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	execRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	modelSegRe = regexp.MustCompile(`^[^\s/\x00-\x1f\x7f]+$`)
)

// maxRegexLen bounds tag value patterns. It is our own limit, not an Azure one.
const maxRegexLen = 1024

func noControl(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

func (in Inputs) validate(p string, is *issues, repoFile bool) {
	checkPath(p+".azureYaml", in.AzureYAML, repoFile, is)
	checkPath(p+".infraPath", in.InfraPath, repoFile, is)
	if in.Environment != "" && !envNameRe.MatchString(in.Environment) {
		is.add(p+".environment", "%q must match %s", in.Environment, envNameRe)
	}
}

// checkPath validates a path value. In a repository config file a path must be relative and stay inside the
// project root; flags may be absolute because the user typed them on their own machine.
func checkPath(p, v string, confined bool, is *issues) {
	if v == "" {
		return
	}
	if !noControl(v) {
		is.add(p, "path contains control characters")
		return
	}
	if !confined {
		return
	}
	if path.IsAbs(v) || strings.HasPrefix(v, `\`) || (len(v) > 1 && v[1] == ':') {
		is.add(p, "path %q must be relative to the project root", v)
		return
	}
	c := path.Clean(strings.ReplaceAll(v, `\`, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		is.add(p, "path %q must stay inside the project root", v)
	}
}

func (r Rules) validate(p string, is *issues) {
	for _, l := range []struct {
		key  string
		list []string
		re   *regexp.Regexp
	}{{"include", r.Include, selectorRe}, {"exclude", r.Exclude, selectorRe}, {"packs", r.Packs, nameRe}} {
		checkList(p+"."+l.key, l.list, l.re, false, is)
	}
}

func (v Validation) validate(p string, is *issues) {
	for _, m := range []struct {
		key string
		v   Mode
	}{{"bicep", v.Bicep}, {"azure", v.Azure}, {"runtime", v.Runtime}} {
		switch m.v {
		case "", ModeRequired, ModeOptional, ModeDisabled:
		default:
			is.add(p+"."+m.key, "%q is not one of required, optional, disabled", m.v)
		}
	}
}

func (a Adoption) validate(p string, is *issues) {
	checkPath(p+".baseline", a.Baseline, true, is)
	checkPath(p+".suppressions", a.Suppressions, true, is)
}

// Formats lists the accepted output formats.
func Formats() []Format { return []Format{FormatConsole, FormatJSON, FormatMarkdown, FormatSARIF} }

func (o Outputs) validate(p string, is *issues, repoFile bool) {
	seen := map[Format]int{}
	for i, f := range o.Formats {
		ip := p + ".formats[" + strconv.Itoa(i) + "]"
		if !slices.Contains(Formats(), f) {
			is.add(ip, "%q is not one of console, json, markdown, sarif", f)
			continue
		}
		if j, dup := seen[f]; dup {
			is.add(ip, "duplicate of formats[%d]", j)
		}
		seen[f] = i
	}
	checkPath(p+".directory", o.Directory, repoFile, is)
}

func (t Tool) validate(p string, is *issues) {
	switch t.Enabled {
	case "", ToggleAuto, ToggleTrue, ToggleFalse:
	default:
		is.add(p+".enabled", "%q is not one of auto, true, false", t.Enabled)
	}
	if t.Config != nil {
		checkPath(p+".config", *t.Config, true, is)
	}
}

func (a Advanced) validate(p string, is *issues) {
	a.PSRule.validate(p+".psrule", is)
	a.Checkov.validate(p+".checkov", is)
	if a.Bicep.Executable != "" && a.Bicep.Executable != "auto" && !execRe.MatchString(a.Bicep.Executable) {
		is.add(p+".bicep.executable", "%q must be auto or a bare command name without a path", a.Bicep.Executable)
	}
	if a.Bicep.Config != nil {
		checkPath(p+".bicep.config", *a.Bicep.Config, true, is)
	}
	checkList(p+".adapters", a.Adapters, nameRe, false, is)
}

// checkList validates a list of strings: each entry matches re (when not nil) and entries are unique.
func checkList(p string, list []string, re *regexp.Regexp, fold bool, is *issues) {
	seen := map[string]int{}
	for i, s := range list {
		ip := p + "[" + strconv.Itoa(i) + "]"
		if s == "" {
			is.add(ip, "empty entry")
			continue
		}
		if re != nil && !re.MatchString(s) {
			is.add(ip, "%q does not match %s", s, re)
			continue
		}
		k := s
		if fold {
			k = strings.ToLower(s)
		}
		if j, dup := seen[k]; dup {
			is.add(ip, "duplicate of %s[%d]", p, j)
			continue
		}
		seen[k] = i
	}
}

// validatePolicy checks one policy block (ADR-007) and appends problems under the key path prefix.
// A zero Policy is valid: every key is optional and absence means "skip the dependent rules".
func validatePolicy(pol model.Policy, prefix string, is *issues) {
	k := func(s string) string { return prefix + "." + s }

	if pol.ResourceScope != nil {
		switch *pol.ResourceScope {
		case model.ScopeSameResourceGroup, model.ScopeSameSubscription, model.ScopeAny:
		default:
			is.add(k("resourceScope"), "%q is not one of same-resource-group, same-subscription, any", *pol.ResourceScope)
		}
	}
	for i, s := range pol.AllowedExternalScopes {
		ip := k("allowedExternalScopes") + "[" + strconv.Itoa(i) + "]"
		if !strings.HasPrefix(s, "/") || strings.ContainsFunc(s, unicode.IsSpace) || !noControl(s) || len(s) < 2 {
			is.add(ip, "%q is not a resource ID (expected a value starting with /subscriptions/ or /providers/)", s)
		}
	}
	checkList(k("allowedExternalScopes"), onlyValid(pol.AllowedExternalScopes), nil, true, is)

	e := pol.Environments
	checkList(k("environments.production"), e.Production, envNameRe, false, is)
	checkList(k("environments.nonProduction"), e.NonProduction, envNameRe, false, is)
	checkList(k("environments.development"), e.Development, envNameRe, false, is)
	for _, name := range e.Production {
		if slices.Contains(e.NonProduction, name) {
			is.add(k("environments.nonProduction"), "environment %q is listed as production and nonProduction", name)
		}
		if slices.Contains(e.Development, name) {
			is.add(k("environments.development"), "environment %q is listed as production and development", name)
		}
	}

	validateTags(pol.Tags, k("tags"), is)

	if pol.LogRetention.MinimumDays != nil && *pol.LogRetention.MinimumDays < 0 {
		is.add(k("logRetention.minimumDays"), "must be a non-negative integer, got %d", *pol.LogRetention.MinimumDays)
	}

	validateModels(pol.Models, k("models"), is)

	dr := pol.DataResidency
	if dr.Scope != nil {
		switch *dr.Scope {
		case model.ResidencyGlobal, model.ResidencyDatazoneUS, model.ResidencyDatazoneEU, model.ResidencyDatazoneAPAC, model.ResidencyGeography:
		default:
			is.add(k("dataResidency.scope"), "%q is not one of global, datazone-us, datazone-eu, datazone-apac, geography", *dr.Scope)
		}
	}
	checkList(k("dataResidency.regions"), dr.Regions, regionRe, false, is)
	checkList(k("dataResidency.deploymentSkus"), dr.DeploymentSkus, skuRe, false, is)

	if r := pol.DisasterRecovery.Reference; r != nil && (strings.TrimSpace(*r) == "" || !noControl(*r)) {
		is.add(k("disasterRecovery.reference"), "must be a non-empty URL or path without control characters")
	}

	if pa := pol.Network.PublicAccess; pa != nil {
		switch *pa {
		case model.PublicForbidden, model.PublicEntraOnly, model.PublicAllowed:
		default:
			is.add(k("network.publicAccess"), "%q is not one of forbidden, entra-only, allowed", *pa)
		}
	}

	seen := map[model.ManagedBy]int{}
	for i, m := range pol.ManagedByAzurePolicy {
		ip := k("managedByAzurePolicy") + "[" + strconv.Itoa(i) + "]"
		switch m {
		case model.ManagedPrivateDNSZoneGroup, model.ManagedDiagnosticSettings:
		default:
			is.add(ip, "%q is not one of private-dns-zone-group, diagnostic-settings", m)
			continue
		}
		if j, dup := seen[m]; dup {
			is.add(ip, "duplicate of managedByAzurePolicy[%d]", j)
		}
		seen[m] = i
	}

	if t := pol.Cost.DevMaxCosmosThroughput; t != nil && *t < 0 {
		is.add(k("cost.devMaxCosmosThroughput"), "must be a non-negative integer, got %d", *t)
	}
	for i, s := range pol.Cost.ProductionSizedSkuExemptions {
		if strings.TrimSpace(s) == "" || !noControl(s) {
			is.add(k("cost.productionSizedSkuExemptions")+"["+strconv.Itoa(i)+"]", "must be a non-empty resource name or ID without control characters")
		}
	}
	checkList(k("cost.productionSizedSkuExemptions"), onlyValid(pol.Cost.ProductionSizedSkuExemptions), nil, false, is)
}

// onlyValid drops empty entries so the uniqueness pass does not repeat the shape error.
func onlyValid(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func validateTags(t model.PolicyTags, p string, is *issues) {
	seen := map[string]int{}
	for i, r := range t.Required {
		ip := p + ".required[" + strconv.Itoa(i) + "]"
		switch {
		case strings.TrimSpace(r.Name) == "" || !noControl(r.Name):
			is.add(ip+".name", "tag name is required and must not contain control characters")
		case r.Name != strings.TrimSpace(r.Name):
			is.add(ip+".name", "tag name %q has leading or trailing whitespace", r.Name)
		default:
			// Tag names compare case-insensitively (ADR-007 decision 6).
			key := strings.ToLower(r.Name)
			if j, dup := seen[key]; dup {
				is.add(ip+".name", "duplicate of required[%d] (tag names compare case-insensitively)", j)
			}
			seen[key] = i
		}
		if r.Format != "" {
			if len(r.Format) > maxRegexLen {
				is.add(ip+".format", "pattern is longer than %d characters", maxRegexLen)
			} else if _, err := regexp.Compile(r.Format); err != nil {
				is.add(ip+".format", "not a valid regular expression: %v", err)
			}
		}
	}
	seenT := map[string]int{}
	for i, s := range t.ResourceTypes {
		ip := p + ".resourceTypes[" + strconv.Itoa(i) + "]"
		if !resTypeRe.MatchString(s) {
			is.add(ip, "%q is not a resource type such as Microsoft.CognitiveServices/accounts", s)
			continue
		}
		key := strings.ToLower(s) // Azure resource types are case-insensitive
		if j, dup := seenT[key]; dup {
			is.add(ip, "duplicate of resourceTypes[%d]", j)
		}
		seenT[key] = i
	}
}

func validateModels(m model.PolicyModels, p string, is *issues) {
	check := func(key string, list []string) {
		seen := map[string]int{}
		for i, s := range list {
			ip := p + "." + key + "[" + strconv.Itoa(i) + "]"
			segs := strings.Split(s, "/")
			if len(segs) < 2 || len(segs) > 3 || slices.ContainsFunc(segs, func(x string) bool { return !modelSegRe.MatchString(x) }) {
				is.add(ip, "%q is not a model identifier of the form format/name[/version]", s)
				continue
			}
			if j, dup := seen[s]; dup {
				is.add(ip, "duplicate of %s[%d]", key, j)
			}
			seen[s] = i
		}
	}
	check("allow", m.Allow)
	check("deny", m.Deny)
	for _, a := range m.Allow {
		if slices.Contains(m.Deny, a) {
			is.add(p+".deny", "%q is in both allow and deny", a)
		}
	}
}

// ValidatePolicyValues is the exported entry for callers that hold a bare Policy (for example tests of other
// packages). It returns the problems in key-path order.
func ValidatePolicyValues(pol model.Policy) []Issue {
	var is issues
	validatePolicy(pol, "policy", &is)
	sort.SliceStable(is, func(i, j int) bool { return is[i].Path < is[j].Path })
	return is
}
