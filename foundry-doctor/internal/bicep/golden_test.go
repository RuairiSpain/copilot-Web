package bicep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden files under testdata/golden were produced by Bicep 0.48.1 against
// the sibling fixtures; project paths were rewritten to /proj.

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "rodonnell") || strings.Contains(string(b), `C:\`) {
		t.Fatalf("golden %s contains a machine-specific path", name)
	}
	return b
}

func TestGoldenSARIFAndText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		file     string
		sarif    bool
		wantCode string
		wantSev  Severity
		wantFile string
		line     int
		col      int
	}{
		{"sarif lint warning", "lint.sarif.json", true, "no-unused-params", SeverityWarning, "lint.bicep", 1, 7},
		{"text lint warning", "lint.txt", false, "no-unused-params", SeverityWarning, "lint.bicep", 1, 7},
		{"sarif compile error", "error.sarif.json", true, "BCP057", SeverityError, "error.bicep", 11, 21},
		{"text compile error", "error.txt", false, "BCP057", SeverityError, "error.bicep", 11, 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := golden(t, tt.file)
			var got []Diagnostic
			if tt.sarif {
				var err error
				if got, err = ParseSARIF(data, "/proj"); err != nil {
					t.Fatal(err)
				}
			} else {
				got = ParseTextDiagnostics(string(data), "/proj")
			}
			var found bool
			for _, d := range got {
				if d.Code == tt.wantCode {
					found = true
					if d.Severity != tt.wantSev || !strings.HasSuffix(d.File, tt.wantFile) ||
						d.Line != tt.line || d.Column != tt.col || d.Message == "" {
						t.Fatalf("unexpected diagnostic: %+v", d)
					}
					if strings.Contains(d.File, "..") || strings.Contains(d.File, "\\") {
						t.Fatalf("unsafe path %q", d.File)
					}
				}
			}
			if !found {
				t.Fatalf("%s not found in %+v", tt.wantCode, got)
			}
			if tt.wantSev == SeverityError != HasErrors(got) {
				t.Fatalf("HasErrors mismatch for %+v", got)
			}
		})
	}
}

func TestGoldenARM(t *testing.T) {
	t.Parallel()
	tmpl, err := ParseARM(golden(t, "main.arm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Resources) != 5 {
		t.Fatalf("top-level resources = %d", len(tmpl.Resources))
	}
	var modules, conditional, looped int
	tmpl.Walk(func(r Resource, depth int) {
		if depth == 0 && r.IsModule() {
			modules++
		}
		if r.Conditional {
			conditional++
		}
		if r.Copy != nil {
			looped++
		}
	})
	if modules != 2 || conditional == 0 || looped == 0 {
		t.Fatalf("modules=%d conditional=%d looped=%d", modules, conditional, looped)
	}
	var secure bool
	for _, p := range tmpl.Parameters {
		if p.Name == "adminPassword" {
			secure = p.Secure
		}
	}
	if !secure || len(tmpl.Outputs) != 2 || len(tmpl.Parameters) != 4 {
		t.Fatalf("params=%+v outputs=%+v", tmpl.Parameters, tmpl.Outputs)
	}
	// Location derivation must be likely, never certain, and never invent lines.
	for _, lr := range (Mapper{Entry: "main.bicep", Modules: ModuleFiles{"single": "modules/storage.bicep"}}).LocateAll(tmpl) {
		if lr.Location.Confidence == ConfidenceCertain || lr.Location.Line != 0 {
			t.Fatalf("derived location overclaims: %+v", lr.Location)
		}
	}

	lint, err := ParseARM(golden(t, "lint.arm.json"))
	if err != nil || len(lint.Resources) != 1 || lint.Resources[0].Type != "Microsoft.Storage/storageAccounts" {
		t.Fatalf("lint ARM: %+v err=%v", lint, err)
	}
}

func TestGoldenCompileThroughInterpret(t *testing.T) {
	t.Parallel()
	c := Compiler{Tool: Tool{Path: "x"}, Root: "/proj"}
	ok, err := c.interpret(RunResult{Stdout: golden(t, "lint.arm.json"), Stderr: golden(t, "lint.sarif.json")})
	if err != nil || !ok.OK || len(ok.Diagnostics) != 1 || HasErrors(ok.Diagnostics) {
		t.Fatalf("warning compile: %+v err=%v", ok, err)
	}
	bad, err := c.interpret(RunResult{ExitCode: 1, Stderr: golden(t, "error.sarif.json")})
	if err != nil || bad.OK || !HasErrors(bad.Diagnostics) || len(bad.ARM) != 0 {
		t.Fatalf("error compile: %+v err=%v", bad, err)
	}
}
