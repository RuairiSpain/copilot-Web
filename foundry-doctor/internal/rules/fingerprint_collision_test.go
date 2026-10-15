package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func keylessAt(line int, ev string) sdk.Finding {
	f := finding()
	f.Location.Line = line
	f.Evidence = ev
	return f
}

func fpsOf(t *testing.T, fs ...sdk.Finding) (*Report, []string) {
	t.Helper()
	e := &Engine{Catalog: []catalog.Rule{cr("FND-CFG-001", []string{"opinion"}, "")},
		Registry: reg(t, fake{id: "FND-CFG-001", res: sdk.Result{Findings: fs}})}
	rep := run(t, e, "foundry-prod", "")
	var out []string
	for _, f := range rep.Findings {
		out = append(out, f.Fingerprint)
	}
	return rep, out
}

func TestFingerprintStableWhenSiblingAppears(t *testing.T) {
	a := keylessAt(3, "host is bad")
	_, alone := fpsOf(t, a)
	rep, both := fpsOf(t, a, keylessAt(9, "other problem"))
	if len(alone) != 1 || len(both) != 2 {
		t.Fatalf("alone=%v both=%v", alone, both)
	}
	if !slices.Contains(both, alone[0]) {
		t.Errorf("fingerprint changed when a sibling appeared: %v vs %v", alone, both)
	}
	if len(rep.Diagnostics) != 1 {
		t.Errorf("distinct findings sharing a base fingerprint should be diagnosed once: %v", rep.Diagnostics)
	}
}

func TestDuplicateKeysAtTwoLinesKeptDistinct(t *testing.T) {
	rep, fps := fpsOf(t, keylessAt(3, "duplicate key host"), keylessAt(7, "duplicate key host"))
	if len(rep.Findings) != 2 || fps[0] == fps[1] {
		t.Fatalf("findings=%d fps=%v", len(rep.Findings), fps)
	}
	if len(rep.Diagnostics) != 1 || !strings.Contains(rep.Diagnostics[0], "FND-CFG-001") {
		t.Errorf("diagnostics = %v", rep.Diagnostics)
	}
}

func TestExactDuplicatesMerge(t *testing.T) {
	rep, _ := fpsOf(t, keylessAt(3, "same"), keylessAt(3, "same"))
	if len(rep.Findings) != 1 {
		t.Fatalf("findings = %d", len(rep.Findings))
	}
	if len(rep.Diagnostics) != 0 {
		t.Errorf("exact duplicates are not a collision: %v", rep.Diagnostics)
	}
}

func TestKeyedSameFingerprintDifferentLocationBothKept(t *testing.T) {
	a, b := keylessAt(3, "x"), keylessAt(7, "x")
	a.Key, b.Key = "host", "host"
	rep, _ := fpsOf(t, a, b)
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %d", len(rep.Findings))
	}
	if len(rep.Diagnostics) != 1 || !strings.Contains(rep.Diagnostics[0], "sets Finding.Key") {
		t.Errorf("diagnostics = %v", rep.Diagnostics)
	}
}
