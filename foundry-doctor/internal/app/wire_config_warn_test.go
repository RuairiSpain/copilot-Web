package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func resolveWarnings(t *testing.T, cfg string) string {
	t.Helper()
	dir := t.TempDir()
	if cfg != "" {
		writeFile(t, dir, RepoConfigPath, cfg)
	}
	var buf bytes.Buffer
	c := configAdapter{getenv: func(string) string { return "" }, stderr: &buf}
	if _, err := c.Resolve(context.Background(), ConfigRequest{Dir: dir}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return buf.String()
}

func TestConfigWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  string
		want []string
		not  []string
	}{
		{"no config", "", nil, []string{"psrule", "rules.packs"}},
		{"psrule auto is silent", "version: 1\nadvanced:\n  psrule:\n    enabled: auto\n", nil, []string{"psrule"}},
		{"psrule false is silent", "version: 1\nadvanced:\n  psrule:\n    enabled: false\n", nil, []string{"psrule"}},
		{"psrule true warns", "version: 1\nadvanced:\n  psrule:\n    enabled: true\n", []string{"advanced.psrule.enabled is true", "not available"}, []string{"rules.packs"}},
		{"default pack is silent", "version: 1\nrules:\n  packs: [foundry-core]\n", nil, []string{"rules.packs"}},
		{"other pack warns", "version: 1\nrules:\n  packs: [foundry-core, custom-pack]\n", []string{"rules.packs names custom-pack", "every registered rule runs"}, []string{"psrule"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveWarnings(t, tt.cfg)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %q", w, got)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q in %q", n, got)
				}
			}
		})
	}
}
