// Package suppress reads suppression files and applies them to findings.
//
// A suppression accepts one finding, or the findings of one rule on a resource, with a
// required reason, owner and expiry date. It targets either an exact fingerprint
// (ADR-008) or a resource selector (kind, type, name, pointer; every set field must
// match, type compared case-insensitively).
//
// Expiry rule: a suppression is valid through the END of its expiry day, evaluated in
// UTC. With expires 2026-12-31 it still hides a finding at 2026-12-31T23:59:59Z and no
// longer hides it from 2027-01-01T00:00:00Z. The clock is injected: Apply never reads
// the wall clock. Callers in other time zones must pass the instant; the calendar day
// is always taken from now.UTC().
//
// An expired suppression does NOT hide its finding. It yields an error-severity
// diagnostic finding FND-SYS-SUPPRESSION-EXPIRED (ADR-009). A suppression that matches
// no finding is reported as unused (informational). Findings that are themselves about
// suppressions (FND-SYS-SUPPRESSION-*) cannot be suppressed.
//
// Files are untrusted input: Read refuses symlinks, paths outside the root, oversize
// files, unknown keys, multi-document YAML and invalid entries. Validation reports a
// precise error for each missing reason, owner or expiry.
package suppress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Version is the only suppression file version this package reads.
const Version = 1

// MaxFileBytes is the largest suppression file Read accepts.
const MaxFileBytes = 1 << 20

// Diagnostic rule IDs reserved by ADR-009.
const (
	RuleExpired = "FND-SYS-SUPPRESSION-EXPIRED"
	RuleInvalid = "FND-SYS-SUPPRESSION-INVALID"
)

// Errors returned (wrapped) by this package.
var (
	ErrInvalid    = errors.New("invalid suppressions")
	ErrUnsafePath = errors.New("unsafe suppressions path")
	ErrTooLarge   = errors.New("suppressions file too large")
)

var fingerprintRE = regexp.MustCompile(`^fp1:[0-9a-f]{32}$`)

// File is the suppression file. Source is the project-relative path it was read from;
// it is not part of the file format.
type File struct {
	Version      int           `yaml:"version"`
	Suppressions []Suppression `yaml:"suppressions"`
	Source       string        `yaml:"-"`
}

// Selector identifies resources by logical identity. At least one field must be set.
type Selector struct {
	Kind    string `yaml:"kind,omitempty"`
	Type    string `yaml:"type,omitempty"`
	Name    string `yaml:"name,omitempty"`
	Pointer string `yaml:"pointer,omitempty"`
}

// Suppression is one entry. Exactly one of Fingerprint and Resource is set.
type Suppression struct {
	RuleID      string   `yaml:"ruleId"`
	Fingerprint string   `yaml:"fingerprint,omitempty"`
	Resource    Selector `yaml:"resource,omitempty"`
	Reason      string   `yaml:"reason"`
	Owner       string   `yaml:"owner"`
	Expires     string   `yaml:"expires"` // YYYY-MM-DD, valid through the end of that day (UTC)
}

func (s Suppression) hasSelector() bool { return s.Resource != Selector{} }

// target renders the entry's target for diagnostics and keys. It contains no free text.
func (s Suppression) target() string {
	if s.Fingerprint != "" {
		return s.Fingerprint
	}
	r := s.Resource
	return fmt.Sprintf("resource[kind=%s type=%s name=%s pointer=%s]", r.Kind, strings.ToLower(r.Type), r.Name, r.Pointer)
}

func (s Suppression) matches(f sdk.Finding) bool {
	if s.RuleID != f.RuleID {
		return false
	}
	if s.Fingerprint != "" {
		return s.Fingerprint == f.Fingerprint
	}
	r, fr := s.Resource, f.Resource
	return (r.Kind == "" || r.Kind == fr.Kind) &&
		(r.Type == "" || strings.EqualFold(r.Type, fr.Type)) &&
		(r.Name == "" || r.Name == fr.Name) &&
		(r.Pointer == "" || r.Pointer == fr.Pointer)
}

// expiry parses an expires value. It rejects anything but a real YYYY-MM-DD date.
func expiry(s string) (time.Time, error) {
	if len(s) != len(time.DateOnly) {
		return time.Time{}, fmt.Errorf("not a YYYY-MM-DD date")
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("not a valid YYYY-MM-DD date")
	}
	return t, nil
}

// expired reports whether the suppression no longer applies at now. The day after the
// expiry date starts at 00:00 UTC.
func expired(expires string, now time.Time) bool {
	t, err := expiry(expires)
	if err != nil {
		return true
	}
	return !now.UTC().Before(t.AddDate(0, 0, 1))
}

// Validate checks every entry and returns all problems found, joined, each naming the
// entry index and the missing or invalid field.
func (f File) Validate() error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, a...)...))
	}
	if f.Version != Version {
		bad("unsupported version %d: want %d", f.Version, Version)
	}
	seen := map[string]int{}
	for i, s := range f.Suppressions {
		if s.RuleID == "" {
			bad("suppressions[%d]: missing required field \"ruleId\"", i)
		} else if strings.HasPrefix(s.RuleID, "FND-SYS-SUPPRESSION-") {
			bad("suppressions[%d]: %q is about suppressions and cannot be suppressed", i, s.RuleID)
		}
		hasFP, hasSel := s.Fingerprint != "", s.hasSelector()
		switch {
		case hasFP && hasSel:
			bad("suppressions[%d]: set either \"fingerprint\" or \"resource\", not both", i)
		case !hasFP && !hasSel:
			bad("suppressions[%d]: missing target: set \"fingerprint\" or \"resource\"", i)
		case hasFP && !fingerprintRE.MatchString(s.Fingerprint):
			bad("suppressions[%d]: fingerprint is not a fp1:<32 hex> value", i)
		}
		if strings.TrimSpace(s.Reason) == "" {
			bad("suppressions[%d]: missing required field \"reason\"", i)
		}
		if strings.TrimSpace(s.Owner) == "" {
			bad("suppressions[%d]: missing required field \"owner\"", i)
		}
		if s.Expires == "" {
			bad("suppressions[%d]: missing required field \"expires\"", i)
		} else if _, err := expiry(s.Expires); err != nil {
			bad("suppressions[%d]: expires %q is %v", i, s.Expires, err)
		}
		if s.RuleID != "" && (hasFP || hasSel) {
			key := s.RuleID + "\x00" + s.target()
			if j, dup := seen[key]; dup {
				bad("suppressions[%d]: duplicate of suppressions[%d]", i, j)
			}
			seen[key] = i
		}
	}
	return errors.Join(errs...)
}

// Parse strictly decodes and validates suppression YAML. source is recorded on every
// applied suppression and diagnostic; pass the project-relative path.
func Parse(data []byte, source string) (File, error) {
	if len(data) > MaxFileBytes {
		return File{}, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, len(data), MaxFileBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return File{}, fmt.Errorf("%w: file is empty", ErrInvalid)
		}
		return File{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("%w: more than one YAML document", ErrInvalid)
	}
	f.Source = source
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

// Read loads the suppression file at path, relative to root or absolute inside root.
// Symlinks (in any path component), paths outside root and non-regular files are refused.
func Read(ctx context.Context, root, path string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, fmt.Errorf("read suppressions: %w", err)
	}
	rel, err := relInRoot(root, path)
	if err != nil {
		return File{}, err
	}
	data, err := readFile(root, rel)
	if err != nil {
		return File{}, err
	}
	return Parse(data, filepath.ToSlash(rel))
}

// Result is the outcome of Apply.
type Result struct {
	// Findings is a copy of the input with Suppressed set on suppressed findings.
	Findings []sdk.Finding
	// Diagnostics holds one FND-SYS-SUPPRESSION-EXPIRED error finding per expired entry.
	Diagnostics []sdk.Finding
	// Unused lists unexpired entries that matched no finding, in file order. They are
	// informational. ADR-009 reserves no finding ID for them, so they are not Findings.
	Unused []Suppression
}

// Apply marks findings covered by a valid (unexpired) suppression. now is injected; see the
// package documentation for the expiry boundary. When several valid entries match, the
// first in file order wins. The input slice is not modified.
func Apply(findings []sdk.Finding, f File, now time.Time) Result {
	res := Result{Findings: make([]sdk.Finding, len(findings))}
	copy(res.Findings, findings)

	isExpired := make([]bool, len(f.Suppressions))
	used := make([]bool, len(f.Suppressions))
	for i, s := range f.Suppressions {
		isExpired[i] = expired(s.Expires, now)
		if isExpired[i] {
			res.Diagnostics = append(res.Diagnostics, expiredDiagnostic(s, f.Source))
		}
	}
	for fi := range res.Findings {
		fd := &res.Findings[fi]
		if strings.HasPrefix(fd.RuleID, "FND-SYS-SUPPRESSION-") || fd.Suppressed != nil {
			continue
		}
		for i, s := range f.Suppressions {
			if isExpired[i] || !s.matches(*fd) {
				continue
			}
			used[i] = true
			fd.Suppressed = &sdk.Suppression{Reason: s.Reason, Expires: s.Expires, Owner: s.Owner, Source: f.Source}
			break
		}
	}
	for i, s := range f.Suppressions {
		if !isExpired[i] && !used[i] {
			res.Unused = append(res.Unused, s)
		}
	}
	return res
}

// expiredDiagnostic builds the ADR-009 diagnostic. The reason text is deliberately left
// out of the evidence: it is free text and may carry something that should not be echoed.
func expiredDiagnostic(s Suppression, source string) sdk.Finding {
	return sdk.Finding{
		RuleID:      RuleExpired,
		RuleVersion: 1,
		Severity:    sdk.SeverityError,
		Resource:    sdk.ResourceRef{Kind: "file", Name: source},
		Location:    sdk.Location{File: source},
		Key:         s.RuleID + ":" + s.target(),
		Evidence: fmt.Sprintf("suppression of %s (owner %s) expired on %s (valid through the end of that day, UTC); the finding is visible again",
			s.RuleID, s.Owner, s.Expires),
		Recommendation: "Fix the finding, or renew the suppression with a new expiry date and a current reason.",
		Confidence:     sdk.ConfidenceCertain,
	}
}

func relInRoot(root, path string) (string, error) {
	if root == "" || path == "" {
		return "", fmt.Errorf("%w: root and path are required", ErrUnsafePath)
	}
	rel := path
	if filepath.IsAbs(path) {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrUnsafePath, err)
		}
		rel, err = filepath.Rel(absRoot, filepath.Clean(path))
		if err != nil {
			return "", fmt.Errorf("%w: path is outside the project root", ErrUnsafePath)
		}
	}
	rel = filepath.Clean(rel)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%w: path is outside the project root", ErrUnsafePath)
	}
	return rel, nil
}

func rejectSymlinks(r *os.Root, rel string) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		fi, err := r.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnsafePath, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is a symlink", ErrUnsafePath, p)
		}
	}
	return nil
}

func readFile(root, rel string) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	defer r.Close()
	if err := rejectSymlinks(r, rel); err != nil {
		return nil, err
	}
	fi, err := r.Lstat(rel)
	if err != nil {
		return nil, fmt.Errorf("read suppressions %s: %w", filepath.ToSlash(rel), err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrUnsafePath, filepath.ToSlash(rel))
	}
	if fi.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, fi.Size(), MaxFileBytes)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("read suppressions %s: %w", filepath.ToSlash(rel), err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read suppressions %s: %w", filepath.ToSlash(rel), err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%w: limit %d", ErrTooLarge, MaxFileBytes)
	}
	return data, nil
}
