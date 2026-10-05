// Package baseline creates, stores and applies adoption baselines.
//
// A baseline records the fingerprints (ADR-008) of findings an existing project has
// accepted for now. Apply hides nothing: it marks a finding Baselined only when its
// fingerprint equals a baseline entry exactly. A changed finding gets a new fingerprint
// and stays visible, as does every new finding. Entries that match no finding are
// returned as stale so the caller can prune them.
//
// Engine diagnostics (FND-SYS-* and bicep/*, ADR-009) cannot be baselined: Generate
// skips them, Apply never marks them and the reader rejects entries for them.
//
// Files are untrusted input. Read refuses symlinks, paths outside the root, oversize
// files, unknown keys, unknown versions, duplicate fingerprints and multi-document YAML.
// Output is deterministic: sorted entries, fixed layout, and no wall-clock time unless
// the caller supplies a firstSeen date.
package baseline

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
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Version is the only baseline file version this package reads and writes.
const Version = 1

// MaxFileBytes is the largest baseline file Read accepts.
const MaxFileBytes = 4 << 20

// FingerprintPrefix is the ADR-008 scheme prefix. Other prefixes are rejected.
const FingerprintPrefix = "fp1:"

// Errors returned (wrapped) by this package.
var (
	ErrInvalid    = errors.New("invalid baseline")
	ErrUnsafePath = errors.New("unsafe baseline path")
	ErrTooLarge   = errors.New("baseline file too large")
)

var fingerprintRE = regexp.MustCompile(`^fp1:[0-9a-f]{32}$`)

// File is the baseline file.
type File struct {
	Version     int     `yaml:"version"`
	GeneratedBy string  `yaml:"generatedBy"`
	Entries     []Entry `yaml:"entries"`
}

// Resource is the logical identity of the baselined resource. It carries no Azure
// resource ID, because an ID can embed a subscription.
type Resource struct {
	Kind    string `yaml:"kind,omitempty"`
	Type    string `yaml:"type,omitempty"`
	Name    string `yaml:"name,omitempty"`
	Pointer string `yaml:"pointer,omitempty"`
}

// Entry is one accepted finding.
type Entry struct {
	Fingerprint string   `yaml:"fingerprint"`
	RuleID      string   `yaml:"ruleId"`
	RuleVersion int      `yaml:"ruleVersion"`
	Resource    Resource `yaml:"resource"`
	// FirstSeen is a YYYY-MM-DD date supplied by the caller. Empty when not supplied.
	FirstSeen string `yaml:"firstSeen,omitempty"`
}

// IsDiagnostic reports whether ruleID is an engine diagnostic (ADR-009), which is never baselined.
func IsDiagnostic(ruleID string) bool {
	return strings.HasPrefix(ruleID, "FND-SYS-") || strings.HasPrefix(ruleID, "bicep/")
}

// Generate builds a baseline from findings. toolVersion is recorded as generatedBy.
// firstSeen is an optional YYYY-MM-DD date stamped on every entry; pass "" to omit it.
// Engine diagnostics are skipped. Findings with the same fingerprint collapse to one entry.
// A finding without a valid fingerprint is an error: the engine must stamp it first.
func Generate(findings []sdk.Finding, toolVersion, firstSeen string) (File, error) {
	if firstSeen != "" {
		if _, err := time.Parse(time.DateOnly, firstSeen); err != nil {
			return File{}, fmt.Errorf("%w: firstSeen %q is not a YYYY-MM-DD date", ErrInvalid, firstSeen)
		}
	}
	f := File{Version: Version, GeneratedBy: strings.TrimSpace(toolVersion), Entries: []Entry{}}
	seen := map[string]bool{}
	for i, fd := range findings {
		if IsDiagnostic(fd.RuleID) {
			continue
		}
		if !fingerprintRE.MatchString(fd.Fingerprint) {
			return File{}, fmt.Errorf("%w: finding %d (%s) has no valid fingerprint", ErrInvalid, i, fd.RuleID)
		}
		if seen[fd.Fingerprint] {
			continue
		}
		seen[fd.Fingerprint] = true
		f.Entries = append(f.Entries, Entry{
			Fingerprint: fd.Fingerprint,
			RuleID:      fd.RuleID,
			RuleVersion: fd.RuleVersion,
			Resource: Resource{
				Kind:    fd.Resource.Kind,
				Type:    strings.ToLower(fd.Resource.Type),
				Name:    fd.Resource.Name,
				Pointer: fd.Resource.Pointer,
			},
			FirstSeen: firstSeen,
		})
	}
	sortEntries(f.Entries)
	return f, nil
}

// Apply returns a copy of findings with Baselined set for exact fingerprint matches, and
// the baseline entries that matched no finding (stale), sorted. The input is not modified.
func Apply(findings []sdk.Finding, b File) (marked []sdk.Finding, stale []Entry) {
	index := make(map[string]bool, len(b.Entries))
	for _, e := range b.Entries {
		index[e.Fingerprint] = false
	}
	marked = make([]sdk.Finding, len(findings))
	copy(marked, findings)
	for i := range marked {
		if IsDiagnostic(marked[i].RuleID) || marked[i].Fingerprint == "" {
			continue
		}
		if _, ok := index[marked[i].Fingerprint]; ok {
			marked[i].Baselined = true
			index[marked[i].Fingerprint] = true
		}
	}
	for _, e := range b.Entries {
		if !index[e.Fingerprint] {
			stale = append(stale, e)
		}
	}
	sortEntries(stale)
	return marked, stale
}

// Validate checks a baseline and returns every problem found, joined.
func (f File) Validate() error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, a...)...))
	}
	if f.Version != Version {
		bad("unsupported version %d: want %d", f.Version, Version)
	}
	seen := map[string]int{}
	for i, e := range f.Entries {
		switch {
		case e.Fingerprint == "":
			bad("entries[%d]: missing required field \"fingerprint\"", i)
		case !fingerprintRE.MatchString(e.Fingerprint):
			bad("entries[%d]: fingerprint is not a %s<32 hex> value (unknown scheme?)", i, FingerprintPrefix)
		default:
			if j, dup := seen[e.Fingerprint]; dup {
				bad("entries[%d]: duplicate fingerprint, first seen at entries[%d]", i, j)
			}
			seen[e.Fingerprint] = i
		}
		if e.RuleID == "" {
			bad("entries[%d]: missing required field \"ruleId\"", i)
		} else if IsDiagnostic(e.RuleID) {
			bad("entries[%d]: engine diagnostic %q cannot be baselined", i, e.RuleID)
		}
		if e.RuleVersion < 1 {
			bad("entries[%d]: ruleVersion must be 1 or greater", i)
		}
		if e.FirstSeen != "" {
			if _, err := time.Parse(time.DateOnly, e.FirstSeen); err != nil {
				bad("entries[%d]: firstSeen %q is not a YYYY-MM-DD date", i, e.FirstSeen)
			}
		}
	}
	return errors.Join(errs...)
}

// Marshal validates f and renders it as deterministic YAML: entries sorted by rule ID and
// fingerprint, two-space indent, LF line endings. f is not modified.
func Marshal(f File) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	out := f
	out.Entries = append([]Entry{}, f.Entries...)
	sortEntries(out.Entries)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("encode baseline: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode baseline: %w", err)
	}
	return buf.Bytes(), nil
}

// Parse strictly decodes and validates baseline YAML.
func Parse(data []byte) (File, error) {
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
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

// Read loads the baseline at path. path is relative to root, or absolute inside root.
// Symlinks (in any path component), paths outside root and non-regular files are refused.
func Read(ctx context.Context, root, path string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, fmt.Errorf("read baseline: %w", err)
	}
	data, err := readFile(root, path)
	if err != nil {
		return File{}, err
	}
	return Parse(data)
}

// Write validates f and writes it to path (relative to root, or absolute inside root),
// creating parent directories. Symlinks and paths outside root are refused.
func Write(ctx context.Context, root, path string, f File) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	data, err := Marshal(f)
	if err != nil {
		return err
	}
	return writeFile(root, path, data)
}

func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].RuleID != es[j].RuleID {
			return es[i].RuleID < es[j].RuleID
		}
		return es[i].Fingerprint < es[j].Fingerprint
	})
}

// relInRoot returns a cleaned root-relative path or an ErrUnsafePath error.
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

// rejectSymlinks lstats each existing component of rel inside r. Missing components are fine.
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

func readFile(root, path string) ([]byte, error) {
	rel, err := relInRoot(root, path)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("read baseline %s: %w", filepath.ToSlash(rel), err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrUnsafePath, filepath.ToSlash(rel))
	}
	if fi.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, fi.Size(), MaxFileBytes)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("read baseline %s: %w", filepath.ToSlash(rel), err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read baseline %s: %w", filepath.ToSlash(rel), err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%w: limit %d", ErrTooLarge, MaxFileBytes)
	}
	return data, nil
}

func writeFile(root, path string, data []byte) (err error) {
	rel, err := relInRoot(root, path)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer r.Close()
	if err := rejectSymlinks(r, rel); err != nil {
		return err
	}
	if fi, lerr := r.Lstat(rel); lerr == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a regular file", ErrUnsafePath, filepath.ToSlash(rel))
	}
	if dir := filepath.Dir(rel); dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", filepath.ToSlash(rel), err)
		}
	}
	tmp := rel + ".tmp"
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("write baseline %s: %w", filepath.ToSlash(rel), err)
	}
	defer func() {
		if err != nil {
			_ = r.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write baseline %s: %w", filepath.ToSlash(rel), err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write baseline %s: %w", filepath.ToSlash(rel), err)
	}
	if err = r.Rename(tmp, rel); err != nil {
		return fmt.Errorf("write baseline %s: %w", filepath.ToSlash(rel), err)
	}
	return nil
}
