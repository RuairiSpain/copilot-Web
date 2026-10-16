package baseline

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func sample() *File {
	return FromFindings([]sdk.Finding{
		{RuleID: "FND-B", Fingerprint: fpB, Evidence: "secret-looking evidence /abs/path"},
		{RuleID: "FND-A", Fingerprint: fpA},
	})
}

func TestWriteFileCreatesDeterministicContent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(dir, DefaultPath, sample(), false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(DefaultPath)))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := sample().Marshal()
	if string(got) != string(want) {
		t.Fatalf("content differs:\n%s", got)
	}
	if strings.Contains(string(got), "evidence") || strings.Contains(string(got), "/abs") {
		t.Fatalf("baseline leaks finding text:\n%s", got)
	}
	if strings.Index(string(got), fpA) > strings.Index(string(got), fpB) {
		t.Fatal("entries are not sorted by fingerprint")
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("written file does not load: %v", err)
	}
}

func TestWriteFileRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(p, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteFile(dir, "b.yaml", sample(), false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "keep" {
		t.Fatalf("existing file was modified: %q", b)
	}
	if err := WriteFile(dir, "b.yaml", sample(), true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), fpA) {
		t.Fatalf("force did not overwrite: %q", b)
	}
}

func TestWriteFileRefusesUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"", ".", "..", "../x.yaml", "a/../../x.yaml", "/etc/x.yaml", filepath.Join(dir, "abs.yaml")} {
		if err := WriteFile(dir, rel, sample(), true); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%q: err = %v, want ErrUnsafePath", rel, err)
		}
	}
}

func TestWriteFileRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.yaml")
	if err := os.WriteFile(victim, []byte("victim"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Target is a symlink.
	if err := os.Symlink(victim, filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		if err := WriteFile(dir, "link.yaml", sample(), force); err == nil {
			t.Fatalf("force=%v: symlink target was accepted", force)
		}
	}
	// A directory component is a symlink to a directory outside the root.
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	err := WriteFile(dir, "linkdir/new.yaml", sample(), true)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
	// A symlinked directory inside the root is refused too.
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "inner")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, "inner/b.yaml", sample(), true); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "victim" {
		t.Fatalf("symlink target was written: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.yaml")); err == nil {
		t.Fatal("file was created through a symlinked directory")
	}
}

func TestWriteFileRefusesDirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, "d", sample(), true); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
}

func TestWriteFileMissingProjectDir(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "nope"), "b.yaml", sample(), false); err == nil {
		t.Fatal("want error for a missing project directory")
	}
}
