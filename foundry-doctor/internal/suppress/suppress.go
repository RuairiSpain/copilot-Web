// Package suppress reads suppression files and applies them to findings.
//
// Every suppression needs a reason, an owner and an expiry date. A live
// suppression marks matching findings (Finding.Suppressed). An expired
// suppression no longer hides anything and yields an error finding with the
// engine-internal rule ID ExpiredRuleID. That ID is not a catalogue rule (the
// catalogue only holds product rules); it is documented here and in the
// report contract.
package suppress

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Version is the only supported suppression file schema version.
const Version = 1

// DefaultPath is the conventional repo-relative suppression file location.
const DefaultPath = ".foundry-doctor/suppressions.yaml"

// MaxBytes bounds the suppression file size accepted by Parse.
const MaxBytes = 1 << 20

// ExpiredRuleID is the engine-internal rule ID for an expired suppression.
const ExpiredRuleID = "FND-SUPPRESS-001"

// ExpiredRuleVersion is the version of the expired-suppression check.
const ExpiredRuleVersion = 1

// DateLayout is the expiry date format (inclusive: valid through that day, UTC).
const DateLayout = "2006-01-02"

var (
	ruleIDRE      = regexp.MustCompile(`^FND-[A-Z]+-[0-9]{3}$`)
	fingerprintRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Entry is one suppression. RuleID and/or Fingerprint select findings; Path
// optionally narrows by location file. At least one of RuleID or Fingerprint
// is required.
type Entry struct {
	RuleID      string `yaml:"ruleId,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty"`
	Path        string `yaml:"path,omitempty"`
	Reason      string `yaml:"reason"`
	Owner       string `yaml:"owner"`
	Expires     string `yaml:"expires"`
}

// File is the serialised suppression list.
type File struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

// Parse decodes and validates a suppression file. Unknown keys, missing
// reason/owner/expiry, bad dates and bad selectors are errors.
func Parse(data []byte) (*File, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("suppress: file exceeds %d bytes", MaxBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("suppress: empty document")
		}
		return nil, fmt.Errorf("suppress: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("suppress: multiple documents are not allowed")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate checks every entry.
func (f *File) Validate() error {
	if f.Version != Version {
		return fmt.Errorf("suppress: unsupported version %d (want %d)", f.Version, Version)
	}
	for i, e := range f.Entries {
		if err := e.validate(); err != nil {
			return fmt.Errorf("suppress: entries[%d]: %w", i, err)
		}
	}
	return nil
}

func (e Entry) validate() error {
	if e.RuleID == "" && e.Fingerprint == "" {
		return errors.New("ruleId or fingerprint is required")
	}
	if e.RuleID != "" && !ruleIDRE.MatchString(e.RuleID) {
		return fmt.Errorf("invalid ruleId %q", e.RuleID)
	}
	if e.Fingerprint != "" && !fingerprintRE.MatchString(e.Fingerprint) {
		return fmt.Errorf("invalid fingerprint %q", e.Fingerprint)
	}
	if strings.TrimSpace(e.Reason) == "" {
		return errors.New("reason is required")
	}
	if strings.TrimSpace(e.Owner) == "" {
		return errors.New("owner is required")
	}
	if strings.TrimSpace(e.Expires) == "" {
		return errors.New("expires is required")
	}
	if _, err := time.Parse(DateLayout, e.Expires); err != nil {
		return fmt.Errorf("expires %q must be YYYY-MM-DD", e.Expires)
	}
	return nil
}

// Marshal renders the file deterministically (sorted by selector).
func (f *File) Marshal() ([]byte, error) {
	out := File{Version: Version, Entries: append([]Entry{}, f.Entries...)}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Fingerprint != b.Fingerprint {
			return a.Fingerprint < b.Fingerprint
		}
		return a.Path < b.Path
	})
	if err := out.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("suppress: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("suppress: %w", err)
	}
	return buf.Bytes(), nil
}

// Expired reports whether the entry is past its expiry at now. The expiry day
// itself is still valid. An unparsable date counts as expired (fail closed).
func (e Entry) Expired(now time.Time) bool {
	d, err := time.Parse(DateLayout, e.Expires)
	if err != nil {
		return true
	}
	n := now.UTC()
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	return today.After(d)
}

func (e Entry) matches(f sdk.Finding) bool {
	if e.Fingerprint != "" && e.Fingerprint != f.Fingerprint {
		return false
	}
	if e.RuleID != "" && e.RuleID != f.RuleID {
		return false
	}
	if e.Path != "" && e.Path != f.Location.File {
		return false
	}
	return true
}

// Result summarises Apply.
type Result struct {
	// Suppressed counts findings hidden by live suppressions.
	Suppressed int
	// Expired lists the indexes of expired entries, ascending.
	Expired []int
	// Findings holds one error finding per expired entry.
	Findings []sdk.Finding
}

// Apply marks findings matched by live suppressions and reports expired ones.
// now is injected for determinism. Expired entries never suppress. A nil file
// changes nothing. fs is modified in place; the returned findings must be
// appended by the caller.
func (f *File) Apply(now time.Time, fs []sdk.Finding) Result {
	var res Result
	if f == nil {
		return res
	}
	for idx, e := range f.Entries {
		if e.Expired(now) {
			res.Expired = append(res.Expired, idx)
			res.Findings = append(res.Findings, expiredFinding(e))
			continue
		}
		for i := range fs {
			if fs[i].Suppressed != nil || !e.matches(fs[i]) {
				continue
			}
			fs[i].Suppressed = &sdk.Suppression{Reason: e.Reason, Owner: e.Owner, Expires: e.Expires}
			res.Suppressed++
		}
	}
	findings.Sort(res.Findings)
	return res
}

func expiredFinding(e Entry) sdk.Finding {
	sel := e.RuleID
	if e.Fingerprint != "" {
		sel = strings.TrimSpace(sel + " " + e.Fingerprint)
	}
	f := sdk.Finding{
		RuleID:      ExpiredRuleID,
		RuleVersion: ExpiredRuleVersion,
		Severity:    sdk.SeverityError,
		Category:    sdk.CategoryMustHave,
		Resource:    sdk.ResourceRef{Type: "suppression", Name: sel},
		Evidence:    fmt.Sprintf("suppression for %s owned by %s expired on %s", sel, e.Owner, e.Expires),
		Recommendation: "Fix the underlying finding, or renew the suppression with a new expiry " +
			"and a current reason.",
		Confidence: sdk.ConfidenceHigh,
	}
	f.Fingerprint = findings.Fingerprint(f)
	return f
}
