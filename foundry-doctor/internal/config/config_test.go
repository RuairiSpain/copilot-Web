package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"minimal", "version: 1\n", ""},
		{"full", "version: 1\nprofile: foundry-prod\ninputs:\n  environment: prod\nrules:\n  include: [must-have]\nvalidation:\n  bicep: required\noutputs:\n  formats: [console, sarif]\npolicy:\n  resourceScope: same-resource-group\n  network:\n    publicAccess: forbidden\n", ""},
		{"empty", "", "empty"},
		{"unknown top key", "version: 1\nbogus: 1\n", "bogus"},
		{"unknown policy key", "version: 1\npolicy:\n  nope: 1\n", "nope"},
		{"bad version", "version: 2\n", "version"},
		{"missing version", "profile: foundry-dev\n", "version"},
		{"bad profile", "version: 1\nprofile: other\n", "profile"},
		{"bad format", "version: 1\noutputs:\n  formats: [pdf]\n", "pdf"},
		{"bad mode", "version: 1\nvalidation:\n  bicep: sometimes\n", "bicep"},
		{"multi doc", "version: 1\n---\nversion: 1\n", "document"},
		{"bad scope", "version: 1\npolicy:\n  resourceScope: galaxy\n", "resourceScope"},
		{"bad tag regex", "version: 1\npolicy:\n  tags:\n    required:\n      - name: owner\n        format: '('\n", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load([]byte(tc.in))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error %v does not wrap ErrInvalid", err)
			}
		})
	}
}

func TestLoadOversize(t *testing.T) {
	if _, err := Load([]byte("version: 1\n# " + strings.Repeat("a", MaxConfigBytes))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func mustLoad(t *testing.T, s string) *Config {
	t.Helper()
	c, err := Load([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResolvePrecedence(t *testing.T) {
	cfg := mustLoad(t, `version: 1
profile: foundry-test
inputs: {environment: dev}
policy:
  resourceScope: same-subscription
  network: {publicAccess: allowed}
environments:
  prod:
    policy:
      network: {publicAccess: forbidden}
`)
	t.Run("repo over profile", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-test" || e.Profile.Key != "test" {
			t.Fatalf("profile = %+v", e.Profile)
		}
		if v, _ := e.Policy.Get("network.publicAccess"); v != "allowed" {
			t.Fatalf("publicAccess = %v", v)
		}
		if e.Sources["profile"] != "repo" || e.Sources["policy.network.publicAccess"] != "repo" {
			t.Fatalf("sources = %v", e.Sources)
		}
	})
	t.Run("env over repo", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{}, map[string]string{EnvProfile: "foundry-dev"})
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-dev" || e.Sources["profile"] != "env" {
			t.Fatalf("profile = %v sources=%v", e.Profile.Name, e.Sources)
		}
	})
	t.Run("flag over env", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{Profile: "foundry-prod"}, map[string]string{EnvProfile: "foundry-dev"})
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-prod" || e.Sources["profile"] != "flag" {
			t.Fatalf("profile = %v", e.Profile.Name)
		}
	})
	t.Run("environment policy over repo", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{Environment: "prod"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := e.Policy.Get("network.publicAccess"); v != "forbidden" {
			t.Fatalf("publicAccess = %v", v)
		}
		if v, _ := e.Policy.Get("resourceScope"); v != "same-subscription" {
			t.Fatalf("resourceScope = %v (repo value must survive)", v)
		}
		if e.Sources["policy.network.publicAccess"] != "environment" {
			t.Fatalf("sources = %v", e.Sources)
		}
	})
}

func TestResolveDefaultsAndMissing(t *testing.T) {
	e, err := Resolve(nil, Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Profile.Name != DefaultProfile {
		t.Fatalf("profile = %s", e.Profile.Name)
	}
	if v, _ := e.Policy.Get("resourceScope"); v != DefaultResourceScope {
		t.Fatalf("resourceScope = %v", v)
	}
	if v, _ := e.Policy.Get("network.publicAccess"); v != DefaultPublicAccess {
		t.Fatalf("publicAccess = %v", v)
	}
	// Keys without ADR defaults must be absent so dependent rules skip.
	for _, k := range []string{"logRetention.minimumDays", "models.allowed", "dataResidency.allowedRegions", "knowledge.requireDocumentLevelAccess"} {
		if _, ok := e.Policy.Get(k); ok {
			t.Errorf("%s unexpectedly set", k)
		}
	}
	if len(e.Outputs.Formats) == 0 || e.Inputs.AzureYAML != "./azure.yaml" {
		t.Fatalf("defaults wrong: %+v %+v", e.Outputs, e.Inputs)
	}
}

func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name  string
		flags Flags
		env   map[string]string
	}{
		{"bad flag profile", Flags{Profile: "x"}, nil},
		{"bad env profile", Flags{}, map[string]string{EnvProfile: "x"}},
		{"bad format flag", Flags{Formats: []string{"pdf"}}, nil},
		{"bad environment name", Flags{Environment: "../x"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Resolve(nil, tc.flags, tc.env); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPolicyImmutableAndDeterministic(t *testing.T) {
	cfg := mustLoad(t, "version: 1\npolicy:\n  allowedExternalScopes: [/subscriptions/x]\n")
	e, _ := Resolve(cfg, Flags{}, nil)
	v, _ := e.Policy.Get("allowedExternalScopes")
	v.([]string)[0] = "mutated"
	v2, _ := e.Policy.Get("allowedExternalScopes")
	if v2.([]string)[0] != "/subscriptions/x" {
		t.Fatal("Get must return a clone")
	}
	a, err1 := e.Policy.Dump()
	b, err2 := e.Policy.Dump()
	if err1 != nil || err2 != nil || a != b || a == "" {
		t.Fatal("Dump not deterministic")
	}
}

func TestProfiles(t *testing.T) {
	for _, n := range ProfileNames() {
		p, ok := LookupProfile(n)
		if !ok || p.Name != n || p.Key == "" {
			t.Errorf("profile %s bad: %+v", n, p)
		}
	}
	if _, ok := LookupProfile("nope"); ok {
		t.Fatal("unknown profile found")
	}
}

func FuzzLoad(f *testing.F) {
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\npolicy:\n  tags:\n    required: [{name: a, format: '^x$'}]\n"))
	f.Add([]byte("{{{{"))
	f.Fuzz(func(t *testing.T, b []byte) {
		c, err := Load(b)
		if err == nil {
			if _, rerr := Resolve(c, Flags{}, nil); rerr != nil {
				t.Fatalf("loaded config failed to resolve: %v", rerr)
			}
		}
	})
}
