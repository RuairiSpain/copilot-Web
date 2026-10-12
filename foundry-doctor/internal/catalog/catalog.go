// Package catalog loads, validates and documents the Foundry Doctor rule catalogue.
//
// The catalogue is a directory tree of YAML files, one per rule:
// rules/catalog/<group>/<RULE-ID>.yaml. Loading is strict: unknown fields are errors.
package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// maxRuleFileBytes bounds the size of one rule file; real files are a few KiB.
const maxRuleFileBytes = 1 << 20

// InvalidError reports catalogue content that is malformed, as opposed to an I/O failure.
// Callers use errors.As to tell the two apart (the CLI maps content problems to exit 1 and I/O to 2).
type InvalidError struct{ Problems []error }

func (e *InvalidError) Error() string   { return errors.Join(e.Problems...).Error() }
func (e *InvalidError) Unwrap() []error { return e.Problems }

// Status is the lifecycle state of a rule in the catalogue.
type Status string

const (
	StatusProposed       Status = "proposed"        // seeded from the PRD, not yet researched
	StatusVerified       Status = "verified"        // platform fact confirmed in a primary source
	StatusProductOpinion Status = "product-opinion" // no platform source; labelled as opinion
	StatusImplemented    Status = "implemented"
	StatusDeprecated     Status = "deprecated"
	StatusDropped        Status = "dropped" // overlap decision is drop
)

// Rule is one catalogue entry.
type Rule struct {
	ID             string         `yaml:"id"`
	Version        int            `yaml:"version"`
	Group          string         `yaml:"group"`
	Title          string         `yaml:"title"`
	Description    string         `yaml:"description"`
	Status         Status         `yaml:"status"`
	Phases         []string       `yaml:"phases"`
	Inputs         []string       `yaml:"inputs"`
	Basis          []string       `yaml:"basis"`
	Category       string         `yaml:"category"`
	Pillar         string         `yaml:"pillar"`
	Severity       Severity       `yaml:"severity"`
	Compatibility  Compatibility  `yaml:"compatibility"`
	Evidence       Evidence       `yaml:"evidence"`
	Recommendation string         `yaml:"recommendation"`
	Fix            string         `yaml:"fix"`
	Sources        []Source       `yaml:"sources"`
	Origins        []Origin       `yaml:"origins"`
	Overlap        Overlap        `yaml:"overlap"`
	Implementation Implementation `yaml:"implementation"`
	Tests          Tests          `yaml:"tests"`
	Testability    string         `yaml:"testability"`
	Notes          string         `yaml:"notes"`
}

// Severity holds the per-profile severity.
type Severity struct {
	Dev  string `yaml:"dev"`
	Test string `yaml:"test"`
	Prod string `yaml:"prod"`
}

// Compatibility declares the tool and API versions a rule was verified against, in a form the engine can compare.
//
// Azd and Extensions hold exact versions or semver ranges with both endpoints; an empty Azd means the rule does not
// depend on azd behaviour. APIVersions contains exact, plane-specific API mappings. Preview must be true when any
// API version is a preview version or any azd or extension version is a prerelease; it means "verified
// against prerelease tooling", not that the Azure API itself is preview (many rules carry it only because the azd
// range ends at a prerelease build). A version outside a declared
// range is reported as skipped (unsupported-version), never evaluated. Prose goes in Notes.
type Compatibility struct {
	Azd         VersionRange            `yaml:"azd"`
	Extensions  map[string]VersionRange `yaml:"extensions"`
	APIVersions []APIVersion            `yaml:"apiVersions"`
	Preview     bool                    `yaml:"preview"`
	Notes       string                  `yaml:"notes"`
}

// APIPlane identifies which Azure API surface an exact version belongs to.
type APIPlane string

const (
	APIPlaneManagement APIPlane = "management"
	APIPlaneData       APIPlane = "data"
)

// APIVersion is one exact API compatibility tuple. Management-plane tuples use
// Provider and ResourceType; data-plane tuples use Service and EndpointFamily.
// Operation is optional, but absence is significant when matching.
type APIVersion struct {
	Plane          APIPlane `yaml:"plane"`
	Provider       string   `yaml:"provider"`
	ResourceType   string   `yaml:"resourceType"`
	Service        string   `yaml:"service"`
	EndpointFamily string   `yaml:"endpointFamily"`
	Operation      string   `yaml:"operation"`
	Version        string   `yaml:"version"`
}

// Valid reports whether the tuple is complete, internally consistent, and exact.
func (v APIVersion) Valid() bool {
	return validateAPIVersion(v) == nil
}

// Matches reports an exact match between two valid tuples. An omitted operation
// matches only another omitted operation; it is never a wildcard.
func (v APIVersion) Matches(target APIVersion) bool {
	return v.Valid() && target.Valid() && v == target
}

// SelectAPIVersion returns the unique exact match for target. It fails closed if
// target or any candidate is invalid, candidates contain duplicates, or no exact
// match exists.
func SelectAPIVersion(versions []APIVersion, target APIVersion) (APIVersion, bool) {
	if !target.Valid() {
		return APIVersion{}, false
	}
	seen := make(map[APIVersion]bool, len(versions))
	var selected APIVersion
	found := false
	for _, candidate := range versions {
		if !candidate.Valid() || seen[candidate] {
			return APIVersion{}, false
		}
		seen[candidate] = true
		if candidate == target {
			selected = candidate
			found = true
		}
	}
	return selected, found
}

// SelectAPIVersion returns the unique exact API mapping matching target.
func (c Compatibility) SelectAPIVersion(target APIVersion) (APIVersion, bool) {
	return SelectAPIVersion(c.APIVersions, target)
}

// VersionRange is an exact SemVer version or a range with both endpoints.
// Minimum/Maximum are compared according to SemVer precedence; build metadata
// does not affect precedence. IncludeMinimum and IncludeMaximum define whether
// the corresponding endpoint is included. Exact is mutually exclusive with
// all four bounded-range fields. The YAML loader also accepts the catalogue's
// legacy scalar spelling ("=1.2.3" or ">=1.2.3 <2.0.0") during migration.
type VersionRange struct {
	Exact          string `yaml:"exact,omitempty"`
	Minimum        string `yaml:"minimum,omitempty"`
	Maximum        string `yaml:"maximum,omitempty"`
	IncludeMinimum bool   `yaml:"includeMinimum,omitempty"`
	IncludeMaximum bool   `yaml:"includeMaximum,omitempty"`
	legacy         string
}

// Empty reports whether no compatibility constraint was supplied.
func (r VersionRange) Empty() bool {
	return r.Exact == "" && r.Minimum == "" && r.Maximum == "" && r.legacy == ""
}

func (r VersionRange) String() string {
	if r.legacy != "" {
		return r.legacy
	}
	if r.Exact != "" {
		return "=" + r.Exact
	}
	minOp, maxOp := ">", "<"
	if r.IncludeMinimum {
		minOp = ">="
	}
	if r.IncludeMaximum {
		maxOp = "<="
	}
	return minOp + r.Minimum + " " + maxOp + r.Maximum
}

// Contains reports whether v is valid SemVer and lies in the validated range.
func (r VersionRange) Contains(v string) bool {
	got, ok := parseSemVer(v)
	if !ok {
		return false
	}
	exact, min, max, minInc, maxInc, ok := r.bounds()
	if !ok {
		return false
	}
	if exact != nil {
		return compareSemVer(got, *exact) == 0
	}
	lo, hi := compareSemVer(got, *min), compareSemVer(got, *max)
	return (lo > 0 || minInc && lo == 0) && (hi < 0 || maxInc && hi == 0)
}

// UnmarshalYAML accepts both the structured contract and the legacy scalar.
func (r *VersionRange) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag != "!!str" {
			return fmt.Errorf("version range must be a string or mapping")
		}
		*r = VersionRange{legacy: n.Value}
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("version range must be a string or mapping")
	}
	allowed := set("exact", "minimum", "maximum", "includeMinimum", "includeMaximum")
	for i := 0; i < len(n.Content); i += 2 {
		if !allowed[n.Content[i].Value] {
			return fmt.Errorf("field %s not found in type catalog.VersionRange", n.Content[i].Value)
		}
	}
	type plain VersionRange
	var decoded plain
	if err := n.Decode(&decoded); err != nil {
		return err
	}
	*r = VersionRange(decoded)
	return nil
}

// Evidence describes what the deterministic check observes.
type Evidence struct {
	Description string `yaml:"description"`
	Confidence  string `yaml:"confidence"`
}

// Source is a verified reference. VerifiedVia is the path or URL actually read, when it
// differs from URL (for example the docs source repository behind a Microsoft Learn page).
type Source struct {
	URL          string `yaml:"url"`
	LastVerified string `yaml:"lastVerified"`
	VerifiedVia  string `yaml:"verifiedVia"`
	Note         string `yaml:"note"`
}

// Origin identifies a check from which a canonical rule was derived. System is
// currently "xf"; ID is the stable identifier in that source inventory.
type Origin struct {
	System string `yaml:"system"`
	ID     string `yaml:"id"`
}

// Overlap records how the rule relates to lower-level tools.
type Overlap struct {
	Research    OverlapResearch `yaml:"research"`
	Decision    string          `yaml:"decision"`
	Provisional bool            `yaml:"provisional"`
	Coverage    string          `yaml:"coverage"`
	Rationale   string          `yaml:"rationale"`

	// The top-level match arrays are the rule packet's migration format.
	// New entries put matches beside each per-tool research state instead.
	PSRule      []string `yaml:"psrule"`
	AzurePolicy []string `yaml:"azurePolicy"`
	Defender    []string `yaml:"defender"`
	Advisor     []string `yaml:"advisor"`
	BicepLinter []string `yaml:"bicepLinter"`
	Checkov     []string `yaml:"checkov"`
}

// ResearchState records the outcome of searching one external tool.
type ResearchState string

const (
	ResearchUnresearched  ResearchState = "unresearched"
	ResearchSearchedMatch ResearchState = "searched-match"
	ResearchSearchedNone  ResearchState = "searched-none"
)

// ToolResearch records both whether a tool catalogue was searched and the
// stable external identifiers found by that search.
type ToolResearch struct {
	State   ResearchState `yaml:"state"`
	Matches []string      `yaml:"matches"`
}

// OverlapResearch has one explicit state for every configured overlap tool.
type OverlapResearch struct {
	PSRule      ToolResearch `yaml:"psrule"`
	AzurePolicy ToolResearch `yaml:"azurePolicy"`
	Defender    ToolResearch `yaml:"defender"`
	Advisor     ToolResearch `yaml:"advisor"`
	BicepLinter ToolResearch `yaml:"bicepLinter"`
	Checkov     ToolResearch `yaml:"checkov"`
}

// ToolResearch returns research for tool, including the deterministic
// compatibility interpretation of the existing rule packet: a non-empty old
// match array is searched-match and an empty array is unresearched.
func (o Overlap) ToolResearch(tool string) ToolResearch {
	var current ToolResearch
	legacy := o.legacyMatches(tool)
	switch tool {
	case "psrule":
		current = o.Research.PSRule
	case "azurePolicy":
		current = o.Research.AzurePolicy
	case "defender":
		current = o.Research.Defender
	case "advisor":
		current = o.Research.Advisor
	case "bicepLinter":
		current = o.Research.BicepLinter
	case "checkov":
		current = o.Research.Checkov
	default:
		return ToolResearch{}
	}
	if current.State != "" || len(current.Matches) != 0 {
		return current
	}
	if len(legacy) != 0 {
		return ToolResearch{State: ResearchSearchedMatch, Matches: legacy}
	}
	return ToolResearch{State: ResearchUnresearched}
}

func (o Overlap) legacyMatches(tool string) []string {
	switch tool {
	case "psrule":
		return o.PSRule
	case "azurePolicy":
		return o.AzurePolicy
	case "defender":
		return o.Defender
	case "advisor":
		return o.Advisor
	case "bicepLinter":
		return o.BicepLinter
	case "checkov":
		return o.Checkov
	default:
		return nil
	}
}

// DecisionIsProvisional reports the explicit marker, or the compatibility
// result for the existing packet: a decision is provisional while any tool is
// unresearched.
func (o Overlap) DecisionIsProvisional() bool {
	if o.Provisional {
		return true
	}
	for _, tool := range overlapTools {
		if o.ToolResearch(tool).State == ResearchUnresearched {
			return true
		}
	}
	return false
}

func (o Overlap) hasStructuredResearch() bool {
	for _, research := range []ToolResearch{o.Research.PSRule, o.Research.AzurePolicy, o.Research.Defender,
		o.Research.Advisor, o.Research.BicepLinter, o.Research.Checkov} {
		if research.State != "" || len(research.Matches) != 0 {
			return true
		}
	}
	return false
}

// Implementation names who runs the check.
type Implementation struct {
	Owner   string `yaml:"owner"`
	Package string `yaml:"package"`
}

// Tests lists the scenarios the implementation must cover. Positive scenarios
// produce a violation/finding; negative scenarios are compliant and produce no
// finding. Skipped scenarios cannot be evaluated because a named input or
// capability is unavailable. Uncertain is optional and means the result is
// inconclusive; it is never treated as compliant, a finding, or skipped.
type Tests struct {
	Positive  []string `yaml:"positive"`
	Negative  []string `yaml:"negative"`
	Skipped   []string `yaml:"skipped"`
	Uncertain []string `yaml:"uncertain"`
}

// Allowed enumerations.
var (
	statuses = map[Status]bool{StatusProposed: true, StatusVerified: true, StatusProductOpinion: true,
		StatusImplemented: true, StatusDeprecated: true, StatusDropped: true}
	decisions    = set("reuse", "wrap", "adapt", "native", "drop")
	severities   = set("info", "warning", "error")
	confidences  = set("certain", "likely", "uncertain")
	categories   = set("must-have", "nice-to-have")
	coverages    = set("none", "partial", "full")
	pillars      = set("", "security", "reliability", "cost", "operations", "performance")
	owners       = set("native", "psrule", "azure-policy", "defender", "advisor", "bicep", "checkov", "adapter")
	inputPlanes  = set("azure.yaml", "bicep-arm", "azd-env", "control-plane", "data-plane", "network", "external-tool")
	basisValues  = set("platform", "security", "waf", "wara", "opinion", "reliability", "cost", "operations")
	phaseValues  = set("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "later", "future")
	overlapTools = []string{"psrule", "azurePolicy", "defender", "advisor", "bicepLinter", "checkov"}
	errEmptyTree = errors.New("catalogue is empty")
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// parseID splits FND-<GROUP>-<NNN> and returns the group.
func parseID(id string) (string, bool) {
	parts := strings.Split(id, "-")
	if len(parts) != 3 || parts[0] != "FND" || parts[1] == "" || len(parts[2]) != 3 {
		return "", false
	}
	for _, r := range parts[1] {
		if r < 'A' || r > 'Z' {
			return "", false
		}
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return parts[1], true
}

// Load reads every <root>/<group>/<ID>.yaml in fsys. Rules are returned sorted by ID.
//
// Loading is strict. Unknown YAML fields, duplicate keys, empty files, files with more than one
// YAML document, symlinks, files over 1 MiB, files that are not .yaml, and files not exactly two
// levels below root are reported as an *InvalidError. I/O failures are returned as is.
func Load(ctx context.Context, fsys fs.FS, root string) ([]Rule, error) {
	var rules []Rule
	var problems []error
	bad := func(format string, a ...any) { problems = append(problems, fmt.Errorf(format, a...)) }

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			bad("%s: symlinks are not allowed in the catalogue", p)
			return nil
		}
		if path.Ext(p) != ".yaml" {
			bad("%s: unexpected file; the catalogue holds only .yaml rule files", p)
			return nil
		}
		rel := p
		if root != "." {
			rel = strings.TrimPrefix(p, strings.TrimSuffix(root, "/")+"/")
		}
		if strings.Count(rel, "/") != 1 {
			bad("%s: rule files must be exactly <group>/<ID>.yaml below the catalogue root", p)
			return nil
		}
		f, err := fsys.Open(p)
		if err != nil {
			return err
		}
		b, err := func() ([]byte, error) {
			data, readErr := io.ReadAll(io.LimitReader(f, maxRuleFileBytes+1))
			closeErr := f.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			return data, nil
		}()
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(b) > maxRuleFileBytes {
			bad("%s: file exceeds %d bytes", p, maxRuleFileBytes)
			return nil
		}
		var r Rule
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(&r); err != nil {
			if errors.Is(err, io.EOF) {
				bad("%s: file is empty", p)
			} else {
				bad("%s: %w", p, err)
			}
			return nil
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			bad("%s: more than one YAML document in a rule file", p)
			return nil
		}
		if want := strings.TrimSuffix(path.Base(p), ".yaml"); want != r.ID {
			bad("%s: file name must equal rule id %q, got id %q", p, want, r.ID)
		}
		if g, ok := parseID(r.ID); ok && path.Base(path.Dir(p)) != strings.ToLower(g) {
			bad("%s: rule %s must live in directory %q", p, r.ID, strings.ToLower(g))
		}
		rules = append(rules, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, &InvalidError{Problems: problems}
	}
	slices.SortFunc(rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return rules, nil
}

// Options controls validation strictness.
type Options struct {
	// Phase0Gate additionally fails any rule still in the proposed state:
	// the Phase 0 Definition of Done requires every rule to have a source or an opinion label,
	// and a reuse/wrap/adapt/native/drop decision.
	Phase0Gate bool
	// Now supplies the date used to reject future source verification dates.
	// The zero value uses the current UTC time.
	Now time.Time
}

// Validate returns one error per problem found, joined. A nil result means the catalogue is valid.
func Validate(rules []Rule, opt Options) error {
	if len(rules) == 0 {
		return errEmptyTree
	}
	rules = append([]Rule(nil), rules...)
	slices.SortFunc(rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	var errs []error
	seen := map[string]bool{}
	add := func(r Rule, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", r.ID, fmt.Sprintf(format, a...)))
	}
	for _, r := range rules {
		group, ok := parseID(r.ID)
		if !ok {
			errs = append(errs, fmt.Errorf("%q: id must match FND-<GROUP>-<NNN>", r.ID))
			continue
		}
		if seen[r.ID] {
			add(r, "duplicate rule id")
		}
		seen[r.ID] = true
		if r.Group != group {
			add(r, "group %q does not match id group %q", r.Group, group)
		}
		if r.Version < 1 {
			add(r, "version must be >= 1")
		}
		if strings.TrimSpace(r.Title) == "" {
			add(r, "title is required")
		}
		if !statuses[r.Status] {
			add(r, "unknown status %q", r.Status)
		}
		if len(r.Phases) == 0 {
			add(r, "phases is required")
		}
		for _, l := range []struct {
			name string
			list []string
		}{{"phases", r.Phases}, {"inputs", r.Inputs}, {"basis", r.Basis}} {
			if len(slices.Compact(slices.Sorted(slices.Values(l.list)))) != len(l.list) {
				add(r, "%s contains duplicate entries", l.name)
			}
		}
		for _, p := range r.Phases {
			if !phaseValues[p] {
				add(r, "unknown phase %q", p)
			}
		}
		for _, v := range r.Inputs {
			if !inputPlanes[v] {
				add(r, "unknown input plane %q", v)
			}
		}
		for _, v := range r.Basis {
			if !basisValues[v] {
				add(r, "unknown basis %q", v)
			}
		}
		if r.Category != "" && !categories[r.Category] {
			add(r, "unknown category %q", r.Category)
		}
		if !pillars[r.Pillar] {
			add(r, "unknown pillar %q", r.Pillar)
		}
		validateSources(r, opt, add)
		validateOrigins(r, add)
		if r.Status != StatusProposed {
			validateResearched(r, add)
		}
		if opt.Phase0Gate && r.Status == StatusProposed {
			add(r, "still proposed; Phase 0 requires a verified source or product-opinion label and an overlap decision")
		}
	}
	return errors.Join(errs...)
}

func validateSources(r Rule, opt Options, add func(Rule, string, ...any)) {
	today := opt.Now.UTC()
	if today.IsZero() {
		today = time.Now().UTC()
	}
	for i, s := range r.Sources {
		u, err := url.Parse(s.URL)
		if err != nil || strings.ContainsAny(s.URL, "\r\n\t") || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
			add(r, "sources[%d].url must be an absolute https URL without user information", i)
		}
		verified, err := time.Parse("2006-01-02", s.LastVerified)
		if err != nil {
			add(r, "sources[%d].lastVerified must be a valid YYYY-MM-DD date, got %q", i, s.LastVerified)
		} else if verified.After(today) {
			add(r, "sources[%d].lastVerified must not be in the future, got %q", i, s.LastVerified)
		}
	}
}

func validateOrigins(r Rule, add func(Rule, string, ...any)) {
	seen := map[string]bool{}
	for i, origin := range r.Origins {
		if origin.System != "xf" {
			add(r, "origins[%d].system must be \"xf\", got %q", i, origin.System)
		}

		if strings.TrimSpace(origin.ID) == "" {
			add(r, "origins[%d].id is required", i)
		}
		key := origin.System + "\x00" + origin.ID
		if seen[key] {
			add(r, "origins contains duplicate %s id %q", origin.System, origin.ID)
		}
		seen[key] = true
	}
}

// ValidateOriginInventory verifies that the reviewed origin inventory and the
// catalogue are a one-to-one match. It reports missing, duplicate and
// unreviewed origins deterministically. A caller can therefore keep the
// inventory in a separately reviewed manifest without coupling canonical rule
// IDs to legacy XF IDs.
func ValidateOriginInventory(rules []Rule, inventory []Origin) error {
	rules = append([]Rule(nil), rules...)
	slices.SortFunc(rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	inventory = append([]Origin(nil), inventory...)
	slices.SortFunc(inventory, func(a, b Origin) int {
		if bySystem := strings.Compare(a.System, b.System); bySystem != 0 {
			return bySystem
		}
		return strings.Compare(a.ID, b.ID)
	})
	want := make(map[string]bool, len(inventory))
	var errs []error
	key := func(o Origin) string { return o.System + "\x00" + o.ID }
	label := func(o Origin) string { return o.System + ":" + o.ID }
	for _, o := range inventory {
		k := key(o)
		if want[k] {
			errs = append(errs, fmt.Errorf("origin inventory contains duplicate %s", label(o)))
		}
		want[k] = true
	}
	got := map[string]string{}
	for _, r := range rules {
		origins := append([]Origin(nil), r.Origins...)
		slices.SortFunc(origins, func(a, b Origin) int {
			if bySystem := strings.Compare(a.System, b.System); bySystem != 0 {
				return bySystem
			}
			return strings.Compare(a.ID, b.ID)
		})
		for _, o := range origins {
			k := key(o)
			if previous, ok := got[k]; ok {
				errs = append(errs, fmt.Errorf("origin %s is mapped by both %s and %s", label(o), previous, r.ID))
			} else {
				got[k] = r.ID
			}
			if !want[k] {
				errs = append(errs, fmt.Errorf("%s maps unreviewed origin %s", r.ID, label(o)))
			}
		}
	}
	missing := make([]string, 0)
	for k := range want {
		if _, ok := got[k]; !ok {
			missing = append(missing, k)
		}
	}
	slices.Sort(missing)
	for _, k := range missing {
		parts := strings.SplitN(k, "\x00", 2)
		errs = append(errs, fmt.Errorf("origin inventory entry %s:%s is not mapped", parts[0], parts[1]))
	}
	return errors.Join(errs...)
}

func validateResearched(r Rule, add func(Rule, string, ...any)) {
	structured := r.Overlap.hasStructuredResearch()
	hasUnresearched := false
	for _, tool := range overlapTools {
		research := r.Overlap.ToolResearch(tool)
		if structured && len(r.Overlap.legacyMatches(tool)) != 0 {
			add(r, "overlap.%s legacy matches cannot be combined with overlap.research", tool)
		}
		if research.State != ResearchUnresearched && research.State != ResearchSearchedMatch && research.State != ResearchSearchedNone {
			add(r, "overlap.research.%s.state must be searched-match|searched-none|unresearched, got %q", tool, research.State)
		}
		if research.State == ResearchSearchedMatch && len(research.Matches) == 0 {
			add(r, "overlap.research.%s searched-match requires at least one match", tool)
		}
		if research.State != ResearchSearchedMatch && len(research.Matches) != 0 {
			add(r, "overlap.research.%s %s forbids matches", tool, research.State)
		}
		if research.State == ResearchUnresearched {
			hasUnresearched = true
		}
	}
	if structured && hasUnresearched && !r.Overlap.Provisional {
		add(r, "overlap.provisional must be true while any tool is unresearched")
	}
	if !decisions[r.Overlap.Decision] {
		add(r, "overlap.decision must be one of reuse|wrap|adapt|native|drop, got %q", r.Overlap.Decision)
	}
	if strings.TrimSpace(r.Overlap.Rationale) == "" {
		add(r, "overlap.rationale is required")
	}
	if !coverages[r.Overlap.Coverage] {
		add(r, "overlap.coverage must be none|partial|full, got %q", r.Overlap.Coverage)
	}
	if r.Status == StatusDropped {
		if r.Overlap.Decision != "drop" {
			add(r, "dropped rule must have overlap.decision drop")
		}
		return
	}
	if r.Status == StatusVerified || r.Status == StatusImplemented {
		if len(r.Sources) == 0 {
			add(r, "%s rule needs at least one source", r.Status)
		}
	}
	if r.Status == StatusProductOpinion && !contains(r.Basis, "opinion") {
		add(r, "product-opinion rule must include basis \"opinion\"")
	}
	if len(r.Inputs) == 0 {
		add(r, "inputs is required")
	}
	if !categories[r.Category] {
		add(r, "category must be must-have|nice-to-have")
	}
	for _, sv := range []struct{ name, value string }{{"dev", r.Severity.Dev}, {"test", r.Severity.Test}, {"prod", r.Severity.Prod}} {
		if !severities[sv.value] {
			add(r, "severity.%s must be info|warning|error, got %q", sv.name, sv.value)
		}
	}
	if contains(r.Basis, "platform") {
		if r.Severity.Dev != "error" || r.Severity.Test != "error" || r.Severity.Prod != "error" {
			add(r, "platform-basis rules must be error in every profile (PRD section 9)")
		}
	}
	if !confidences[r.Evidence.Confidence] {
		add(r, "evidence.confidence must be certain|likely|uncertain")
	}
	if strings.TrimSpace(r.Evidence.Description) == "" {
		add(r, "evidence.description is required")
	}
	if strings.TrimSpace(r.Recommendation) == "" {
		add(r, "recommendation is required")
	}
	if strings.TrimSpace(r.Fix) == "" {
		add(r, "fix is required")
	}
	if strings.TrimSpace(r.Testability) == "" {
		add(r, "testability is required")
	}
	if strings.TrimSpace(r.Description) == "" {
		add(r, "description is required")
	}
	if len(r.Tests.Positive) == 0 || len(r.Tests.Negative) == 0 || len(r.Tests.Skipped) == 0 {
		add(r, "tests.positive, tests.negative and tests.skipped need at least one scenario each")
	}
	if strings.TrimSpace(r.Implementation.Package) == "" {
		add(r, "implementation.package is required")
	}
	validateCompatibility(r, add)
	if !owners[r.Implementation.Owner] {
		add(r, "implementation.owner must be one of native|psrule|azure-policy|defender|advisor|bicep|checkov|adapter")
	}
	switch r.Overlap.Decision {
	case "native", "adapt":
		if r.Implementation.Owner != "native" {
			add(r, "decision %s requires implementation.owner native (our own code), got %q", r.Overlap.Decision, r.Implementation.Owner)
		}
	case "reuse", "wrap":
		if r.Implementation.Owner == "native" {
			add(r, "decision %s requires an external implementation.owner, got native", r.Overlap.Decision)
		}
	}
}

func validateCompatibility(r Rule, add func(Rule, string, ...any)) {
	c := r.Compatibility
	if c.Azd.Empty() && len(c.APIVersions) == 0 && len(c.Extensions) == 0 && strings.TrimSpace(c.Notes) == "" {
		add(r, "compatibility must name an azd range, extension ranges or API versions, or say in notes why the rule has no versioned dependency")
	}
	preview := false
	if !c.Azd.Empty() {
		if !validVersionRange(c.Azd) {
			add(r, "compatibility.azd must be an exact version or a semver range with ordered lower and upper endpoints, got %q", c.Azd.String())
		}
		if c.Azd.hasPrerelease() {
			preview = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.Extensions)) {
		rng := c.Extensions[name]
		if !validVersionRange(rng) {
			add(r, "compatibility.extensions[%s] must be an exact version or a semver range with ordered lower and upper endpoints, got %q", name, rng.String())
		}
		if rng.hasPrerelease() {
			preview = true
		}
	}
	seen := make(map[APIVersion]int, len(c.APIVersions))
	for i, v := range c.APIVersions {
		if err := validateAPIVersion(v); err != nil {
			add(r, "compatibility.apiVersions[%d]: %v", i, err)
		}
		if previous, ok := seen[v]; ok {
			add(r, "compatibility.apiVersions[%d] duplicates apiVersions[%d]", i, previous)
		} else {
			seen[v] = i
		}
		if strings.HasSuffix(v.Version, "-preview") {
			preview = true
		}
	}
	if preview && !c.Preview {
		add(r, "compatibility.preview must be true: a preview API version or prerelease extension is listed")
	}
}

func validateAPIVersion(v APIVersion) error {
	if v.Plane != APIPlaneManagement && v.Plane != APIPlaneData {
		return fmt.Errorf("plane must be management|data, got %q", v.Plane)
	}
	if v.Operation != "" && !validAPITarget(v.Operation, false) {
		return fmt.Errorf("operation must be one machine-readable operation, got %q", v.Operation)
	}
	switch v.Plane {
	case APIPlaneManagement:
		if !validAPITarget(v.Provider, false) {
			return fmt.Errorf("management plane requires one provider")
		}
		if !validAPITarget(v.ResourceType, true) {
			return fmt.Errorf("management plane requires one resourceType")
		}
		if v.Service != "" || v.EndpointFamily != "" {
			return fmt.Errorf("management plane forbids service and endpointFamily")
		}
	case APIPlaneData:
		if !validAPITarget(v.Service, false) {
			return fmt.Errorf("data plane requires one service")
		}
		if !validAPITarget(v.EndpointFamily, true) {
			return fmt.Errorf("data plane requires one endpointFamily")
		}
		if v.Provider != "" || v.ResourceType != "" {
			return fmt.Errorf("data plane forbids provider and resourceType")
		}
	}
	if !validExactAPIVersion(v.Version) {
		return fmt.Errorf("version must be an exact, valid YYYY-MM-DD or YYYY-MM-DD-preview value, got %q", v.Version)
	}
	return nil
}

func validAPITarget(value string, allowPath bool) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	parts := []string{value}
	if allowPath {
		parts = strings.Split(value, "/")
	} else if strings.Contains(value, "/") {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		separator := false
		for i, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				separator = false
				continue
			}
			if i > 0 && r >= '0' && r <= '9' {
				separator = false
				continue
			}
			if i > 0 && !separator && (r == '.' || r == '-' || r == '_') {
				separator = true
				continue
			}
			return false
		}
		if separator {
			return false
		}
	}
	return true
}

func validExactAPIVersion(value string) bool {
	const preview = "-preview"
	date := strings.TrimSuffix(value, preview)
	if len(date) != len("2006-01-02") || date[:4] == "0000" ||
		len(value) != len(date) && len(value) != len(date)+len(preview) {
		return false
	}
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && parsed.Format("2006-01-02") == date
}

func contains(s []string, v string) bool { return slices.Contains(s, v) }

type semVersion struct {
	major, minor, patch string
	pre                 []string
}

func parseSemVer(s string) (semVersion, bool) {
	if strings.HasPrefix(s, "v") || s == "" || strings.TrimSpace(s) != s {
		return semVersion{}, false
	}
	coreAndBuild := strings.SplitN(s, "+", 2)
	if len(coreAndBuild) == 2 && !validIdentifiers(coreAndBuild[1], false) {
		return semVersion{}, false
	}
	coreAndPre := strings.SplitN(coreAndBuild[0], "-", 2)
	core := strings.Split(coreAndPre[0], ".")
	if len(core) != 3 {
		return semVersion{}, false
	}
	for _, part := range core {
		if !numericIdentifier(part) || len(part) > 1 && part[0] == '0' {
			return semVersion{}, false
		}
	}
	var pre []string
	if len(coreAndPre) == 2 {
		if !validIdentifiers(coreAndPre[1], true) {
			return semVersion{}, false
		}
		pre = strings.Split(coreAndPre[1], ".")
	}
	return semVersion{major: core[0], minor: core[1], patch: core[2], pre: pre}, true
}

func validIdentifiers(s string, rejectLeadingZero bool) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for _, c := range id {
			if !('0' <= c && c <= '9') {
				numeric = false
			}
			if !(('0' <= c && c <= '9') || ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || c == '-') {
				return false
			}
		}
		if rejectLeadingZero && numeric && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

func compareSemVer(a, b semVersion) int {
	for _, pair := range [][2]string{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if len(pair[0]) < len(pair[1]) || len(pair[0]) == len(pair[1]) && pair[0] < pair[1] {
			return -1
		}
		if len(pair[0]) > len(pair[1]) || len(pair[0]) == len(pair[1]) && pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < min(len(a.pre), len(b.pre)); i++ {
		if a.pre[i] == b.pre[i] {
			continue
		}
		aNumeric, bNumeric := numericIdentifier(a.pre[i]), numericIdentifier(b.pre[i])
		switch {
		case aNumeric && bNumeric && len(a.pre[i]) < len(b.pre[i]):
			return -1
		case aNumeric && bNumeric && len(a.pre[i]) > len(b.pre[i]):
			return 1
		case aNumeric && bNumeric && a.pre[i] < b.pre[i]:
			return -1
		case aNumeric && bNumeric:
			return 1
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		case a.pre[i] < b.pre[i]:
			return -1
		default:
			return 1
		}
	}

	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	default:
		return 0
	}
}

func numericIdentifier(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func (r VersionRange) bounds() (exact, minimum, maximum *semVersion, minInclusive, maxInclusive, ok bool) {
	if r.legacy != "" {
		fields := strings.Fields(r.legacy)
		if len(fields) == 1 {
			version := strings.TrimPrefix(fields[0], "=")
			if strings.HasPrefix(version, ">") || strings.HasPrefix(version, "<") {
				return nil, nil, nil, false, false, false
			}
			v, valid := parseSemVer(version)
			return &v, nil, nil, false, false, valid
		}
		if len(fields) != 2 {
			return nil, nil, nil, false, false, false
		}
		lowerOp, lowerVersion := splitComparator(fields[0])
		upperOp, upperVersion := splitComparator(fields[1])
		if lowerOp != ">" && lowerOp != ">=" || upperOp != "<" && upperOp != "<=" {
			return nil, nil, nil, false, false, false
		}
		lo, lok := parseSemVer(lowerVersion)
		hi, hok := parseSemVer(upperVersion)
		return nil, &lo, &hi, lowerOp == ">=", upperOp == "<=", lok && hok
	}
	if r.Exact != "" {
		if r.Minimum != "" || r.Maximum != "" || r.IncludeMinimum || r.IncludeMaximum {
			return nil, nil, nil, false, false, false
		}
		v, valid := parseSemVer(r.Exact)
		return &v, nil, nil, false, false, valid
	}
	if r.Minimum == "" || r.Maximum == "" {
		return nil, nil, nil, false, false, false
	}
	lo, lok := parseSemVer(r.Minimum)
	hi, hok := parseSemVer(r.Maximum)
	return nil, &lo, &hi, r.IncludeMinimum, r.IncludeMaximum, lok && hok
}

func splitComparator(s string) (string, string) {
	for _, op := range []string{">=", "<=", ">", "<"} {
		if strings.HasPrefix(s, op) {
			return op, strings.TrimPrefix(s, op)
		}
	}
	return "", s
}

func validVersionRange(r VersionRange) bool {
	exact, min, max, minInc, maxInc, ok := r.bounds()
	if !ok || exact != nil {
		return ok
	}
	order := compareSemVer(*min, *max)
	return order < 0 || order == 0 && minInc && maxInc
}

func (r VersionRange) hasPrerelease() bool {
	exact, min, max, _, _, ok := r.bounds()
	if !ok {
		return false
	}
	if exact != nil {
		return len(exact.pre) > 0
	}
	return len(min.pre) > 0 || len(max.pre) > 0
}
