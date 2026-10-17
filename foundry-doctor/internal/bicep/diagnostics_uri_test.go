package bicep

import (
	"runtime"
	"testing"
)

func TestFileURIPathAndSafePath(t *testing.T) {
	t.Parallel()
	// A rooted path without a drive letter is not absolute on Windows, and
	// real Windows diagnostics carry drive-letter URIs, so the inside-root
	// cases use the platform's own spelling.
	inRoot, inURI := "/work/proj", "file:///work/proj/infra/main.bicep"
	spRoot, spURI := "/work/my proj", "file:///work/my%20proj/a.bicep"
	if runtime.GOOS == "windows" {
		inRoot, inURI = `C:\work\proj`, "file:///C:/work/proj/infra/main.bicep"
		spRoot, spURI = `C:\work\my proj`, "file:///C:/work/my%20proj/a.bicep"
	}
	tests := []struct{ name, root, in, want string }{
		{"uri inside root", inRoot, inURI, "infra/main.bicep"},
		{"percent-encoded space", spRoot, spURI, "a.bicep"},
		{"posix uri outside root keeps base name", "/work/proj", "file:///etc/other/x.bicep", "x.bicep"},
		{"windows drive uri outside root keeps base name", "/work/proj", "file:///C:/other/x.bicep", "x.bicep"},
		{"relative path", "/work/proj", "infra/main.bicep", "infra/main.bicep"},
		{"empty", "/work/proj", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := safePath(tt.root, tt.in); got != tt.want {
				t.Fatalf("safePath(%q, %q) = %q, want %q", tt.root, tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSARIFSplitsDocsURL(t *testing.T) {
	t.Parallel()
	in := `{"runs":[{"results":[{"ruleId":"no-unused-params","message":{"text":"Parameter \"p\" is unused. [https://aka.ms/bicep/linter-diagnostics#no-unused-params]"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"file:///proj/main.bicep"},"region":{"startLine":2,"charOffset":7}}}]}]}]}`
	got, err := ParseSARIF([]byte(in), "/proj")
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	d := got[0]
	if d.File != "main.bicep" || d.Line != 2 || d.Column != 7 || d.Message != `Parameter "p" is unused.` ||
		d.DocsURL != "https://aka.ms/bicep/linter-diagnostics#no-unused-params" {
		t.Fatalf("unexpected diagnostic %+v", d)
	}
}
