package project

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDefaultEnvironmentSelection(t *testing.T) {
	tests := []struct {
		name       string
		config     string // "" means no .azure/config.json
		request    string
		wantEnv    string
		wantSource string
		wantDef    string
		wantWarn   string // substring of a warning, "" means none expected
	}{
		{"default selected", `{"version":1,"defaultEnvironment":"prod"}`, "", "prod", EnvSourceDefault, "prod", ""},
		{"request wins over default", `{"version":1,"defaultEnvironment":"prod"}`, "dev", "dev", EnvSourceRequested, "prod", ""},
		{"no config file", "", "", "", "", "", ""},
		{"default names missing directory", `{"defaultEnvironment":"ghost"}`, "", "", "", "ghost", "names no listed environment directory"},
		{"default has invalid name", `{"defaultEnvironment":"../x"}`, "", "", "", "", "not a valid environment name"},
		{"config is not JSON", `defaultEnvironment: prod`, "", "", "", "", "not valid JSON"},
		{"config without field", `{"version":1}`, "", "", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "azure.yaml", "name: x\n")
			write(t, root, ".azure/dev/.env", "A=1\n")
			write(t, root, ".azure/prod/.env", "A=2\n")
			if tc.config != "" {
				write(t, root, ".azure/config.json", tc.config)
			}
			p, err := Discover(context.Background(), root, Options{Environment: tc.request})
			if err != nil {
				t.Fatal(err)
			}
			if p.Environment != tc.wantEnv || p.EnvironmentSource != tc.wantSource || p.DefaultEnvironment != tc.wantDef {
				t.Fatalf("env=%q source=%q default=%q", p.Environment, p.EnvironmentSource, p.DefaultEnvironment)
			}
			joined := strings.Join(p.Warnings, "\n")
			if tc.wantWarn == "" && joined != "" {
				t.Fatalf("unexpected warnings %q", joined)
			}
			if tc.wantWarn != "" && !strings.Contains(joined, tc.wantWarn) {
				t.Fatalf("warnings %q lack %q", joined, tc.wantWarn)
			}
		})
	}
}

func TestDefaultEnvironmentConfigOversizeIsIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	write(t, root, ".azure/prod/.env", "A=2\n")
	write(t, root, ".azure/config.json", `{"defaultEnvironment":"prod","pad":"`+strings.Repeat("x", MaxAzureConfigSize)+`"}`)
	p, err := Discover(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Environment != "" || !strings.Contains(strings.Join(p.Warnings, "\n"), "size limit") {
		t.Fatalf("env=%q warnings=%v", p.Environment, p.Warnings)
	}
}

func TestEnvironmentDirectoryCap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	for i := 0; i < MaxEnvironments+5; i++ {
		write(t, root, fmt.Sprintf(".azure/e%03d/config.json", i), "{}")
	}
	p, err := Discover(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Environments) != MaxEnvironments {
		t.Fatalf("listed %d environments", len(p.Environments))
	}
	if !strings.Contains(strings.Join(p.Warnings, "\n"), "environment limit") {
		t.Fatalf("no limit warning: %v", p.Warnings)
	}
}
