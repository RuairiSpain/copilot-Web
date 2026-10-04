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

// Compatibility declares the tool versions a rule was verified against.
type Compatibility struct {
	Azd         string            `yaml:"azd"`
	Extensions  map[string]string `yaml:"extensions"`
	APIVersions []string          `yaml:"apiVersions"`
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

// Overlap records how the rule relates to lower-level tools.
type Overlap struct {
	Decision    string   `yaml:"decision"`
	Coverage    string   `yaml:"coverage"`
	Rationale   string   `yaml:"rationale"`
	PSRule      []string `yaml:"psrule"`
	AzurePolicy []string `yaml:"azurePolicy"`
	Defender    []string `yaml:"defender"`
	Advisor     []string `yaml:"advisor"`
	BicepLinter []string `yaml:"bicepLinter"`
	Checkov     []string `yaml:"checkov"`
}

// Implementation names who runs the check.
type Implementation struct {
	Owner   string `yaml:"owner"`
	Package string `yaml:"package"`
}

// Tests lists the scenarios the implementation must cover.
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
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		if strings.Count(rel, "/") != 1 {
			bad("%s: rule files must be exactly <group>/<ID>.yaml below the catalogue root", p)
			return nil
		}
		f, err := fsys.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, maxRuleFileBytes+1))
		if err != nil {
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
}

// Validate returns one error per problem found, joined. A nil result means the catalogue is valid.
func Validate(rules []Rule, opt Options) error {
	if len(rules) == 0 {
		return errEmptyTree
	}
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
		validateSources(r, add)
		if r.Status != StatusProposed {
			validateResearched(r, add)
		}
		if opt.Phase0Gate && r.Status == StatusProposed {
			add(r, "still proposed; Phase 0 requires a verified source or product-opinion label and an overlap decision")
		}
	}
	return errors.Join(errs...)
}

func validateSources(r Rule, add func(Rule, string, ...any)) {
	for i, s := range r.Sources {
		if !strings.HasPrefix(s.URL, "https://") {
			add(r, "sources[%d].url must be https", i)
		}
		if _, err := time.Parse("2006-01-02", s.LastVerified); err != nil {
			add(r, "sources[%d].lastVerified must be a valid YYYY-MM-DD date, got %q", i, s.LastVerified)
		}
	}
}

func validateResearched(r Rule, add func(Rule, string, ...any)) {
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
	if strings.TrimSpace(r.Description) == "" {
		add(r, "description is required")
	}
	if len(r.Tests.Positive) == 0 || len(r.Tests.Negative) == 0 {
		add(r, "tests.positive and tests.negative need at least one scenario each")
	}
	if strings.TrimSpace(r.Implementation.Package) == "" {
		add(r, "implementation.package is required")
	}
	if r.Compatibility.Azd == "" && len(r.Compatibility.APIVersions) == 0 && len(r.Compatibility.Extensions) == 0 {
		add(r, "compatibility must name an azd version, extension versions or API versions")
	}
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

func contains(s []string, v string) bool { return slices.Contains(s, v) }
