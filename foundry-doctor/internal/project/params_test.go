package project

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestParameterFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/main.bicep", "")
	write(t, root, "infra/main.parameters.json", "{}")
	write(t, root, "infra/main.bicepparam", "")
	write(t, root, "infra/other.bicep", "")
	write(t, root, "infra/other.parameters.json", "{}")
	if err := os.MkdirAll(filepath.Join(root, "infra", "dir.parameters.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if bp, pj := ParameterFiles(r, "infra/main.bicep"); bp != "infra/main.bicepparam" || pj != "infra/main.parameters.json" {
		t.Errorf("main: %q %q", bp, pj)
	}
	if bp, pj := ParameterFiles(r, "infra/other.bicep"); bp != "" || pj != "infra/other.parameters.json" {
		t.Errorf("other: %q %q", bp, pj)
	}
	if bp, pj := ParameterFiles(r, "infra/missing.bicep"); bp != "" || pj != "" {
		t.Errorf("missing: %q %q", bp, pj)
	}
	if _, pj := ParameterFiles(r, "infra/dir.bicep"); pj != "" {
		t.Errorf("a directory is not a parameter file: %q", pj)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "infra", "main.parameters.json"), filepath.Join(root, "infra", "link.parameters.json")); err != nil {
			t.Fatal(err)
		}
		if _, pj := ParameterFiles(r, "infra/link.bicep"); pj != "" {
			t.Errorf("a symlink must not be read: %q", pj)
		}
	}
}

func TestListInfraFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/main.bicep", "")
	write(t, root, "infra/modules/a.bicep", "")
	write(t, root, "infra/modules/z.bicep", "")
	write(t, root, "infra/main.bicepparam", "")
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	files, _, err := ListInfraFiles(context.Background(), r, "infra", 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"infra/main.bicep", "infra/modules/a.bicep", "infra/modules/z.bicep"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files %v", files)
	}
	files, warns, err := ListInfraFiles(context.Background(), r, "infra", 2)
	if err != nil || len(files) > 2 || len(warns) == 0 {
		t.Errorf("bound: %v %v %v", files, warns, err)
	}
	if files, _, err := ListInfraFiles(context.Background(), r, "nope", 0); err != nil || len(files) != 0 {
		t.Errorf("missing dir: %v %v", files, err)
	}
}
