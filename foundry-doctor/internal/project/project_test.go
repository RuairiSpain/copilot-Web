package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverPositive(t *testing.T) {
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	write(t, root, "infra/main.bicep", "")
	write(t, root, "infra/modules/b.bicep", "")
	write(t, root, "infra/main.bicepparam", "")
	write(t, root, "infra/readme.md", "")
	write(t, root, ".azure/prod/.env", "SECRET=do-not-read")
	write(t, root, ".azure/dev/config.json", "{}")
	write(t, root, ".azure/config.json", "{}")
	p, err := Discover(context.Background(), root, Options{Environment: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"infra/main.bicep", "infra/main.bicepparam", "infra/modules/b.bicep"}
	if !reflect.DeepEqual(p.InfraFiles, wantFiles) {
		t.Errorf("InfraFiles = %v", p.InfraFiles)
	}
	if !reflect.DeepEqual(p.Environments, []string{"dev", "prod"}) {
		t.Errorf("Environments = %v", p.Environments)
	}
	if !p.EnvDirExists || !p.EnvFilePresent || !p.InfraExists {
		t.Errorf("flags: %+v", p)
	}
	if string(p.AzureYAMLData) != "name: x\n" || p.AzureYAML != "azure.yaml" {
		t.Errorf("azure.yaml: %q %q", p.AzureYAML, p.AzureYAMLData)
	}
	if strings.Contains(string(p.AzureYAMLData), "SECRET") {
		t.Error("env file must never be read")
	}
}

func TestDiscoverCustomPathsAndMissing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "app/azure.yaml", "name: x\n")
	write(t, root, "app/iac/m.bicep", "")
	p, err := Discover(context.Background(), root, Options{AzureYAML: "app/azure.yaml", InfraPath: "app/iac"})
	if err != nil || len(p.InfraFiles) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	// Missing infra and .azure are fine.
	root2 := t.TempDir()
	write(t, root2, "azure.yaml", "name: x\n")
	p2, err := Discover(context.Background(), root2, Options{Environment: "dev"})
	if err != nil || p2.InfraExists || p2.EnvDirExists || len(p2.Environments) != 0 {
		t.Fatalf("%v %+v", err, p2)
	}
}

func TestDiscoverErrors(t *testing.T) {
	empty := t.TempDir()
	big := t.TempDir()
	write(t, big, "azure.yaml", strings.Repeat("a", MaxAzureYAMLSize+1))
	dirYML := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirYML, "azure.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	ok := t.TempDir()
	write(t, ok, "azure.yaml", "name: x\n")
	tests := []struct {
		name string
		root string
		opts Options
		want error
	}{
		{"missing azure.yaml", empty, Options{}, ErrNoAzureYAML},
		{"too large", big, Options{}, ErrTooLarge},
		{"directory azure.yaml", dirYML, Options{}, ErrNotRegularYML},
		{"dotdot azure path", ok, Options{AzureYAML: "../azure.yaml"}, ErrUnsafePath},
		{"absolute infra", ok, Options{InfraPath: filepath.Join(ok, "infra")}, ErrUnsafePath},
		{"rooted slash infra", ok, Options{InfraPath: "/etc"}, ErrUnsafePath},
		{"dotdot infra", ok, Options{InfraPath: "a/../../x"}, ErrUnsafePath},
		{"bad env", ok, Options{Environment: "../x"}, ErrBadEnvName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Discover(context.Background(), tt.root, tt.opts); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDiscoverSymlinksSkipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "evil.bicep", "")
	write(t, root, "azure.yaml", "name: x\n")
	write(t, root, "infra/main.bicep", "")
	if err := os.Symlink(outside, filepath.Join(root, "infra", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p, err := Discover(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.InfraFiles, []string{"infra/main.bicep"}) {
		t.Errorf("InfraFiles = %v", p.InfraFiles)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "skipped symlink") {
		t.Errorf("Warnings = %v", p.Warnings)
	}
}

func TestDiscoverSymlinkAzureYAMLRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "azure.yaml", "name: x\n")
	if err := os.Symlink(filepath.Join(outside, "azure.yaml"), filepath.Join(root, "azure.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Discover(context.Background(), root, Options{}); !errors.Is(err, ErrNotRegularYML) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverFileLimitAndCancel(t *testing.T) {
	root := t.TempDir()
	write(t, root, "azure.yaml", "name: x\n")
	for _, n := range []string{"a", "b", "c"} {
		write(t, root, "infra/"+n+".bicep", "")
	}
	p, err := Discover(context.Background(), root, Options{MaxFiles: 2})
	if err != nil || len(p.InfraFiles) != 2 || len(p.Warnings) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, root, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func FuzzCleanRel(f *testing.F) {
	for _, s := range []string{"", "infra", "../x", "/abs", "a/./b", "a\x00b", `C:\x`, "a/../../b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := cleanRel(s, "infra")
		if err != nil {
			return
		}
		if strings.HasPrefix(got, "/") || got == ".." || strings.HasPrefix(got, "../") || strings.Contains(got, "\x00") {
			t.Fatalf("cleanRel(%q) = %q escapes", s, got)
		}
	})
}
