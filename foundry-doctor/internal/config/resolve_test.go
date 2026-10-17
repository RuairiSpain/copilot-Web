package config

import (
	"errors"
	"slices"
	"testing"
)

func TestDefaultProfileIsProdAndNamedInSources(t *testing.T) {
	e, err := Resolve(nil, Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Profile.Name != "foundry-prod" || DefaultProfile != "foundry-prod" {
		t.Fatalf("default profile = %q", e.Profile.Name)
	}
	if e.Sources["profile"] != "default" {
		t.Fatalf("source = %q", e.Sources["profile"])
	}
}

func TestResolvePrecedenceFlagsEnvRepoProfile(t *testing.T) {
	cfg := &Config{Version: Version, Profile: "foundry-test",
		Inputs:   Inputs{Environment: "repoenv", AzureYAML: "repo/azure.yaml"},
		Outputs:  Outputs{Formats: []string{"markdown"}, Directory: "repo-out"},
		Adoption: Adoption{Baseline: "repo-baseline.yaml"}}
	env := map[string]string{EnvProfile: "foundry-dev", EnvEnvironment: "envenv", EnvFormats: " json , sarif ,", EnvOutputDir: "env-out"}

	t.Run("env beats repo", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{}, env)
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-dev" || e.Environment != "envenv" || e.Outputs.Directory != "env-out" {
			t.Fatalf("got %s %s %s", e.Profile.Name, e.Environment, e.Outputs.Directory)
		}
		if !slices.Equal(e.Outputs.Formats, []string{"json", "sarif"}) {
			t.Fatalf("formats = %v", e.Outputs.Formats)
		}
		if e.Sources["profile"] != "env" || e.Sources["environment"] != "env" {
			t.Fatalf("sources = %v", e.Sources)
		}
		if e.Inputs.AzureYAML != "repo/azure.yaml" || e.Inputs.InfraPath != "./infra" || e.Adoption.Baseline != "repo-baseline.yaml" {
			t.Fatalf("inputs/adoption = %+v %+v", e.Inputs, e.Adoption)
		}
		if e.Adoption.Suppressions != ".foundry-doctor/suppressions.yaml" {
			t.Fatalf("suppressions default = %q", e.Adoption.Suppressions)
		}
	})
	t.Run("flags beat env", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{Profile: "foundry-prod", Environment: "flagenv", Formats: []string{"console", "console", "json"},
			OutputDir: "flag-out", AzureYAML: "flag/azure.yaml", InfraPath: "flag-infra", Baseline: "flag-b.yaml", Suppressions: "flag-s.yaml"}, env)
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-prod" || e.Environment != "flagenv" || e.Outputs.Directory != "flag-out" {
			t.Fatalf("got %s %s %s", e.Profile.Name, e.Environment, e.Outputs.Directory)
		}
		if !slices.Equal(e.Outputs.Formats, []string{"console", "json"}) {
			t.Fatalf("formats must be deduplicated: %v", e.Outputs.Formats)
		}
		if e.Inputs.AzureYAML != "flag/azure.yaml" || e.Inputs.InfraPath != "flag-infra" || e.Adoption.Baseline != "flag-b.yaml" || e.Adoption.Suppressions != "flag-s.yaml" {
			t.Fatalf("inputs/adoption = %+v %+v", e.Inputs, e.Adoption)
		}
		if e.Sources["profile"] != "flag" {
			t.Fatalf("source = %q", e.Sources["profile"])
		}
	})
	t.Run("repo beats profile defaults", func(t *testing.T) {
		e, err := Resolve(cfg, Flags{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.Profile.Name != "foundry-test" || !slices.Equal(e.Outputs.Formats, []string{"markdown"}) || e.Outputs.Directory != "repo-out" {
			t.Fatalf("got %s %v %s", e.Profile.Name, e.Outputs.Formats, e.Outputs.Directory)
		}
	})
	t.Run("profile defaults apply last", func(t *testing.T) {
		e, err := Resolve(&Config{Version: Version}, Flags{Profile: "foundry-dev"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(e.Outputs.Formats, []string{"console"}) || e.Validation.Bicep != ModeOptional {
			t.Fatalf("dev defaults = %v %s", e.Outputs.Formats, e.Validation.Bicep)
		}
		if e.Outputs.Directory != ".foundry-doctor/out" || e.Advanced.Bicep.Executable != "auto" || e.Advanced.PSRule.Enabled != Auto || e.Advanced.Checkov.Enabled != False {
			t.Fatalf("defaults = %+v %+v", e.Outputs, e.Advanced)
		}
		if !slices.Equal(e.Rules.Include, []string{"must-have", "nice-to-have"}) || !slices.Equal(e.Rules.Packs, []string{"foundry-core"}) {
			t.Fatalf("rules defaults = %+v", e.Rules)
		}
	})
	t.Run("repo validation overrides profile", func(t *testing.T) {
		c := &Config{Version: Version, Validation: Validation{Bicep: ModeDisabled}}
		e, err := Resolve(c, Flags{Profile: "foundry-prod"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.Validation.Bicep != ModeDisabled || e.Validation.Azure != ModeOptional {
			t.Fatalf("validation = %+v", e.Validation)
		}
	})
}

func TestResolveEnvironmentPolicyOverridesRepo(t *testing.T) {
	cfg, err := Load([]byte(`version: 1
policy:
  network: {publicAccess: forbidden}
  logRetention: {minimumDays: 30}
environments:
  dev:
    policy:
      network: {publicAccess: allowed}
`))
	if err != nil {
		t.Fatal(err)
	}
	dev, err := Resolve(cfg, Flags{Environment: "dev"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := Resolve(cfg, Flags{Environment: "prod"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := dev.Policy.Get("network.publicAccess"); v != "allowed" || dev.Sources["policy.network.publicAccess"] != "environment" {
		t.Fatalf("dev publicAccess = %v (%s)", v, dev.Sources["policy.network.publicAccess"])
	}
	if v, _ := prod.Policy.Get("network.publicAccess"); v != "forbidden" || prod.Sources["policy.network.publicAccess"] != "repo" {
		t.Fatalf("prod publicAccess = %v", v)
	}
	if v, _ := dev.Policy.Get("logRetention.minimumDays"); v != 30 {
		t.Fatalf("untouched repo key lost: %v", v)
	}
	if v, _ := prod.Policy.Get("resourceScope"); v != DefaultResourceScope || prod.Sources["policy.resourceScope"] != "default" {
		t.Fatalf("default not applied: %v", v)
	}
	// An absent, default-less key stays absent so rules skip rather than guess.
	if _, ok := prod.Policy.Get("network.cloud"); ok {
		t.Fatal("network.cloud has no default and must be absent")
	}
}

func TestResolveErrorsWrapErrInvalid(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *Config
		flags Flags
		env   map[string]string
	}{
		{"unknown flag profile", nil, Flags{Profile: "dev"}, nil},
		{"unknown env profile", nil, Flags{}, map[string]string{EnvProfile: "staging"}},
		{"invalid environment name", nil, Flags{Environment: "bad name!"}, nil},
		{"invalid format flag", nil, Flags{Formats: []string{"pdf"}}, nil},
		{"invalid format env", nil, Flags{}, map[string]string{EnvFormats: "console,html"}},
		{"invalid repo config", &Config{Version: 2}, Flags{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Resolve(tc.cfg, tc.flags, tc.env)
			if err == nil || e != nil {
				t.Fatalf("Resolve = %v, %v; want error", e, err)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error must wrap ErrInvalid: %v", err)
			}
		})
	}
}

func TestLookupProfileReturnsIndependentCopy(t *testing.T) {
	p, ok := LookupProfile("foundry-prod")
	if !ok {
		t.Fatal("foundry-prod missing")
	}
	p.Formats[0] = "mutated"
	again, _ := LookupProfile("foundry-prod")
	if again.Formats[0] != "console" {
		t.Fatal("LookupProfile must not expose shared state")
	}
	if _, ok := LookupProfile("dev"); ok {
		t.Fatal("short names are not config profile names")
	}
	if !slices.Equal(ProfileNames(), []string{"foundry-dev", "foundry-test", "foundry-prod"}) {
		t.Fatalf("names = %v", ProfileNames())
	}
}
