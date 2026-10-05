package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"sort"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	"github.com/ruairispain/copilot-web/foundry-doctor/rules/profiles"
	"go.yaml.in/yaml/v3"
)

// Curated profile names (ADR-010 decision 6).
const (
	ProfileDev     = "foundry-dev"
	ProfileTest    = "foundry-test"
	ProfilePROD    = "foundry-prod"
	DefaultProfile = ProfileDev
)

// ProfileNames lists the curated profiles in severity order.
func ProfileNames() []string { return []string{ProfileDev, ProfileTest, ProfilePROD} }

// NormalizeProfile maps dev|test|prod and foundry-dev|foundry-test|foundry-prod to the long form.
// Any other value, including the empty string, is an exit-2 error.
func NormalizeProfile(s string) (string, error) {
	switch s {
	case "dev", ProfileDev:
		return ProfileDev, nil
	case "test", ProfileTest:
		return ProfileTest, nil
	case "prod", ProfilePROD:
		return ProfilePROD, nil
	}
	return "", &Error{Msg: fmt.Sprintf("unknown profile %q: want dev, test, prod, foundry-dev, foundry-test or foundry-prod", s)}
}

// Profile is a curated severity table. It carries severities only; organisation values live in policy
// (ADR-007 decision 4).
type Profile struct {
	Name       string
	Severities map[string]sdk.Severity // rule ID -> severity in this profile
}

// Severity returns the severity of a rule in this profile. ok is false for a rule the profile does not list.
func (p Profile) Severity(ruleID string) (sdk.Severity, bool) {
	s, ok := p.Severities[ruleID]
	return s, ok
}

// RuleIDs returns the listed rule IDs in sorted order.
func (p Profile) RuleIDs() []string { return sortedKeys(p.Severities) }

// ProfileSet is the set of curated profiles, keyed by long name.
type ProfileSet struct{ byName map[string]Profile }

// Get returns a profile by long name.
func (s ProfileSet) Get(name string) (Profile, bool) { p, ok := s.byName[name]; return p, ok }

// Names returns the profile names in sorted order.
func (s ProfileSet) Names() []string { return sortedKeys(s.byName) }

// profileFile is the on-disk shape of rules/profiles/<name>.yaml.
type profileFile struct {
	Name       string            `yaml:"name"`
	Version    int               `yaml:"version"`
	Severities map[string]string `yaml:"severities"`
}

var ruleIDRe = regexp.MustCompile(`^FND-[A-Z]+-[0-9]{3}$`)

const maxProfileBytes = 256 << 10

// LoadProfiles reads foundry-dev.yaml, foundry-test.yaml and foundry-prod.yaml from the root of fsys.
// Loading is strict: unknown keys, duplicate keys, a name that differs from the file name, an unknown severity
// or a malformed rule ID is an error, and all three profiles must list the same rules.
//
// The profiles are generated from the rule catalogue (see TestGenerateProfiles) rather than read from it at
// run time. Reason: the profile files are the reviewable, diffable "curated profile" deliverable (PRD section 9),
// they let a later release curate a profile without editing rule metadata, and the doctor binary then needs
// only the embedded profile files and not the whole catalogue tree. TestProfilesUpToDate and
// TestProfileSeveritiesMatchCatalogue fail the build if they drift.
func LoadProfiles(fsys fs.FS) (ProfileSet, error) {
	set := ProfileSet{byName: map[string]Profile{}}
	var problems []error
	for _, name := range ProfileNames() {
		file := name + ".yaml"
		f, err := fsys.Open(file)
		if err != nil {
			return ProfileSet{}, fmt.Errorf("load profile %s: %w", name, err)
		}
		b, err := io.ReadAll(io.LimitReader(f, maxProfileBytes+1))
		f.Close()
		if err != nil {
			return ProfileSet{}, fmt.Errorf("load profile %s: %w", name, err)
		}
		if len(b) > maxProfileBytes {
			problems = append(problems, fmt.Errorf("%s: file exceeds %d bytes", file, maxProfileBytes))
			continue
		}
		p, err := parseProfile(file, name, b)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		set.byName[name] = p
	}
	if len(problems) > 0 {
		return ProfileSet{}, errors.Join(problems...)
	}
	base := set.byName[ProfileDev].RuleIDs()
	for _, name := range ProfileNames()[1:] {
		if !slices.Equal(base, set.byName[name].RuleIDs()) {
			problems = append(problems, fmt.Errorf("%s.yaml: lists a different set of rules than %s.yaml", name, ProfileDev))
		}
	}
	if len(problems) > 0 {
		return ProfileSet{}, errors.Join(problems...)
	}
	return set, nil
}

func parseProfile(file, want string, b []byte) (Profile, error) {
	var pf profileFile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", file, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Profile{}, fmt.Errorf("%s: more than one YAML document", file)
	}
	if pf.Name != want {
		return Profile{}, fmt.Errorf("%s: name must be %q, got %q", file, want, pf.Name)
	}
	if pf.Version != 1 {
		return Profile{}, fmt.Errorf("%s: unsupported version %d", file, pf.Version)
	}
	if len(pf.Severities) == 0 {
		return Profile{}, fmt.Errorf("%s: severities is empty", file)
	}
	p := Profile{Name: want, Severities: make(map[string]sdk.Severity, len(pf.Severities))}
	ids := sortedKeys(pf.Severities)
	for _, id := range ids {
		if !ruleIDRe.MatchString(id) {
			return Profile{}, fmt.Errorf("%s: %q is not a rule ID (FND-<GROUP>-<NNN>)", file, id)
		}
		sev, err := sdk.ParseSeverity(pf.Severities[id])
		if err != nil {
			return Profile{}, fmt.Errorf("%s: %s: %w", file, id, err)
		}
		p.Severities[id] = sev
	}
	return p, nil
}

// DefaultProfiles loads the curated profiles embedded in the binary (rules/profiles).
func DefaultProfiles() (ProfileSet, error) { return LoadProfiles(profiles.FS) }

// RenderProfile returns the canonical file content of a profile: sorted rule IDs, one per line.
// The generator test and the up-to-date test both use it, so the format is defined in one place.
func RenderProfile(name string, sev map[string]sdk.Severity) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# GENERATED from rules/catalog by `go test ./internal/config -run TestGenerateProfiles -update`. Do not edit by hand.\n")
	fmt.Fprintf(&b, "# Severities only: no organisation values (ADR-007 decision 4). Platform-basis rules are error in every profile.\n")
	fmt.Fprintf(&b, "name: %s\nversion: 1\nseverities:\n", name)
	ids := make([]string, 0, len(sev))
	for id := range sev {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "  %s: %s\n", id, sev[id])
	}
	return b.Bytes()
}
