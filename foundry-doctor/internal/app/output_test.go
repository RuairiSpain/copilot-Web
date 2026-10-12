package app_test

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// fixtureProject copies test/fixtures/project/<name> into a temp directory.
func fixtureProject(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "test", "fixtures", "project", name)
	dst, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func sampleOptions(t *testing.T, format string) app.Options {
	t.Helper()
	cat := catalogue(t, failing("FND-CFG-001", "azure.yaml has a problem"), failing("FND-ENV-001", "an environment value is missing"), passing("FND-NET-001"))
	o := base(fixtureProject(t, "synthetic-foundry"), cat, "FND-CFG-001,FND-ENV-001,FND-NET-001")
	o.Profile, o.Formats = "test", []string{format}
	return o
}

func TestGolden(t *testing.T) {
	for _, tc := range []struct{ format, file string }{
		{"console", "sample.console.txt"}, {"json", "sample.json"}, {"markdown", "sample.md"}, {"sarif", "sample.sarif"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			r := run(t, sampleOptions(t, tc.format))
			if r.code != 1 {
				t.Fatalf("exit %d\n%s", r.code, r.stderr)
			}
			path := filepath.Join("testdata", "golden", tc.file)
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(r.stdout), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if r.stdout != string(want) {
				t.Errorf("output differs from %s (run with -update to accept)\n--- got\n%s", path, r.stdout)
			}
		})
	}
}

func TestDeterministicAcrossRuns(t *testing.T) {
	for _, f := range []string{"console", "json", "markdown", "sarif"} {
		a := run(t, sampleOptions(t, f))
		b := run(t, sampleOptions(t, f)) // a different temp directory: no absolute path may leak
		if a.stdout != b.stdout || a.stderr != b.stderr || a.code != b.code {
			t.Errorf("%s differs between runs", f)
		}
		if strings.Contains(a.stdout, os.TempDir()) {
			t.Errorf("%s contains a temp path", f)
		}
	}
}

func TestOut(t *testing.T) {
	t.Run("relative creates parents inside the project", func(t *testing.T) {
		o := sampleOptions(t, "json")
		o.Out = "reports/nested/r.json"
		r := run(t, o)
		if r.code != 1 || r.stdout != "" {
			t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
		}
		b, err := os.ReadFile(filepath.Join(o.Dir, "reports", "nested", "r.json"))
		if err != nil || decode(t, string(b)).ExitCode != 1 {
			t.Fatalf("%v %s", err, b)
		}
	})
	t.Run("relative resolves against the working directory", func(t *testing.T) {
		o := sampleOptions(t, "json")
		sub := filepath.Join(o.Dir, "src")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		o.Dir, o.Out = sub, "r.json"
		run(t, o)
		if _, err := os.Stat(filepath.Join(sub, "r.json")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("relative escape is refused", func(t *testing.T) {
		o := sampleOptions(t, "json")
		o.Out = "../r.json"
		if r := run(t, o); r.code != 2 || !strings.Contains(r.stderr, "leaves the project") {
			t.Fatalf("exit %d %q", r.code, r.stderr)
		}
	})
	t.Run("symlink directory is refused", func(t *testing.T) {
		o := sampleOptions(t, "json")
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(o.Dir, "out")); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		o.Out = "out/r.json"
		if r := run(t, o); r.code != 2 || !strings.Contains(r.stderr, "symbolic link") {
			t.Fatalf("exit %d %q", r.code, r.stderr)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 0 {
			t.Fatal("a file was written through the link")
		}
	})
	t.Run("symlink file is refused", func(t *testing.T) {
		o := sampleOptions(t, "json")
		target := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(o.Dir, "r.json")); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		o.Out = "r.json"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
		if b, _ := os.ReadFile(target); string(b) != "keep" {
			t.Fatal("the link target was overwritten")
		}
	})
	t.Run("absolute outside the project, parent exists", func(t *testing.T) {
		o := sampleOptions(t, "json")
		outside, _ := filepath.EvalSymlinks(t.TempDir())
		o.Out = filepath.Join(outside, "r.json")
		if r := run(t, o); r.code != 1 {
			t.Fatalf("exit %d %s", r.code, r.stderr)
		}
		if _, err := os.Stat(o.Out); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("absolute outside never creates directories", func(t *testing.T) {
		o := sampleOptions(t, "json")
		outside, _ := filepath.EvalSymlinks(t.TempDir())
		o.Out = filepath.Join(outside, "new", "r.json")
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
		if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
			t.Fatal("directory was created outside the project")
		}
	})
	t.Run("absolute through a symlink is refused", func(t *testing.T) {
		o := sampleOptions(t, "json")
		real, _ := filepath.EvalSymlinks(t.TempDir())
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(real, link); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		o.Out = filepath.Join(link, "r.json")
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
	t.Run("a directory is not a file", func(t *testing.T) {
		o := sampleOptions(t, "json")
		if err := os.Mkdir(filepath.Join(o.Dir, "d"), 0o755); err != nil {
			t.Fatal(err)
		}
		o.Out = "d"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
	t.Run("empty and several formats", func(t *testing.T) {
		o := sampleOptions(t, "json")
		o.Formats, o.Out = []string{"json", "sarif"}, "r.json"
		if r := run(t, o); r.code != 2 || !strings.Contains(r.stderr, "exactly one --format") {
			t.Fatalf("exit %d %q", r.code, r.stderr)
		}
		o.Formats, o.Out = []string{"json"}, "bad\x00name"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
}

func TestMultipleFormats_ConsoleToStdoutOthersToDirectory(t *testing.T) {
	o := sampleOptions(t, "console")
	o.Formats = []string{"console", "sarif", "markdown"}
	r := run(t, o)
	if r.code != 1 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	contains(t, "stdout", r.stdout, "FND-CFG-001")
	for _, f := range []string{"foundry-doctor.sarif", "foundry-doctor.md"} {
		if _, err := os.Stat(filepath.Join(o.Dir, ".foundry-doctor", "out", f)); err != nil {
			t.Error(err)
		}
	}
	// The configured formats work the same way.
	o2 := sampleOptions(t, "console")
	o2.Formats = nil
	if err := os.MkdirAll(filepath.Join(o2.Dir, ".foundry-doctor"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "version: 1\noutputs:\n  formats: [json, markdown]\n  directory: out/reports\n"
	if err := os.WriteFile(filepath.Join(o2.Dir, ".foundry-doctor", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, o2); r.stdout != "" {
		t.Fatalf("no console format configured, stdout %q", r.stdout)
	}
	if _, err := os.Stat(filepath.Join(o2.Dir, "out", "reports", "foundry-doctor.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSarifPathsAreRepositoryRelative(t *testing.T) {
	o := sampleOptions(t, "sarif")
	repo := filepath.Dir(o.Dir)
	// Put the project one level below a fake repository root.
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(repo, ".git")) })
	r := run(t, o)
	prefix := filepath.Base(o.Dir)
	contains(t, "sarif", r.stdout, `"uri": "`+prefix+`/azure.yaml"`)
}

// canary builds a secret-shaped value at run time, so no secret-like literal is in the source.
func canary(kind string) string {
	switch kind {
	case "key":
		return "sk-" + strings.Repeat("CANARY", 6)
	default:
		return "AccountKey=" + strings.Repeat("Q", 24)
	}
}

func TestCanarySecretsNeverReachOutput(t *testing.T) {
	envCanary, yamlCanary := canary("key"), canary("conn")
	files := map[string]string{
		"azure.yaml":         "name: sample\nservices:\n  api:\n    host: containerapp\n    project: ./src\n    env:\n      CONN: " + yamlCanary + "\n",
		".azure/config.json": `{"defaultEnvironment":"dev"}`,
		".azure/dev/.env":    "OPENAI_API_KEY=" + envCanary + "\nPLAIN=value-" + envCanary + "\n",
	}
	// A careless rule copies values into its evidence; redaction is the last line of defence.
	var leaky fakeRule
	leaky = fakeRule{id: "FND-CFG-001", fn: func(in *model.Input) ([]rules.Result, error) {
		v, _ := in.Environment.Get("OPENAI_API_KEY")
		ev := "OPENAI_API_KEY=" + v.Reveal() + " and " + yamlCanary
		f := rules.NewFinding(leaky, sdk.ResourceRef{Kind: "service", Name: "api", Pointer: "/services/api/env/CONN"}, ev, "rotate "+yamlCanary)
		f.Key = "token=" + envCanary
		return []rules.Result{rules.Fail(f)}, nil
	}}
	cat := catalogue(t, leaky)
	for _, format := range []string{"console", "json", "markdown", "sarif"} {
		o := base(project(t, files), cat, "FND-CFG-001")
		o.Formats, o.Debug = []string{format}, true
		r := run(t, o)
		if r.code != 1 {
			t.Fatalf("%s: exit %d", format, r.code)
		}
		notContains(t, format+" stdout", r.stdout, envCanary, yamlCanary, strings.Repeat("Q", 24))
		notContains(t, format+" stderr", r.stderr, envCanary, yamlCanary, strings.Repeat("Q", 24))
	}
	// --out, the baseline file and the multi-format directory too.
	o := base(project(t, files), cat, "FND-CFG-001")
	o.Formats, o.Out, o.WriteBaseline = []string{"json"}, "r.json", "b.yaml"
	run(t, o)
	for _, f := range []string{"r.json", "b.yaml"} {
		b, err := os.ReadFile(filepath.Join(o.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		notContains(t, f, string(b), envCanary, yamlCanary)
	}
}

func TestCanaryInInvalidYAMLAndConfigErrors(t *testing.T) {
	c := canary("key")
	dir := project(t, map[string]string{"azure.yaml": "name: [" + c + "\n"})
	r := run(t, base(dir, catalogue(t, passing("FND-CFG-001")), "FND-CFG-001"))
	notContains(t, "stdout", r.stdout, c)
	notContains(t, "stderr", r.stderr, c)
}

func TestConsoleColor(t *testing.T) {
	o := sampleOptions(t, "console")
	if r := run(t, o); strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("colour must be off by default")
	}
	o.Color = true
	if r := run(t, o); !strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("colour requested but absent")
	}
}

func TestStdoutWriteFailure(t *testing.T) {
	o := sampleOptions(t, "json")
	err := app.Run(t.Context(), o, failWriter{}, &bytes.Buffer{})
	if app.CodeOf(err) != 2 {
		t.Fatalf("exit %d", app.CodeOf(err))
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
