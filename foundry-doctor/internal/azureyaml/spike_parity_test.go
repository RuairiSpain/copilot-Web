package azureyaml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The Phase 0 spike (test/spikes/azd) pins what a Foundry Doctor reader must find in two fixtures. This test runs the
// production reader against the same fixtures and expected.json, so the spike and the package cannot disagree.
func TestSpikeFixturesParity(t *testing.T) {
	spike := filepath.Join("..", "..", "test", "spikes", "azd")
	raw, err := os.ReadFile(filepath.Join(spike, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exp struct {
		Fixtures map[string]struct {
			DuplicateKeys []struct {
				Path        string
				Occurrences int
			} `json:"duplicateKeys"`
			UnresolvedEnvRefs []struct{ Path, Name string }  `json:"unresolvedEnvRefs"`
			UnresolvedUses    []struct{ Path, Value string } `json:"unresolvedUses"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatal(err)
	}
	if len(exp.Fixtures) != 2 {
		t.Fatalf("expected.json lists %d fixtures, want 2", len(exp.Fixtures))
	}
	for name, want := range exp.Fixtures {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(spike, name, "azure.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			envRaw, err := os.ReadFile(filepath.Join(spike, name, ".azure", "dev", "env.fixture"))
			if err != nil {
				t.Fatal(err)
			}
			env := map[string]string{}
			for _, line := range strings.Split(string(envRaw), "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
					env[k] = strings.Trim(v, `"`)
				}
			}
			r, err := Parse(src, Options{File: "azure.yaml"})
			if err != nil {
				t.Fatal(err)
			}

			var gotDup, wantDup []string
			for _, d := range r.Duplicates {
				gotDup = append(gotDup, fmt.Sprintf("%s x%d", d.Path, len(d.Positions)))
			}
			for _, d := range want.DuplicateKeys {
				wantDup = append(wantDup, fmt.Sprintf("%s x%d", d.Path, d.Occurrences))
			}
			if !slices.Equal(gotDup, wantDup) {
				t.Errorf("duplicate keys = %v, want %v", gotDup, wantDup)
			}

			var gotRef, wantRef []string
			for _, i := range r.UnresolvedRefs(func(n string) bool { _, ok := env[n]; return ok }) {
				gotRef = append(gotRef, i.Path+" "+i.Name)
			}
			for _, d := range want.UnresolvedEnvRefs {
				wantRef = append(wantRef, d.Path+" "+d.Name)
			}
			if !slices.Equal(gotRef, wantRef) {
				t.Errorf("unresolved env refs = %v, want %v", gotRef, wantRef)
			}

			var gotUses, wantUses []string
			for _, u := range r.UndefinedUses() {
				gotUses = append(gotUses, fmt.Sprintf("services.%s.uses[%d] %s", u.Service, u.Index, u.Name))
			}
			for _, d := range want.UnresolvedUses {
				wantUses = append(wantUses, d.Path+" "+d.Value)
			}
			if !slices.Equal(gotUses, wantUses) {
				t.Errorf("unresolved uses = %v, want %v", gotUses, wantUses)
			}
		})
	}
}

func TestSpikeDuplicateLocations(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "test", "spikes", "azd", "invalid-duplicate-and-unresolved", "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Duplicates) != 1 {
		t.Fatalf("duplicates = %+v", r.Duplicates)
	}
	d := r.Duplicates[0]
	if d.Key != "name" || d.Positions[0].Line != 33 || d.Positions[1].Line != 34 || d.Positions[0].Column != 5 {
		t.Errorf("duplicate = %+v", d)
	}
}
