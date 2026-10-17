package bicep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fileSet map[string]string

func guardProject(t *testing.T, files fileSet) (*os.Root, string) {
	t.Helper()
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(proj, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, base
}

const esc = "var x = loadTextContent('../../outside.txt')\n"

func TestGuardFileLoads(t *testing.T) {
	cases := []struct {
		name    string
		files   fileSet
		start   string
		refused bool
	}{
		{"no loads", fileSet{"infra/main.bicep": "param a string\n"}, "infra/main.bicep", false},
		{"load inside project", fileSet{"infra/main.bicep": "var x = loadTextContent('./data/a.txt')\n", "infra/data/a.txt": "hi"}, "infra/main.bicep", false},
		{"load with encoding arg", fileSet{"infra/main.bicep": "var x = loadTextContent('a.txt', 'utf-8')\n", "infra/a.txt": "hi"}, "infra/main.bicep", false},
		{"up and in stays inside", fileSet{"infra/main.bicep": "var x = loadJsonContent('../cfg/a.json')\n", "cfg/a.json": "{}"}, "infra/main.bicep", false},
		{"directory info on dir", fileSet{"infra/main.bicep": "var x = loadDirectoryFileInfo('d')\n", "infra/d/f.txt": "1"}, "infra/main.bicep", false},
		{"escaping path", fileSet{"infra/main.bicep": "param a = loadTextContent('../../outside.txt')\n"}, "infra/main.bicep", true},
		{"escaping path in bicepparam", fileSet{
			"infra/main.bicepparam": "using './main.bicep'\nparam accountName = loadTextContent('../../outside.txt')\n",
			"infra/main.bicep":      "param accountName string\n"}, "infra/main.bicepparam", true},
		{"absolute posix", fileSet{"infra/main.bicep": "var x = loadFileAsBase64('/etc/passwd')\n"}, "infra/main.bicep", true},
		{"absolute drive", fileSet{"infra/main.bicep": "var x = loadYamlContent('C:/Windows/win.ini')\n"}, "infra/main.bicep", true},
		{"unc", fileSet{"infra/main.bicep": "var x = loadTextContent('//host/share/x')\n"}, "infra/main.bicep", true},
		{"backslash", fileSet{"infra/main.bicep": "var x = loadTextContent('..\\\\..\\\\outside.txt')\n"}, "infra/main.bicep", true},
		{"non-literal", fileSet{"infra/main.bicep": "param p string\nvar x = loadTextContent(p)\n"}, "infra/main.bicep", true},
		{"interpolated", fileSet{"infra/main.bicep": "param p string\nvar x = loadTextContent('${p}.txt')\n"}, "infra/main.bicep", true},
		{"concat", fileSet{"infra/main.bicep": "var x = loadTextContent(concat('a','b'))\n"}, "infra/main.bicep", true},
		{"missing target", fileSet{"infra/main.bicep": "var x = loadTextContent('nope.txt')\n"}, "infra/main.bicep", true},
		{"directory for file load", fileSet{"infra/main.bicep": "var x = loadTextContent('d')\n", "infra/d/f.txt": "1"}, "infra/main.bicep", true},
		{"mixed case name", fileSet{"infra/main.bicep": "var x = LOADTEXTCONTENT('../../outside.txt')\n"}, "infra/main.bicep", true},
		{"call in interpolation", fileSet{"infra/main.bicep": "var x = 'a${loadTextContent('../../outside.txt')}b'\n"}, "infra/main.bicep", true},
		{"call after comment marker in string", fileSet{"infra/main.bicep": "var s = '//'\n" + esc}, "infra/main.bicep", true},
		{"unterminated comment", fileSet{"infra/main.bicep": "/* never closed loadTextContent('a')\n"}, "infra/main.bicep", true},
		{"in line comment", fileSet{"infra/main.bicep": "// loadTextContent('../../outside.txt')\nparam a string\n"}, "infra/main.bicep", false},
		{"in block comment", fileSet{"infra/main.bicep": "/* loadTextContent('../../outside.txt') */\nparam a string\n"}, "infra/main.bicep", false},
		{"in string", fileSet{"infra/main.bicep": "var s = 'loadTextContent(\\'../../outside.txt\\')'\n"}, "infra/main.bicep", false},
		{"in multiline string", fileSet{"infra/main.bicep": "var s = '''\nloadTextContent('../../outside.txt')\n'''\n"}, "infra/main.bicep", false},
		{"bare name without call", fileSet{"infra/main.bicep": "var loadTextContentNote = 1\n"}, "infra/main.bicep", false},
		{"hostile module reached from entry", fileSet{
			"infra/main.bicep":  "module m './mod/m.bicep' = {\n  name: 'm'\n}\n",
			"infra/mod/m.bicep": "var x = loadTextContent('../../../outside.txt')\n"}, "infra/main.bicep", true},
		{"benign module with load", fileSet{
			"infra/main.bicep":  "module m 'sub/m.bicep' = {\n  name: 'm'\n}\n",
			"infra/sub/m.bicep": "var x = loadTextContent('ok.txt')\n", "infra/sub/ok.txt": "1"}, "infra/main.bicep", false},
		{"module reference escaping root", fileSet{"infra/main.bicep": "module m '../../shared/m.bicep' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", true},
		{"module cycle terminates", fileSet{
			"infra/a.bicep": "module b './b.bicep' = {\n  name: 'b'\n}\n",
			"infra/b.bicep": "module a './a.bicep' = {\n  name: 'a'\n}\n"}, "infra/a.bicep", false},
		{"bicepparam using hostile main", fileSet{
			"infra/main.bicepparam": "using './main.bicep'\nparam a = 'x'\n",
			"infra/main.bicep":      esc + "param a string\n"}, "infra/main.bicepparam", true},
		{"block comment before paren", fileSet{"infra/main.bicep": "var x = loadTextContent/* x */('../../outside.txt')\n"}, "infra/main.bicep", true},
		{"block comment before paren in bicepparam", fileSet{
			"infra/main.bicepparam": "using './main.bicep'\nparam a = loadTextContent/* x */('../../outside.txt')\n",
			"infra/main.bicep":      "param a string\n"}, "infra/main.bicepparam", true},
		{"space and comments before paren, inside project", fileSet{"infra/main.bicep": "var x = loadTextContent /* a */ /* b */ ('ok.txt')\n", "infra/ok.txt": "1"}, "infra/main.bicep", false},
		{"load name not followed by paren", fileSet{"infra/main.bicep": "var loadTextContent = 1\n"}, "infra/main.bicep", true},
		{"line comment then paren is refused", fileSet{"infra/main.bicep": "var x = loadTextContent// c\n('ok.txt')\n", "infra/ok.txt": "1"}, "infra/main.bicep", true},
		{"unterminated comment after name", fileSet{"infra/main.bicep": "var x = loadTextContent/* never\n"}, "infra/main.bicep", true},
		{"load from .azure", fileSet{"infra/main.bicep": "var x = loadTextContent('../.azure/dev/.env')\n", ".azure/dev/.env": "A=1"}, "infra/main.bicep", true},
		{"load from nested .azure", fileSet{"infra/main.bicep": "var x = loadJsonContent('.AZURE/c.json')\n", "infra/.AZURE/c.json": "{}"}, "infra/main.bicep", true},
		{"directory .azure", fileSet{"main.bicep": "var x = loadDirectoryFileInfo('.azure')\n", ".azure/a": "1"}, "main.bicep", true},
		{"similar name is allowed", fileSet{"infra/main.bicep": "var x = loadTextContent('.azure-notes.txt')\n", "infra/.azure-notes.txt": "1"}, "infra/main.bicep", false},
		{"json module escaping root", fileSet{"infra/main.bicep": "module m '../../outmod.json' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", true},
		{"json module inside root", fileSet{"infra/main.bicep": "module m './t/m.json' = {\n  name: 'm'\n}\n", "infra/t/m.json": "{}"}, "infra/main.bicep", false},
		{"json module with comment-like text is not scanned", fileSet{"infra/main.bicep": "module m './m.json' = {\n  name: 'm'\n}\n", "infra/m.json": "{\"a\": \"/* x\"}"}, "infra/main.bicep", false},
		{"module txt outside root", fileSet{"infra/main.bicep": "module m '../../outmod.txt' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", true},
		{"module extensionless outside root", fileSet{"infra/main.bicep": "module m '../../outmod' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", true},
		{"using txt outside root", fileSet{"infra/main.bicepparam": "using '../../x.txt'\n"}, "infra/main.bicepparam", true},
		{"module txt inside root", fileSet{"infra/main.bicep": "module m './m.txt' = {\n  name: 'm'\n}\n", "infra/m.txt": "param a string\n"}, "infra/main.bicep", false},
		{"module txt inside root with hostile load", fileSet{"infra/main.bicep": "module m './m.txt' = {\n  name: 'm'\n}\n", "infra/m.txt": esc}, "infra/main.bicep", true},
		{"registry module is not a path", fileSet{"infra/main.bicep": "module m 'br:mcr.microsoft.com/bicep/avm/res/x:1.0.0' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", false},
		{"registry alias module is not a path", fileSet{"infra/main.bicep": "module m 'br/public:avm/res/x:1.0.0' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", false},
		{"template spec module is not a path", fileSet{"infra/main.bicep": "module m 'ts:sub/rg/spec:1' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", false},
		{"load from .git", fileSet{"infra/main.bicep": "var x = loadTextContent('../.git/config')\n", ".git/config": "[core]"}, "infra/main.bicep", true},
		{"load from nested .GIT", fileSet{"infra/main.bicep": "var x = loadTextContent('.GIT/HEAD')\n", "infra/.GIT/HEAD": "x"}, "infra/main.bicep", true},
		{"url string does not refuse", fileSet{"infra/main.bicep": "var src = 'https://github.com/org/repo/blob/main/infra/main.bicep'\nvar p = '../../x.bicepparam'\nvar j = '../../x.json'\n"}, "infra/main.bicep", false},
		{"url string and real escaping module", fileSet{"infra/main.bicep": "var src = 'https://github.com/o/r/main.bicep'\nmodule m '../../x/m.bicep' = {\n  name: 'm'\n}\n"}, "infra/main.bicep", true},
		{"url string and hostile real module", fileSet{
			"infra/main.bicep": "var src = 'https://github.com/o/r/main.bicep'\nmodule m './m.bicep' = {\n  name: 'm'\n}\n",
			"infra/m.bicep":    esc}, "infra/main.bicep", true},
		{"extends escaping root", fileSet{"infra/main.bicepparam": "using './main.bicep'\nextends '../../base.bicepparam'\n", "infra/main.bicep": "param a string\n"}, "infra/main.bicepparam", true},
		{"import from escaping root", fileSet{"infra/main.bicep": "import { f } from '../../lib.bicep'\n"}, "infra/main.bicep", true},
		{"unreadable start", fileSet{}, "infra/main.bicep", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := guardProject(t, tc.files)
			err := GuardFileLoads(root, tc.start)
			if tc.refused {
				var ge *LoadGuardError
				if !errors.As(err, &ge) {
					t.Fatalf("want LoadGuardError, got %v", err)
				}
				if !strings.Contains(err.Error(), ReasonFileLoadOutsideProject) {
					t.Fatalf("error does not name the reason: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}

func TestGuardFileLoadsSymlinkOutside(t *testing.T) {
	root, base := guardProject(t, fileSet{"infra/main.bicep": "var x = loadTextContent('link.txt')\n"})
	link := filepath.Join(base, "proj", "infra", "link.txt")
	if err := os.Symlink(filepath.Join(base, "outside.txt"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
		t.Fatalf("symlink to outside must be refused, got %v", err)
	}
}

// A link inside the project can point at .azure or .git, which the path text
// does not show, so a load through any link is refused.
func TestGuardFileLoadsSymlinkInsideIsRefused(t *testing.T) {
	root, base := guardProject(t, fileSet{"infra/main.bicep": "var x = loadTextContent('link.txt')\n", "infra/real.txt": "1"})
	if err := os.Symlink("real.txt", filepath.Join(base, "proj", "infra", "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("a load through a link must be refused, got %v", err)
	}
}

func TestGuardFileLoadsSymlinkToSensitiveDirIsRefused(t *testing.T) {
	for _, dir := range []string{".azure", ".git"} {
		t.Run(dir, func(t *testing.T) {
			root, base := guardProject(t, fileSet{
				"infra/main.bicep": "var x = loadTextContent('az/dev/.env')\n",
				dir + "/dev/.env":  "A=1"})
			if err := os.Symlink(filepath.Join("..", dir), filepath.Join(base, "proj", "infra", "az")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var ge *LoadGuardError
			if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
				t.Fatalf("symlink into %s must be refused, got %v", dir, err)
			}
		})
	}
}

// S1: a module reached through a directory link that points outside the root
// is refused, not skipped; the compiler would follow the link.
func TestGuardFileLoadsModuleThroughOutsideDirLinkIsRefused(t *testing.T) {
	root, base := guardProject(t, fileSet{"infra/main.bicep": "module m './ext/m.bicep' = {\n  name: 'm'\n}\n"})
	outDir := filepath.Join(base, "outmods")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "m.bicep"), []byte("param a string\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outDir, filepath.Join(base, "proj", "infra", "ext")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) || !strings.Contains(err.Error(), ReasonFileLoadOutsideProject) {
		t.Fatalf("module through an outside directory link must be refused, got %v", err)
	}
}

func TestGuardFileLoadsModuleThroughInsideLinkIsRefused(t *testing.T) {
	root, base := guardProject(t, fileSet{
		"infra/main.bicep":   "module m './ext/m.bicep' = {\n  name: 'm'\n}\n",
		"infra/real/m.bicep": "param a string\n"})
	if err := os.Symlink("real", filepath.Join(base, "proj", "infra", "ext")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var ge *LoadGuardError
	if err := GuardFileLoads(root, "infra/main.bicep"); !errors.As(err, &ge) {
		t.Fatalf("module through a link must be refused, got %v", err)
	}
}

// A missing module stays the compiler's diagnostic; an unreadable one is refused.
func TestGuardFileLoadsMissingModuleAllowedDirectoryModuleRefused(t *testing.T) {
	root, _ := guardProject(t, fileSet{"infra/main.bicep": "module m './nope.bicep' = {\n  name: 'm'\n}\n"})
	if err := GuardFileLoads(root, "infra/main.bicep"); err != nil {
		t.Fatalf("missing module: %v", err)
	}
	root2, _ := guardProject(t, fileSet{"infra/main.bicep": "module m './d' = {\n  name: 'm'\n}\n", "infra/d/x.bicep": "param a string\n"})
	var ge *LoadGuardError
	if err := GuardFileLoads(root2, "infra/main.bicep"); !errors.As(err, &ge) {
		t.Fatalf("module path that is a directory must be refused, got %v", err)
	}
}
