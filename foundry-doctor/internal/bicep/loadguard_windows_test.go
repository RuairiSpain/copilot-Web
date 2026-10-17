//go:build windows

package bicep

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkJunction creates a directory junction (mklink /J), which needs no
// privilege. Since Go 1.23 os.Lstat reports it as os.ModeIrregular, not
// os.ModeSymlink.
func mkJunction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		// A junction needs no privilege, so failing to create one is fatal on
		// CI and a skip only locally.
		skipOrFailLinks(t, errors.New(strings.TrimSpace(string(out))+": "+err.Error()))
	}
}

func TestWindowsJunctionReportedAsLinkOrIrregular(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "junc")
	mkJunction(t, link, target)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Fatalf("junction mode %v has neither ModeSymlink nor ModeIrregular", fi.Mode())
	}
}

func TestWindowsJunctionModuleIsRefused(t *testing.T) {
	root, base := guardProject(t, fileSet{
		"infra/main.bicep":   "module m './ext/m.bicep' = {\n  name: 'm'\n}\n",
		"infra/real/m.bicep": "param a string\n"})
	mkJunction(t, filepath.Join(base, "proj", "infra", "ext"), filepath.Join(base, "proj", "infra", "real"))
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
		t.Fatalf("module through a junction must be refused, got %v", err)
	}
}

func TestWindowsJunctionOutsideModuleIsRefused(t *testing.T) {
	root, base := guardProject(t, fileSet{"infra/main.bicep": "module m './ext/m.bicep' = {\n  name: 'm'\n}\n"})
	outDir := filepath.Join(base, "outmods")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "m.bicep"), []byte("param a string\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mkJunction(t, filepath.Join(base, "proj", "infra", "ext"), outDir)
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
		t.Fatalf("module through a junction to outside must be refused, got %v", err)
	}
}

func TestWindowsJunctionToSensitiveDirIsRefused(t *testing.T) {
	for _, dir := range []string{".azure", ".git"} {
		t.Run(dir, func(t *testing.T) {
			root, base := guardProject(t, fileSet{
				"infra/main.bicep": "var x = loadTextContent('az/dev/.env')\n",
				dir + "/dev/.env":  "A=1"})
			mkJunction(t, filepath.Join(base, "proj", "infra", "az"), filepath.Join(base, "proj", dir))
			var ge *LoadGuardError
			if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
				t.Fatalf("junction into %s must be refused, got %v", dir, err)
			}
		})
	}
}
