package rules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

// PackSchema is the value of the schema field of a pack file.
const PackSchema = "pack-v1"

// DefaultPack is the pack used when the configuration names none.
const DefaultPack = "foundry-core"

const maxPackBytes = 1 << 20

var packIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Pack is a named, versioned list of catalogue rule IDs (pack-v1). The list is sorted and unique.
type Pack struct {
	Schema      string   `yaml:"schema"`
	ID          string   `yaml:"id"`
	Version     int      `yaml:"version"`
	Description string   `yaml:"description"`
	Rules       []string `yaml:"rules"`
}

// LoadPacks reads every <root>/*.yaml in fsys, strictly (unknown fields are errors), and returns the
// packs sorted by ID. It does not check the rule IDs against a catalogue; use ValidatePacks.
func LoadPacks(ctx context.Context, fsys fs.FS, root string) ([]Pack, error) {
	ents, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read packs: %w", err)
	}
	var packs []Pack
	var problems []error
	for _, e := range ents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := path.Join(root, e.Name())
		if e.IsDir() || path.Ext(p) != ".yaml" {
			problems = append(problems, fmt.Errorf("%s: unexpected entry; the packs directory holds only .yaml files", p))
			continue
		}
		f, err := fsys.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open pack: %w", err)
		}
		b, err := io.ReadAll(io.LimitReader(f, maxPackBytes+1))
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("read pack %s: %w", p, err)
		}
		if len(b) > maxPackBytes {
			problems = append(problems, fmt.Errorf("%s: file exceeds %d bytes", p, maxPackBytes))
			continue
		}
		var pk Pack
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(&pk); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", p, err))
			continue
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			problems = append(problems, fmt.Errorf("%s: more than one YAML document in a pack file", p))
			continue
		}
		if want := strings.TrimSuffix(e.Name(), ".yaml"); pk.ID != want {
			problems = append(problems, fmt.Errorf("%s: file name must equal pack id %q, got id %q", p, want, pk.ID))
		}
		packs = append(packs, pk)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	slices.SortFunc(packs, func(a, b Pack) int { return strings.Compare(a.ID, b.ID) })
	return packs, nil
}

// ValidatePacks checks the packs against the catalogue: schema, id format, uniqueness, sorted
// unique rule lists, and that every rule exists and is not dropped.
func ValidatePacks(packs []Pack, cat []catalog.Rule) error {
	byID := make(map[string]catalog.Rule, len(cat))
	for _, m := range cat {
		byID[m.ID] = m
	}
	var errs []error
	seen := map[string]bool{}
	for _, p := range packs {
		bad := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("pack %s: "+format, append([]any{p.ID}, a...)...))
		}
		if p.Schema != PackSchema {
			bad("schema must be %q, got %q", PackSchema, p.Schema)
		}
		if !packIDRe.MatchString(p.ID) {
			bad("id must match %s", packIDRe)
		}
		if seen[p.ID] {
			bad("duplicate pack id")
		}
		seen[p.ID] = true
		if p.Version < 1 {
			bad("version must be >= 1")
		}
		if len(p.Rules) == 0 {
			bad("rules is empty")
		}
		if !slices.IsSorted(p.Rules) {
			bad("rules must be sorted")
		}
		if len(slices.Compact(slices.Clone(p.Rules))) != len(p.Rules) {
			bad("rules contains duplicates")
		}
		for _, id := range p.Rules {
			m, ok := byID[id]
			switch {
			case !ok:
				bad("rule %s is not in the catalogue", id)
			case m.Status == catalog.StatusDropped:
				bad("rule %s is dropped", id)
			}
		}
	}
	return errors.Join(errs...)
}

// PackByID finds a pack.
func PackByID(packs []Pack, id string) (Pack, bool) {
	for _, p := range packs {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}
