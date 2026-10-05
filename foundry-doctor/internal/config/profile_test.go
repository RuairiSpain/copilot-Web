package config_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "regenerate rules/profiles/*.yaml from rules/catalog")

func loadCatalogue(t *testing.T) []catalog.Rule {
	t.Helper()
	rules, err := catalog.Load(context.Background(), os.DirFS(filepath.Join("..", "..", "rules")), "catalog")
	if err != nil {
		t.Fatalf("load catalogue: %v", err)
	}
	return rules
}

func catalogueSeverities(rules []catalog.Rule) map[string]map[string]sdk.Severity {
	out := map[string]map[string]sdk.Severity{config.ProfileDev: {}, config.ProfileTest: {}, config.ProfilePROD: {}}
	for _, r := range rules {
		out[config.ProfileDev][r.ID] = sdk.Severity(r.Severity.Dev)
		out[config.ProfileTest][r.ID] = sdk.Severity(r.Severity.Test)
		out[config.ProfilePROD][r.ID] = sdk.Severity(r.Severity.Prod)
	}
	return out
}

// TestGenerateProfiles rewrites rules/profiles/*.yaml when run with -update.
func TestGenerateProfiles(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate rules/profiles")
	}
	for name, sev := range catalogueSeverities(loadCatalogue(t)) {
		p := filepath.Join("..", "..", "rules", "profiles", name+".yaml")
		if err := os.WriteFile(p, config.RenderProfile(name, sev), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfilesUpToDate(t *testing.T) {
	for name, sev := range catalogueSeverities(loadCatalogue(t)) {
		got, err := os.ReadFile(filepath.Join("..", "..", "rules", "profiles", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, config.RenderProfile(name, sev)) {
			t.Errorf("%s.yaml is stale; run go test ./internal/config -run TestGenerateProfiles -update", name)
		}
	}
}

func TestProfileSeveritiesMatchCatalogue(t *testing.T) {
	rules := loadCatalogue(t)
	if len(rules) != 108 {
		t.Fatalf("catalogue has %d rules, want 108", len(rules))
	}
	set, err := config.DefaultProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Names(); !slices.Equal(got, []string{"foundry-dev", "foundry-prod", "foundry-test"}) {
		t.Fatalf("profiles = %v", got)
	}
	for _, r := range rules {
		for name, want := range map[string]string{config.ProfileDev: r.Severity.Dev, config.ProfileTest: r.Severity.Test, config.ProfilePROD: r.Severity.Prod} {
			p, _ := set.Get(name)
			got, ok := p.Severity(r.ID)
			if !ok || string(got) != want {
				t.Errorf("%s %s: profile %q (found %v), catalogue %q", name, r.ID, got, ok, want)
			}
		}
	}
	for _, name := range set.Names() {
		p, _ := set.Get(name)
		if len(p.Severities) != 108 {
			t.Errorf("%s lists %d rules, want 108", name, len(p.Severities))
		}
	}
}

func TestPlatformBasisAlwaysError(t *testing.T) {
	set, err := config.DefaultProfiles()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range loadCatalogue(t) {
		if !slices.Contains(r.Basis, "platform") {
			continue
		}
		n++
		for _, name := range set.Names() {
			p, _ := set.Get(name)
			if s, _ := p.Severity(r.ID); s != sdk.SeverityError {
				t.Errorf("platform-basis rule %s is %q in %s", r.ID, s, name)
			}
		}
	}
	if n == 0 {
		t.Fatal("no platform-basis rules found; the test would pass vacuously")
	}
}

func TestNormalizeProfile(t *testing.T) {
	cases := []struct {
		in, want string
		bad      bool
	}{
		{"dev", "foundry-dev", false}, {"test", "foundry-test", false}, {"prod", "foundry-prod", false},
		{"foundry-dev", "foundry-dev", false}, {"foundry-prod", "foundry-prod", false},
		{"", "", true}, {"PROD", "", true}, {"staging", "", true}, {"foundry-", "", true},
	}
	for _, c := range cases {
		got, err := config.NormalizeProfile(c.in)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("NormalizeProfile(%q) = %q, %v", c.in, got, err)
		}
		if err != nil {
			if ec, ok := err.(config.ExitCoder); !ok || ec.ExitCode() != sdk.ExitCannotRun {
				t.Errorf("error for %q is not exit 2: %v", c.in, err)
			}
		}
	}
}

func goodProfile(name string, extra string) []byte {
	return []byte("name: " + name + "\nversion: 1\nseverities:\n  FND-CFG-001: error\n" + extra)
}

func TestLoadProfilesErrors(t *testing.T) {
	good := func() fstest.MapFS {
		m := fstest.MapFS{}
		for _, n := range config.ProfileNames() {
			m[n+".yaml"] = &fstest.MapFile{Data: goodProfile(n, "")}
		}
		return m
	}
	if _, err := config.LoadProfiles(good()); err != nil {
		t.Fatalf("good set: %v", err)
	}
	cases := []struct {
		name string
		edit func(fstest.MapFS)
		want string
	}{
		{"missing file", func(m fstest.MapFS) { delete(m, "foundry-test.yaml") }, "foundry-test"},
		{"wrong name", func(m fstest.MapFS) { m["foundry-test.yaml"].Data = goodProfile("foundry-dev", "") }, "name must be"},
		{"bad severity", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = goodProfile("foundry-test", "  FND-CFG-002: fatal\n")
		}, "unknown severity"},
		{"bad id", func(m fstest.MapFS) { m["foundry-test.yaml"].Data = goodProfile("foundry-test", "  cfg-2: error\n") }, "not a rule ID"},
		{"unknown key", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = append(goodProfile("foundry-test", ""), "extra: 1\n"...)
		}, "field extra"},
		{"duplicate rule", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = goodProfile("foundry-test", "  FND-CFG-001: warning\n")
		}, "already defined"},
		{"bad version", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = []byte("name: foundry-test\nversion: 2\nseverities:\n  FND-CFG-001: error\n")
		}, "unsupported version"},
		{"empty severities", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = []byte("name: foundry-test\nversion: 1\nseverities: {}\n")
		}, "empty"},
		{"two documents", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = append(goodProfile("foundry-test", ""), "---\na: b\n"...)
		}, "more than one"},
		{"different rule sets", func(m fstest.MapFS) {
			m["foundry-test.yaml"].Data = goodProfile("foundry-test", "  FND-CFG-009: error\n")
		}, "different set"},
		{"too large", func(m fstest.MapFS) { m["foundry-test.yaml"].Data = bytes.Repeat([]byte("#"), 300<<10) }, "exceeds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := good()
			c.edit(m)
			_, err := config.LoadProfiles(m)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want containing %q", err, c.want)
			}
		})
	}
}
