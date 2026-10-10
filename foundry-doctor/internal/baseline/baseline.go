// Package baseline reads, writes and applies fingerprint baselines.
//
// A baseline hides ONLY findings whose fingerprint exactly matches an entry.
// Any finding with a new fingerprint (including a changed rule version,
// resource or location) remains visible. Baselined findings are marked, never
// removed, so reports can still count them.
package baseline

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Version is the only supported baseline file schema version.
const Version = 1

// DefaultPath is the conventional repo-relative baseline location.
const DefaultPath = ".foundry-doctor/baseline.yaml"

// MaxBytes bounds the baseline file size accepted by Read.
const MaxBytes = 4 << 20

var fingerprintRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Entry is one accepted fingerprint.
type Entry struct {
	Fingerprint string `yaml:"fingerprint"`
	RuleID      string `yaml:"ruleId,omitempty"`
	Note        string `yaml:"note,omitempty"`
}

// File is the serialised baseline.
type File struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

// Parse decodes and validates a baseline. Unknown keys, bad versions,
// malformed fingerprints and duplicates are errors.
func Parse(data []byte) (*File, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("baseline: file exceeds %d bytes", MaxBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("baseline: empty document")
		}
		return nil, fmt.Errorf("baseline: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("baseline: multiple documents are not allowed")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate checks the structural rules.
func (f *File) Validate() error {
	if f.Version != Version {
		return fmt.Errorf("baseline: unsupported version %d (want %d)", f.Version, Version)
	}
	seen := map[string]bool{}
	for i, e := range f.Entries {
		if !fingerprintRE.MatchString(e.Fingerprint) {
			return fmt.Errorf("baseline: entries[%d]: invalid fingerprint %q", i, e.Fingerprint)
		}
		if seen[e.Fingerprint] {
			return fmt.Errorf("baseline: entries[%d]: duplicate fingerprint %s", i, e.Fingerprint)
		}
		seen[e.Fingerprint] = true
	}
	return nil
}

// Marshal renders the baseline deterministically (sorted by fingerprint).
func (f *File) Marshal() ([]byte, error) {
	out := File{Version: Version, Entries: append([]Entry{}, f.Entries...)}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Fingerprint < out.Entries[j].Fingerprint })
	if err := out.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	return buf.Bytes(), nil
}

// FromFindings builds a baseline from findings. Skipped results are not
// findings and so are never baselined. Suppressed and already-baselined
// findings are included only if present in the input slice.
func FromFindings(fs []sdk.Finding) *File {
	seen := map[string]bool{}
	out := &File{Version: Version, Entries: []Entry{}}
	for _, f := range fs {
		if f.Fingerprint == "" || seen[f.Fingerprint] {
			continue
		}
		seen[f.Fingerprint] = true
		out.Entries = append(out.Entries, Entry{Fingerprint: f.Fingerprint, RuleID: f.RuleID})
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Fingerprint < out.Entries[j].Fingerprint })
	return out
}

// Result summarises Apply.
type Result struct {
	Baselined int
	// Stale lists baseline fingerprints that matched no finding, sorted.
	Stale []string
}

// Apply marks findings whose fingerprint is in the baseline. Only an exact
// fingerprint match sets Baselined; a nil baseline changes nothing. The input
// slice is modified in place.
func (f *File) Apply(fs []sdk.Finding) Result {
	var res Result
	if f == nil {
		return res
	}
	want := make(map[string]bool, len(f.Entries))
	for _, e := range f.Entries {
		want[e.Fingerprint] = true
	}
	used := map[string]bool{}
	for i := range fs {
		fp := fs[i].Fingerprint
		if fp != "" && want[fp] {
			fs[i].Baselined = true
			used[fp] = true
			res.Baselined++
		}
	}
	for fp := range want {
		if !used[fp] {
			res.Stale = append(res.Stale, fp)
		}
	}
	sort.Strings(res.Stale)
	return res
}
