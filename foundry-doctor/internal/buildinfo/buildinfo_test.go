package buildinfo_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/buildinfo"
)

func TestDescribe(t *testing.T) {
	got := buildinfo.Describe("v1.2.3", "abc123", "2026-10-12")
	if want := "foundry-doctor v1.2.3 (commit abc123, built 2026-10-12)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDefaults(t *testing.T) {
	if buildinfo.Version == "" || buildinfo.Commit == "" || buildinfo.Date == "" {
		t.Fatal("link-time variables must have non-empty defaults")
	}
	if !strings.HasPrefix(buildinfo.String(), buildinfo.ToolName+" ") {
		t.Fatalf("String() = %q", buildinfo.String())
	}
}
