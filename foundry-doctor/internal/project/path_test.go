package project

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRel(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "infra/main.bicep", "infra/main.bicep", false},
		{"dot segments and doubles", "./infra//./main.bicep", "infra/main.bicep", false},
		{"windows slashes", `infra\modules\a.bicep`, "infra/modules/a.bicep", false},
		{"mixed slashes", `infra\modules/a.bicep`, "infra/modules/a.bicep", false},
		{"dot only", ".", ".", false},
		{"dot slash only", "./", ".", false},
		{"hidden dir", ".azure/dev/.env", ".azure/dev/.env", false},
		{"empty", "", "", true},
		{"nul", "a\x00b", "", true},
		{"control", "a\nb", "", true},
		{"del", "a\x7fb", "", true},
		{"absolute", "/etc/passwd", "", true},
		{"windows absolute backslash", `\Windows\win.ini`, "", true},
		{"unc", `\\server\share\f`, "", true},
		{"drive", `C:\x`, "", true},
		{"drive relative", "c:x", "", true},
		{"parent", "../x", "", true},
		{"parent mid", "a/../../x", "", true},
		{"parent mid windows", `a\..\x`, "", true},
		{"parent only", "..", "", true},
		{"stream", "a.txt:stream", "", true},
		{"reserved con", "CON", "", true},
		{"reserved lower nul ext", "dir/nul.txt", "", true},
		{"reserved com1", "x/COM1", "", true},
		{"reserved lpt9 ext", "lpt9.log", "", true},
		{"reserved superscript", "COM\u00b9", "", true},
		{"reserved trailing space", "aux ", "", true},
		{"trailing dot", "foo.", "", true},
		{"trailing space", "foo ", "", true},
		{"not reserved prefix", "console.txt", "console.txt", false},
		{"not reserved com10", "com10", "com10", false},
		{"too long", strings.Repeat("a/", maxRelPath), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateRel(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("want ErrUnsafePath, got %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	good := []string{"dev", "prod-eu", "env.1", "A_b"}
	bad := []string{"", ".", "..", "a/b", `a\b`, "a\x00", "CON", "x:y", "a ", ".."}
	for _, n := range good {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidateName(n); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("ValidateName(%q) = %v, want ErrUnsafePath", n, err)
		}
	}
}

func TestCaseCollisions(t *testing.T) {
	got := CaseCollisions([]string{"dev", "Dev", "prod", "DEV", "test"})
	if len(got) != 1 || strings.Join(got[0], ",") != "DEV,Dev,dev" {
		t.Fatalf("got %v", got)
	}
	if got := CaseCollisions([]string{"a", "b"}); got != nil {
		t.Fatalf("want none, got %v", got)
	}
}

func TestLimitsDefaults(t *testing.T) {
	l := buildLimits([]Option{WithLimits(Limits{MaxFileBytes: 10})})
	d := DefaultLimits()
	if l.MaxFileBytes != 10 || l.MaxEnvBytes != d.MaxEnvBytes || l.MaxListedFiles != d.MaxListedFiles ||
		l.MaxARMSniffs != d.MaxARMSniffs || l.MaxAncestors != d.MaxAncestors {
		t.Fatalf("unexpected limits %+v", l)
	}
}
