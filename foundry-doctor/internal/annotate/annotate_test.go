package annotate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestCollectAndGitHubWorkflowCommands(t *testing.T) {
	finds := []sdk.Finding{
		{
			RuleID:         "FND-CFG-001",
			Severity:       sdk.SeverityError,
			Category:       sdk.CategoryMustHave,
			Location:       sdk.Location{File: "azure.yaml", Line: 3, Column: 1},
			Evidence:       "line1\nline2 :: redacted-marker",
			Recommendation: "Use % managed identity",
			Fingerprint:    "fp-1",
		},
		{
			RuleID:      "FND-NET-001",
			Severity:    sdk.SeverityWarning,
			Location:    sdk.Location{File: "../secret.txt", Line: 1},
			Evidence:    "outside",
			Fingerprint: "fp-2",
		},
		{
			RuleID:      "FND-BCP-001",
			Severity:    sdk.SeverityWarning,
			Location:    sdk.Location{File: "infra/main.bicep", Line: 4, Column: 2},
			Evidence:    "rule-derived bicep finding",
			Fingerprint: "fp-3",
		},
	}
	diags := []bicep.Diagnostic{{File: "infra/main.bicep", Line: 9, Column: 2, Severity: bicep.SeverityWarning, Code: "BCP057", Message: "bad % value :: detail"}}
	entries := Collect(finds, diags)
	if len(entries) != 4 {
		t.Fatalf("entries=%d", len(entries))
	}
	var inlineFound, outsideFound, ruleBicepManifestOnly bool
	for _, entry := range entries {
		switch entry.RuleID {
		case "FND-CFG-001":
			inlineFound = entry.Inline && entry.InlineReason == ""
		case "FND-NET-001":
			outsideFound = !entry.Inline && entry.InlineReason != "" && entry.File == ""
		case "FND-BCP-001":
			ruleBicepManifestOnly = !entry.Inline && entry.InlineReason == "untrusted-bicep-location"
		}
	}
	if !inlineFound {
		t.Fatalf("yaml finding should be inline: %+v", entries)
	}
	if !outsideFound {
		t.Fatalf("unsafe path should be manifest-only: %+v", entries)
	}
	if !ruleBicepManifestOnly {
		t.Fatalf("rule-derived bicep finding should stay manifest-only: %+v", entries)
	}
	var out bytes.Buffer
	if err := GitHubWorkflowCommands(&out, entries); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "\nline2") || strings.Contains(text, ":: ghp") || strings.Contains(text, "line1\nline2") {
		t.Fatalf("workflow output not sanitised:\n%s", text)
	}
	if !strings.Contains(text, "::error file=azure.yaml,line=3,col=1,title=FND-CFG-001::") || !strings.Contains(text, "::warning file=infra/main.bicep,line=9,col=2,title=BCP057::") {
		t.Fatalf("workflow output missing entries:\n%s", text)
	}
}

func TestCollectTruncatesAndEscapesMessages(t *testing.T) {
	entries := Collect([]sdk.Finding{{
		RuleID:         "FND-CFG-001",
		Severity:       sdk.SeverityError,
		Location:       sdk.Location{File: "azure.yaml", Line: 1, Column: 1},
		Evidence:       strings.Repeat("abc::%\n", 80),
		Fingerprint:    "fp-1",
		Recommendation: strings.Repeat("x", 20),
	}}, nil)
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	got := entries[0].Message
	if len(got) > maxMessageBytes || strings.Contains(got, "\n") || !strings.HasSuffix(got, "…") {
		t.Fatalf("message not bounded/single-line: len=%d %q", len(got), got)
	}
	// Canonical text stays plain; workflow escaping belongs to the GitHub sink.
	if !strings.Contains(got, "%") || strings.Contains(got, "%25") || strings.Contains(got, "∷") {
		t.Fatalf("canonical message must not carry sink escaping: %q", got)
	}
}

func TestGitHubEscapesOnceAtTheSink(t *testing.T) {
	entries := Collect([]sdk.Finding{{
		RuleID:         "FND-CFG-001",
		Severity:       sdk.SeverityWarning,
		Location:       sdk.Location{File: "azure.yaml", Line: 2, Column: 1},
		Evidence:       "50% used",
		Recommendation: "keep, below: 80%",
		Fingerprint:    "fp-1",
	}}, nil)
	if entries[0].Message != "50% used" || entries[0].Recommendation != "keep, below: 80%" {
		t.Fatalf("entry text = %q / %q", entries[0].Message, entries[0].Recommendation)
	}
	var out bytes.Buffer
	if err := GitHubWorkflowCommands(&out, entries); err != nil {
		t.Fatal(err)
	}
	want := "::warning file=azure.yaml,line=2,col=1,title=FND-CFG-001::50%25 used — keep, below: 80%25\n"
	if out.String() != want {
		t.Fatalf("got  %q\nwant %q", out.String(), want)
	}
}

func TestFormatCharactersAreStripped(t *testing.T) {
	entries := Collect([]sdk.Finding{{
		RuleID:      "FND-CFG-001",
		Severity:    sdk.SeverityWarning,
		Location:    sdk.Location{File: "azure.yaml", Line: 1},
		Evidence:    "safe\u202etxt.exe\u2066x\u2069\u2028next",
		Fingerprint: "fp-1",
	}}, nil)
	msg := entries[0].Message
	for _, r := range []string{"\u202e", "\u2066", "\u2069", "\u2028"} {
		if strings.Contains(msg, r) {
			t.Fatalf("format character %q not stripped: %q", r, msg)
		}
	}
	if msg != "safetxt.exex next" {
		t.Fatalf("message = %q", msg)
	}
	if c := (Entry{RuleID: "R\u202e1", ID: "x", Message: "a\u2067b"}).ReviewComment(); strings.ContainsAny(c, "\u202e\u2067") {
		t.Fatalf("review comment not stripped: %q", c)
	}
}

func TestCollectDropsDuplicateBicepAdapterFinding(t *testing.T) {
	finds := []sdk.Finding{{
		RuleID: "bicep/BCP057", Adapter: "bicep", Severity: sdk.SeverityError,
		Location: sdk.Location{File: "infra/main.bicep", Line: 9, Column: 2}, Evidence: "The name does not exist.", Fingerprint: "fp-adapter",
	}}
	diags := []bicep.Diagnostic{{File: "infra/main.bicep", Line: 9, Column: 2, Severity: bicep.SeverityError, Code: "BCP057", Message: "The name does not exist."}}
	entries := Collect(finds, diags)
	if len(entries) != 1 || entries[0].Kind != "bicep-diagnostic" {
		t.Fatalf("entries = %+v", entries)
	}
	moved := diags[0]
	moved.Line, moved.Column = 30, 7
	if got := Collect(nil, []bicep.Diagnostic{moved}); got[0].Fingerprint != entries[0].Fingerprint || got[0].Fingerprint == "" {
		t.Fatalf("bicep fingerprint must not depend on the line: %q vs %q", got[0].Fingerprint, entries[0].Fingerprint)
	}
}

func TestWriteReview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\nservices:\n  api:\n    host: containerapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "main.bicep"), []byte("param location string = 'westeurope'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := WriteReview(t.Context(), ReviewRequest{
		RootDir: root, OutDir: "review/sample.review", AzureYAML: "azure.yaml", InfraPath: "infra",
		Entries: []Entry{
			{ID: "f1", RuleID: "FND-CFG-001", Severity: sdk.SeverityError, Category: "must-have", File: "azure.yaml", Line: 1, Message: "config problem", Inline: true},
			{ID: "f2", RuleID: "BCP057", Severity: sdk.SeverityWarning, File: "infra/main.bicep", Line: 1, Message: "compile issue", Inline: true},
			{ID: "f3", RuleID: "FND-NET-001", Severity: sdk.SeverityWarning, File: "../outside.txt", Line: 1, Message: "outside", Inline: false, InlineReason: "outside-project"},
		},
		Profile: "foundry-test", ToolVersion: "0.1.0-dev", WriteDiff: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Summary.Total != 3 || result.Manifest.Summary.Inline != 2 || result.Manifest.Summary.ManifestOnly != 1 {
		t.Fatalf("manifest summary %+v", result.Manifest.Summary)
	}
	manifestPath := filepath.Join(root, "review", "sample.review", "annotations-manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != ManifestSchemaVersion || len(manifest.Files) != 2 {
		t.Fatalf("manifest %+v", manifest)
	}
	if manifest.Files[0].Path != "azure.yaml" || manifest.Files[0].ReviewPath != "review/sample.review/azure.review.yaml" {
		t.Fatalf("manifest must map azure.yaml to its review name: %+v", manifest.Files[0])
	}
	origYAML, _ := os.ReadFile(filepath.Join(root, "azure.yaml"))
	if string(origYAML) != "name: demo\nservices:\n  api:\n    host: containerapp\n" {
		t.Fatalf("original azure.yaml changed:\n%s", origYAML)
	}
	origBicep, _ := os.ReadFile(filepath.Join(root, "infra", "main.bicep"))
	if string(origBicep) != "param location string = 'westeurope'\n" {
		t.Fatalf("original bicep changed:\n%s", origBicep)
	}
	if _, err := os.Stat(filepath.Join(root, "review", "sample.review", "azure.yaml")); !os.IsNotExist(err) {
		t.Fatalf("the review copy must not be discoverable by azd as azure.yaml: %v", err)
	}
	yamlCopy, _ := os.ReadFile(filepath.Join(root, "review", "sample.review", "azure.review.yaml"))
	if !strings.Contains(string(yamlCopy), "# Foundry Doctor: FND-CFG-001 error must-have (ref f1). config problem") {
		t.Fatalf("yaml review copy missing annotation:\n%s", yamlCopy)
	}
	bicepCopy, _ := os.ReadFile(filepath.Join(root, "review", "sample.review", "infra", "main.bicep"))
	if !strings.Contains(string(bicepCopy), "// Foundry Doctor: BCP057 warning (ref f2). compile issue") {
		t.Fatalf("bicep review copy missing annotation:\n%s", bicepCopy)
	}
	if _, err := os.Stat(filepath.Join(root, "review", "sample.review", "annotations.diff")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReviewRejectsUnsafeOut(t *testing.T) {
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "..\\oops.review", AzureYAML: "azure.yaml"}); err == nil {
		t.Fatal("want error")
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "oops", AzureYAML: "azure.yaml"}); err == nil {
		t.Fatal("want .review error")
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "infra\\nested.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); err == nil {
		t.Fatal("want nested review path error")
	}
}

func TestWriteReviewZeroFindingManifestUsesArrays(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{
		RootDir: root, OutDir: "review/empty.review", AzureYAML: "azure.yaml", ToolVersion: "0.1.0-dev",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "review", "empty.review", "annotations-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"annotations": []`) || !strings.Contains(text, `"reviewPath": "review/empty.review/azure.review.yaml"`) || !strings.Contains(text, `"annotationCount": 0`) {
		t.Fatalf("manifest should emit arrays and map the bannered azure.yaml, got:\n%s", text)
	}
	copyData, err := os.ReadFile(filepath.Join(root, "review", "empty.review", "azure.review.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(copyData), "# Foundry Doctor review copy") {
		t.Fatalf("banner must be present with zero annotations:\n%s", copyData)
	}
}

func TestWriteReviewRerunIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\nservices:\n  api:\n    host: containerapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := ReviewRequest{
		RootDir: root, OutDir: "review/idempotent.review", AzureYAML: "azure.yaml",
		Entries:     []Entry{{ID: "f1", RuleID: "FND-CFG-001", Severity: sdk.SeverityError, File: "azure.yaml", Line: 1, Message: "config problem", Inline: true}},
		ToolVersion: "0.1.0-dev",
	}
	if _, err := WriteReview(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReview(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "review", "idempotent.review", "azure.review.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "# Foundry Doctor: FND-CFG-001 error"); got != 1 {
		t.Fatalf("annotation count=%d want 1\n%s", got, data)
	}
}

func writeTestFile(t *testing.T, root, rel, data string, perm os.FileMode) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReviewKeepsForeignDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
	writeTestFile(t, root, "docs.review/notes.md", "mine\n", 0o644)
	_, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "docs.review", AzureYAML: "azure.yaml"})
	if !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("err = %v, want ErrUnsafeOutput", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "docs.review", "notes.md")); err != nil || string(data) != "mine\n" {
		t.Fatalf("user file lost: %q %v", data, err)
	}
	if err := RemoveReview(root, "docs.review"); !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("RemoveReview of a foreign directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs.review", "notes.md")); err != nil {
		t.Fatal(err)
	}
	// An empty existing directory is accepted.
	if err := os.Mkdir(filepath.Join(root, "empty.review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "empty.review", AzureYAML: "azure.yaml"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveReview(root, "empty.review"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "empty.review")); !os.IsNotExist(err) {
		t.Fatalf("owned review copy not removed: %v", err)
	}
}

func TestWriteReviewCopiesAreNeverMorePermissive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o600)
	writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o640)
	writeTestFile(t, root, "infra/local.env", "X=1\n", 0o600)
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "p.review", AzureYAML: "azure.yaml", InfraPath: "infra", WriteDiff: true}); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{
		"azure.yaml":       "p.review/azure.review.yaml",
		"infra/main.bicep": "p.review/infra/main.bicep",
		"infra/local.env":  "p.review/infra/local.env",
	} {
		si, err := os.Stat(filepath.Join(root, filepath.FromSlash(src)))
		if err != nil {
			t.Fatal(err)
		}
		di, err := os.Stat(filepath.Join(root, filepath.FromSlash(dst)))
		if err != nil {
			t.Fatal(err)
		}
		if extra := di.Mode().Perm() &^ si.Mode().Perm(); extra&0o077 != 0 {
			t.Errorf("%s copy mode %v is more permissive than source %v", dst, di.Mode().Perm(), si.Mode().Perm())
		}
	}
	for _, rel := range []string{"p.review", "p.review/annotations.diff"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o007 != 0 {
			t.Errorf("%s is world-accessible: %v", rel, info.Mode().Perm())
		}
	}
}

func TestWriteReviewRemovesPartialTreeOnError(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
	_, err := WriteReview(t.Context(), ReviewRequest{
		RootDir: root, OutDir: "bad.review", AzureYAML: "azure.yaml",
		Entries: []Entry{{ID: "f1", RuleID: "FND-CFG-001", File: "azure.yaml", Line: 99, Message: "out of range", Inline: true}},
	})
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(filepath.Join(root, "bad.review")); !os.IsNotExist(err) {
		t.Fatalf("partial review tree left behind: %v", err)
	}
}

func TestWriteReviewRefusesSymlinks(t *testing.T) {
	link := func(t *testing.T, target, name string) {
		t.Helper()
		if err := os.Symlink(target, name); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Run("output ancestor", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
		if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		link(t, "real", filepath.Join(root, "alias"))
		if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "alias/x.review", AzureYAML: "azure.yaml"}); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("output aliasing infra", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
		writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o644)
		link(t, "infra", filepath.Join(root, "alias"))
		if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "alias/x.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "infra", "x.review")); !os.IsNotExist(err) {
			t.Fatalf("review tree created inside infra: %v", err)
		}
	})
	t.Run("azure.yaml", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "real.yaml", "name: demo\n", 0o644)
		link(t, "real.yaml", filepath.Join(root, "azure.yaml"))
		if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "x.review", AzureYAML: "azure.yaml"}); err == nil {
			t.Fatal("want error")
		}
		if _, err := os.Stat(filepath.Join(root, "x.review")); !os.IsNotExist(err) {
			t.Fatalf("partial review tree left behind: %v", err)
		}
	})
	t.Run("directory inside infra", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
		writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o644)
		writeTestFile(t, root, "outside/secret.txt", "s\n", 0o600)
		link(t, filepath.Join("..", "outside"), filepath.Join(root, "infra", "mods"))
		if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "x.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestWriteReviewRefusesCaseAliasOfInfra(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
	writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o644)
	if _, err := os.Stat(filepath.Join(root, "INFRA")); err != nil {
		t.Skip("case-sensitive file system")
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "INFRA/x.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteOutputFile(t *testing.T) {
	newRoot := func(t *testing.T) string {
		root := t.TempDir()
		writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
		writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o644)
		return root
	}
	inputs := []string{"azure.yaml", "infra"}
	t.Run("writes and replaces a regular file", func(t *testing.T) {
		root := newRoot(t)
		writeTestFile(t, root, "out/a.sarif", "old", 0o644)
		if err := WriteOutputFile(root, "out/a.sarif", inputs, []byte("new")); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(filepath.Join(root, "out", "a.sarif")); string(data) != "new" {
			t.Fatalf("data = %q", data)
		}
	})
	t.Run("lexical overlap", func(t *testing.T) {
		root := newRoot(t)
		for _, out := range []string{"azure.yaml", "infra/x.sarif", "../x.sarif"} {
			if err := WriteOutputFile(root, out, inputs, []byte("x")); !errors.Is(err, ErrUnsafeOutput) {
				t.Errorf("%s: err = %v", out, err)
			}
		}
	})
	t.Run("directory target", func(t *testing.T) {
		root := newRoot(t)
		if err := os.Mkdir(filepath.Join(root, "dir.sarif"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := WriteOutputFile(root, "dir.sarif", inputs, []byte("x")); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("symlink target", func(t *testing.T) {
		root := newRoot(t)
		if err := os.Symlink(filepath.Join("infra", "main.bicep"), filepath.Join(root, "a.sarif")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := WriteOutputFile(root, "a.sarif", inputs, []byte("x")); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("err = %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(root, "infra", "main.bicep")); string(data) != "param p string\n" {
			t.Fatalf("input overwritten: %q", data)
		}
	})
	t.Run("hard link to an input", func(t *testing.T) {
		root := newRoot(t)
		if err := os.Link(filepath.Join(root, "azure.yaml"), filepath.Join(root, "a.sarif")); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if err := WriteOutputFile(root, "a.sarif", inputs, []byte("x")); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("err = %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(root, "azure.yaml")); string(data) != "name: demo\n" {
			t.Fatalf("input overwritten: %q", data)
		}
	})
}

// Phase 6 gate (black belt B3): the review tree is outside the source's ignore
// rules, so credential-like and oversized files are not copied, and the tree
// ignores itself in git.
func TestWriteReviewSkipsCredentialLikeAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
	writeTestFile(t, root, "infra/main.bicep", "param p string\n", 0o644)
	writeTestFile(t, root, "infra/.env", "KEY=value\n", 0o644)
	writeTestFile(t, root, "infra/tls.pem", "pem\n", 0o644)
	writeTestFile(t, root, "infra/terraform.tfstate", "{}\n", 0o644)
	writeTestFile(t, root, "infra/big.bin", strings.Repeat("x", maxFileBytes+1), 0o644)
	res, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "r.review", AzureYAML: "azure.yaml", InfraPath: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", "tls.pem", "terraform.tfstate", "big.bin"} {
		if _, err := os.Stat(filepath.Join(root, "r.review", "infra", name)); err == nil {
			t.Errorf("%s was copied into the review tree", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "r.review", "infra", "main.bicep")); err != nil {
		t.Errorf("main.bicep must be copied: %v", err)
	}
	if got := len(res.Skipped); got != 4 {
		t.Errorf("skipped=%v", res.Skipped)
	}
	gi, err := os.ReadFile(filepath.Join(root, "r.review", ".gitignore"))
	if err != nil || string(gi) != "*\n" {
		t.Errorf(".gitignore=%q err=%v", gi, err)
	}
}

// A large infra tree copies in bounded time and memory per file.
func TestWriteReviewLargeTree(t *testing.T) {
	if testing.Short() {
		t.Skip("large tree")
	}
	root := t.TempDir()
	writeTestFile(t, root, "azure.yaml", "name: demo\n", 0o644)
	for i := 0; i < 1500; i++ {
		writeTestFile(t, root, fmt.Sprintf("infra/mod%04d.bicep", i), "param p string\n", 0o644)
	}
	start := time.Now()
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: root, OutDir: "r.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("copying 1500 files took %s", d)
	}
}
