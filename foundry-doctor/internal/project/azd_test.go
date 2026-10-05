package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// write creates dir/rel with data, making parents.
func write(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func newCtx(t *testing.T, root string, opts ...Option) *StandaloneAzdContext {
	t.Helper()
	s, err := NewStandaloneAzdContext(root, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStandaloneReadFile(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", canary)
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	write(t, root, "infra/main.bicep", "param a string\n")
	write(t, root, "bin.dat", "ab\x00cd")
	symlink(t, "infra", filepath.Join(root, "inlink"))
	symlink(t, filepath.Join(root, "infra"), filepath.Join(root, "abslink"))
	symlink(t, outside, filepath.Join(root, "outdir"))
	symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(root, "outfile"))
	symlink(t, "../../../etc/passwd", filepath.Join(root, "relescape"))
	symlink(t, "loop2", filepath.Join(root, "loop1"))
	symlink(t, "loop1", filepath.Join(root, "loop2"))
	symlink(t, "self", filepath.Join(root, "self"))
	s := newCtx(t, root, WithLimits(Limits{MaxFileBytes: 64}))
	write(t, root, "big.txt", strings.Repeat("x", 65))
	write(t, root, "exact.txt", strings.Repeat("x", 64))
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		rel     string
		want    string
		wantErr error
	}{
		{"plain", "azure.yaml", "name: x\n", nil},
		{"windows slashes", `infra\main.bicep`, "param a string\n", nil},
		{"dot prefix", "./infra/main.bicep", "param a string\n", nil},
		{"binary bytes returned as is", "bin.dat", "ab\x00cd", nil},
		{"link inside root", "inlink/main.bicep", "param a string\n", nil},
		{"absolute symlink refused even inside root", "abslink/main.bicep", "", ErrSymlinkEscape},
		{"exactly at limit", "exact.txt", strings.Repeat("x", 64), nil},
		{"over limit", "big.txt", "", ErrTooLarge},
		{"directory", "adir", "", ErrNotRegular},
		{"missing", "nope.txt", "", fs.ErrNotExist},
		{"absolute", filepath.Join(outside, "secret.txt"), "", ErrUnsafePath},
		{"parent", "../x", "", ErrUnsafePath},
		{"nul", "a\x00b", "", ErrUnsafePath},
		{"empty", "", "", ErrUnsafePath},
		{"reserved", "infra/NUL", "", ErrUnsafePath},
		{"symlinked dir escape", "outdir/secret.txt", "", ErrSymlinkEscape},
		{"symlinked file escape", "outfile", "", ErrSymlinkEscape},
		{"relative symlink escape", "relescape", "", ErrSymlinkEscape},
		{"symlink loop", "loop1", "", ErrSymlinkLoop},
		{"self loop", "self", "", ErrSymlinkLoop},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ReadFile(context.Background(), tc.rel)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), canary) {
					t.Errorf("error leaks absolute path or content: %v", err)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestStandaloneCancelled(t *testing.T) {
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	s := newCtx(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ReadFile(ctx, "azure.yaml"); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadFile: %v", err)
	}
	if _, err := s.ProjectRoot(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ProjectRoot: %v", err)
	}
	if _, err := s.Environments(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Environments: %v", err)
	}
	if _, err := s.CurrentEnvironment(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("CurrentEnvironment: %v", err)
	}
	if _, err := s.EnvValues(ctx, "dev"); !errors.Is(err, context.Canceled) {
		t.Errorf("EnvValues: %v", err)
	}
	if _, err := s.Versions(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Versions: %v", err)
	}
}

func TestStandaloneProjectRootAndVersions(t *testing.T) {
	root := t.TempDir()
	s := newCtx(t, root)
	got, err := s.ProjectRoot(context.Background())
	if err != nil || got != root {
		t.Fatalf("ProjectRoot = %q, %v", got, err)
	}
	if _, err := s.Versions(context.Background()); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("Versions err = %v", err)
	}
	var _ model.AzdContext = s
}

func TestNewStandaloneAzdContextMissingRoot(t *testing.T) {
	_, err := NewStandaloneAzdContext(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestStandaloneEnvironments(t *testing.T) {
	t.Run("missing .azure", func(t *testing.T) {
		s := newCtx(t, t.TempDir())
		got, err := s.Environments(context.Background())
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %#v, %v", got, err)
		}
	})
	t.Run("sorted dirs only", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, ".azure/prod/.env", "A=1\n")
		write(t, root, ".azure/dev/.env", "A=1\n")
		write(t, root, ".azure/staging/config.json", "{}")
		write(t, root, ".azure/config.json", `{"defaultEnvironment":"dev"}`)
		write(t, root, ".azure/stray.txt", "not an env")
		symlink(t, filepath.Join(root, ".azure", "dev"), filepath.Join(root, ".azure", "linked"))
		got, err := newCtx(t, root).Environments(context.Background())
		if err != nil || strings.Join(got, ",") != "dev,prod,staging" {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("azure is a file", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, ".azure", "i am a file")
		_, err := newCtx(t, root).Environments(context.Background())
		if !errors.Is(err, ErrNotDirectory) {
			t.Fatalf("want ErrNotDirectory, got %v", err)
		}
	})
	t.Run("azure symlink escapes", func(t *testing.T) {
		outside := t.TempDir()
		write(t, outside, "evil/.env", "A=1\n")
		root := t.TempDir()
		symlink(t, outside, filepath.Join(root, ".azure"))
		_, err := newCtx(t, root).Environments(context.Background())
		if !errors.Is(err, ErrSymlinkEscape) {
			t.Fatalf("want ErrSymlinkEscape, got %v", err)
		}
	})
	t.Run("unsafe names skipped", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, ".azure/good/.env", "A=1\n")
		write(t, root, ".azure/NUL/.env", "A=1\n")
		write(t, root, ".azure/trail./.env", "A=1\n")
		got, err := newCtx(t, root).Environments(context.Background())
		if err != nil || strings.Join(got, ",") != "good" {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

func TestStandaloneCurrentEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *string // nil: no file
		want    string
		wantErr bool
	}{
		{"set", ptr(`{"version":1,"defaultEnvironment":"dev"}`), "dev", false},
		{"missing file", nil, "", true},
		{"empty name", ptr(`{"version":1,"defaultEnvironment":""}`), "", true},
		{"absent field", ptr(`{"version":1}`), "", true},
		{"invalid json", ptr(`{not json`), "", true},
		{"path traversal name", ptr(`{"defaultEnvironment":"../x"}`), "", true},
		{"separator name", ptr(`{"defaultEnvironment":"a/b"}`), "", true},
		{"wrong type", ptr(`{"defaultEnvironment":5}`), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.cfg != nil {
				write(t, root, ".azure/config.json", *tc.cfg)
			}
			got, err := newCtx(t, root).CurrentEnvironment(context.Background())
			if tc.wantErr {
				if !errors.Is(err, model.ErrUnavailable) {
					t.Fatalf("want ErrUnavailable, got %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
	t.Run("config escapes", func(t *testing.T) {
		outside := t.TempDir()
		write(t, outside, "config.json", `{"defaultEnvironment":"evil"}`)
		root := t.TempDir()
		write(t, root, ".azure/keep", "")
		symlink(t, filepath.Join(outside, "config.json"), filepath.Join(root, ".azure", "config.json"))
		_, err := newCtx(t, root).CurrentEnvironment(context.Background())
		if !errors.Is(err, model.ErrUnavailable) || !errors.Is(err, ErrSymlinkEscape) {
			t.Fatalf("got %v", err)
		}
	})
}

func ptr(s string) *string { return &s }

func TestStandaloneEnvValues(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".azure/dev/.env", "# generated\nAZURE_ENV_NAME=\"dev\"\nTOKEN=\""+canary+"\"\n")
	write(t, root, ".azure/bad/.env", "TOKEN=\""+canary+"\nX=1\n")
	write(t, root, ".azure/bin/.env", "A=\x00"+canary)
	write(t, root, ".azure/bom/.env", "\xef\xbb\xbfA="+canary+"\n")
	write(t, root, ".azure/empty/config.json", "{}")
	write(t, root, ".azure/huge/.env", strings.Repeat("A=1\n", 10))
	s := newCtx(t, root, WithLimits(Limits{MaxEnvBytes: 16}))
	big := newCtx(t, root)

	env, err := big.EnvValues(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(env.Keys(), ",") != "AZURE_ENV_NAME,TOKEN" {
		t.Fatalf("keys %v", env.Keys())
	}
	if v, _ := env.Get("TOKEN"); v.Reveal() != canary {
		t.Fatal("value not parsed")
	}
	j, _ := json.Marshal(env)
	for _, r := range []string{env.String(), string(j), fmt.Sprintf("%v|%+v|%#v", env, env, env)} {
		if strings.Contains(r, canary) {
			t.Errorf("leak in %q", r)
		}
	}

	tests := []struct {
		name    string
		sctx    *StandaloneAzdContext
		env     string
		wantIs  error
		wantPE  bool
		wantStr string
	}{
		{"unterminated", big, "bad", nil, true, "line 1"},
		{"binary", big, "bin", nil, true, "line 1"},
		{"bom", big, "bom", nil, true, "line 1"},
		{"missing env file", big, "empty", fs.ErrNotExist, false, ""},
		{"missing env", big, "nope", fs.ErrNotExist, false, ""},
		{"traversal name", big, "../dev", ErrUnsafePath, false, ""},
		{"separator name", big, "dev/x", ErrUnsafePath, false, ""},
		{"empty name", big, "", ErrUnsafePath, false, ""},
		{"over env limit", s, "huge", ErrTooLarge, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.sctx.EnvValues(context.Background(), tc.env)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("want %v, got %v", tc.wantIs, err)
			}
			if _, ok := IsParseError(err); ok != tc.wantPE {
				t.Fatalf("parse error = %v, want %v (%v)", ok, tc.wantPE, err)
			}
			if tc.wantStr != "" && !strings.Contains(err.Error(), tc.wantStr) {
				t.Errorf("error %q lacks %q", err, tc.wantStr)
			}
			if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), root) {
				t.Errorf("error leaks value or absolute path: %v", err)
			}
		})
	}
}

func TestStandaloneEnvValuesSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "dev/.env", "TOKEN="+canary+"\n")
	root := t.TempDir()
	write(t, root, ".azure/keep", "")
	symlink(t, filepath.Join(outside, "dev"), filepath.Join(root, ".azure", "dev"))
	s := newCtx(t, root)
	_, err := s.EnvValues(context.Background(), "dev")
	if !errors.Is(err, ErrSymlinkEscape) {
		t.Fatalf("want ErrSymlinkEscape, got %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatal("leak")
	}
	// the linked directory is not listed as an environment either
	names, err := s.Environments(context.Background())
	if err != nil || len(names) != 0 {
		t.Fatalf("got %v, %v", names, err)
	}
}

func TestStandaloneEnvValuesConcurrent(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".azure/dev/.env", "A=1\n")
	write(t, root, ".azure/config.json", `{"defaultEnvironment":"dev"}`)
	s := newCtx(t, root)
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			ctx := context.Background()
			if _, err := s.EnvValues(ctx, "dev"); err != nil {
				done <- err
				return
			}
			if _, err := s.Environments(ctx); err != nil {
				done <- err
				return
			}
			_, err := s.CurrentEnvironment(ctx)
			done <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
