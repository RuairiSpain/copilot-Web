package config_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name, doc string
		want      []string // all must appear in the error text
	}{
		{"empty", "", []string{"empty"}},
		{"only comment", "# nothing\n", []string{"empty"}},
		{"top level list", "- a\n", []string{"top level must be a mapping"}},
		{"top level scalar", "hello\n", []string{"top level must be a mapping"}},
		{"unknown top key", "version: 1\nprofil: dev\n", []string{":2:", `unknown key "profil"`, "allowed keys"}},
		{"unknown nested key", "version: 1\npolicy:\n  network:\n    publicAcces: allowed\n", []string{":4:", "policy.network.publicAcces"}},
		{"unknown key in env", "version: 1\nenvironments:\n  prod:\n    profile: dev\n", []string{":4:", "environments.prod.profile"}},
		{"unknown key in list item", "version: 1\npolicy:\n  tags:\n    required:\n      - name: a\n        regex: x\n", []string{":6:", "required[0].regex"}},
		{"duplicate key", "version: 1\nversion: 1\n", []string{":2:", "duplicate key", "line 1"}},
		{"duplicate nested key", "version: 1\npolicy:\n  resourceScope: any\n  resourceScope: any\n", []string{":4:", "policy.resourceScope", "duplicate"}},
		{"two documents", "version: 1\n---\nversion: 1\n", []string{"more than one YAML document"}},
		{"anchor", "version: 1\npolicy: &p\n  resourceScope: any\n", []string{":2:", "anchors"}},
		{"alias", "version: 1\nx: &a [1]\ny: *a\n", []string{"anchors", "aliases"}},
		{"merge key", "version: 1\nrules:\n  <<: {include: [a]}\n", []string{"merge keys"}},
		{"explicit tag", "version: 1\nprofile: !!str dev\n", []string{"tags"}},
		{"non-scalar key", "version: 1\n? [a]\n: b\n", []string{"plain scalars"}},
		{"wrong type scalar", "version: one\n", []string{"cannot unmarshal"}},
		{"wrong type list", "version: 1\nrules:\n  include: must-have\n", []string{"cannot unmarshal"}},
		{"wrong type int", "version: 1\npolicy:\n  logRetention:\n    minimumDays: [1]\n", []string{"cannot unmarshal"}},
		{"toggle mapping", "version: 1\nadvanced:\n  psrule:\n    enabled: {a: b}\n", []string{"auto, true or false"}},
		{"syntax error", "version: 1\n  bad: [\n", []string{"config t.yaml"}},
		{"NUL byte", "version: 1\x00\n", []string{"NUL"}},
		{"tab indentation", "version: 1\npolicy:\n\tresourceScope: any\n", []string{"config t.yaml"}},
		{"missing version", "profile: dev\n", []string{"version: required"}},
		{"future version", "version: 2\n", []string{":1:", "unsupported version 2"}},
		{"explicit null version", "version: null\n", []string{"version: required"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := config.Parse("t.yaml", []byte(c.doc))
			if err == nil || f != nil {
				t.Fatalf("Parse = %v, %v; want an error", f, err)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			var ec config.ExitCoder
			if !errors.As(err, &ec) || ec.ExitCode() != sdk.ExitCannotRun {
				t.Errorf("error is not in the exit-2 class: %T", err)
			}
		})
	}
}

func TestParseAliasBomb(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 1\nx0: &a0 [\"lol\"]\n")
	for i := 1; i < 10; i++ {
		b.WriteString("x" + string(rune('0'+i)) + ": &a" + string(rune('0'+i)) + " [")
		for j := 0; j < 9; j++ {
			b.WriteString("*a" + string(rune('0'+i-1)) + ",")
		}
		b.WriteString("*a" + string(rune('0'+i-1)) + "]\n")
	}
	_, err := config.Parse("bomb.yaml", []byte(b.String()))
	if err == nil || !strings.Contains(err.Error(), "anchors") {
		t.Fatalf("alias bomb not rejected: %v", err)
	}
}

func TestParseDeepAndWide(t *testing.T) {
	deep := "version: 1\nx: " + strings.Repeat("[", 60) + strings.Repeat("]", 60) + "\n"
	if _, err := config.Parse("d.yaml", []byte(deep)); err == nil {
		t.Error("deep nesting accepted")
	}
	deepMap := "version: 1\nenvironments:\n"
	indent := "  "
	for i := 0; i < 40; i++ {
		deepMap += indent + "k" + string(rune('a'+i%26)) + ":\n"
		indent += "  "
	}
	if _, err := config.Parse("d.yaml", []byte(deepMap)); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Errorf("deep mapping: %v", err)
	}
	wide := "version: 1\nrules:\n  exclude:\n" + strings.Repeat("    - FND-CFG-001\n", 21000)
	if len(wide) > config.MaxFileBytes {
		wide = "version: 1\nrules:\n  exclude: [" + strings.Repeat("a,", 21000) + "a]\n"
	}
	_, err := config.Parse("w.yaml", []byte(wide))
	if err == nil {
		t.Error("21000-node document accepted")
	}
}

func TestParseTooLarge(t *testing.T) {
	big := "version: 1\n# " + strings.Repeat("x", config.MaxFileBytes) + "\n"
	_, err := config.Parse("big.yaml", []byte(big))
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseReportsAllProblemsSorted(t *testing.T) {
	doc := "version: 1\nprofile: staging\npolicy:\n  resourceScope: galaxy\n  logRetention:\n    minimumDays: -3\n"
	_, err := config.Parse("t.yaml", []byte(doc))
	var inv *config.InvalidError
	if !errors.As(err, &inv) || len(inv.Problems) != 3 {
		t.Fatalf("err = %#v", err)
	}
	lines := []int{inv.Problems[0].Line, inv.Problems[1].Line, inv.Problems[2].Line}
	if lines[0] != 2 || lines[1] != 4 || lines[2] != 6 {
		t.Errorf("lines = %v, want [2 4 6]", lines)
	}
	var one *config.Error
	if !errors.As(err, &one) || one.Path != "profile" {
		t.Errorf("errors.As found %#v", one)
	}
}

func TestParseAccepts(t *testing.T) {
	cases := []struct{ name, doc string }{
		{"minimal", "version: 1\n"},
		{"toggle bool and string", "version: 1\nadvanced:\n  psrule:\n    enabled: auto\n  checkov:\n    enabled: false\n"},
		{"explicit empty lists", "version: 1\npolicy:\n  allowedExternalScopes: []\n  managedByAzurePolicy: []\n"},
		{"null block", "version: 1\npolicy:\nrules:\n"},
		{"flow style", "version: 1\npolicy: {network: {publicAccess: allowed}}\n"},
		{"comments and CRLF", "# c\r\nversion: 1 # v\r\nprofile: prod\r\n"},
		{"env with dots", "version: 1\nenvironments:\n  prod.eu-1:\n    policy:\n      logRetention:\n        minimumDays: 0\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := config.Parse("t.yaml", []byte(c.doc)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseKeepsEmptyListDistinctFromMissing(t *testing.T) {
	f, err := config.Parse("t.yaml", []byte("version: 1\npolicy:\n  models:\n    allow: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Policy.Models.Allow == nil || len(f.Policy.Models.Allow) != 0 {
		t.Errorf("allow = %#v, want empty non-nil", f.Policy.Models.Allow)
	}
	if f.Policy.Models.Deny != nil {
		t.Errorf("deny = %#v, want nil", f.Policy.Models.Deny)
	}
}

func TestLoadFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.yaml", "version: 1\nprofile: test\n")
	f, err := config.Load(ctx, good)
	if err != nil || f.Profile != "test" {
		t.Fatalf("Load = %v, %v", f, err)
	}

	t.Run("missing", func(t *testing.T) {
		_, err := config.Load(ctx, filepath.Join(dir, "none.yaml"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v", err)
		}
		if f, err := config.LoadOptional(ctx, filepath.Join(dir, "none.yaml")); f != nil || err != nil {
			t.Errorf("LoadOptional = %v, %v", f, err)
		}
	})
	t.Run("optional still fails on bad content", func(t *testing.T) {
		p := write("bad.yaml", "version: 1\nnope: 1\n")
		if _, err := config.LoadOptional(ctx, p); err == nil || !strings.Contains(err.Error(), "bad.yaml:2") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(dir, "link.yaml")
		if err := os.Symlink(good, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		_, err := config.Load(ctx, link)
		var ec config.ExitCoder
		if err == nil || !strings.Contains(err.Error(), "symlink") || !errors.As(err, &ec) || ec.ExitCode() != 2 {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		if _, err := config.Load(ctx, dir); err == nil || !strings.Contains(err.Error(), "regular") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("huge file", func(t *testing.T) {
		p := write("huge.yaml", "version: 1\n# "+strings.Repeat("x", config.MaxFileBytes)+"\n")
		if _, err := config.Load(ctx, p); err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := config.Load(c, good); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		p := write("noread.yaml", "version: 1\n")
		if err := os.Chmod(p, 0); err != nil {
			t.Skip()
		}
		if f, err := os.Open(p); err == nil {
			f.Close()
			t.Skip("running as a user that can read mode 0 files")
		}
		if _, err := config.Load(ctx, p); err == nil {
			t.Error("unreadable file accepted")
		}
	})
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\npolicy:\n  tags:\n    required:\n      - name: a\n        format: '['\n"))
	f.Add([]byte("a: &a [*a]\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		got, err := config.Parse("f.yaml", b)
		if (got == nil) == (err == nil) {
			t.Fatalf("exactly one of file and error must be set: %v %v", got, err)
		}
	})
}
