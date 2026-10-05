package baseline

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const fpA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const fpB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestParse(t *testing.T) {
	tests := []struct {
		name, in, wantErr string
	}{
		{"valid", "version: 1\nentries:\n  - fingerprint: " + fpA + "\n", ""},
		{"empty entries", "version: 1\nentries: []\n", ""},
		{"empty doc", "", "empty document"},
		{"bad version", "version: 2\nentries: []\n", "unsupported version"},
		{"missing version", "entries: []\n", "unsupported version"},
		{"unknown key", "version: 1\nentries: []\nextra: 1\n", "baseline:"},
		{"bad fingerprint", "version: 1\nentries:\n  - fingerprint: XYZ\n", "invalid fingerprint"},
		{"uppercase fingerprint", "version: 1\nentries:\n  - fingerprint: " + strings.ToUpper(fpA) + "\n", "invalid fingerprint"},
		{"duplicate", "version: 1\nentries:\n  - fingerprint: " + fpA + "\n  - fingerprint: " + fpA + "\n", "duplicate"},
		{"two docs", "version: 1\nentries: []\n---\nversion: 1\n", "multiple documents"},
		{"malformed", "version: [", "baseline:"},
		{"too large", strings.Repeat("#", MaxBytes+1), "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestMarshalDeterministicRoundTrip(t *testing.T) {
	f := &File{Version: 1, Entries: []Entry{{Fingerprint: fpB, RuleID: "FND-X-001"}, {Fingerprint: fpA}}}
	a, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.Marshal()
	if string(a) != string(b) {
		t.Fatal("marshal not deterministic")
	}
	if strings.Index(string(a), fpA) > strings.Index(string(a), fpB) {
		t.Fatalf("entries not sorted:\n%s", a)
	}
	back, err := Parse(a)
	if err != nil || len(back.Entries) != 2 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	if _, err := (&File{Version: 1, Entries: []Entry{{Fingerprint: "bad"}}}).Marshal(); err == nil {
		t.Fatal("invalid entries must not marshal")
	}
}

func TestFromFindings(t *testing.T) {
	fs := []sdk.Finding{{RuleID: "R", Fingerprint: fpB}, {RuleID: "R", Fingerprint: fpA}, {RuleID: "R", Fingerprint: fpA}, {RuleID: "R"}}
	f := FromFindings(fs)
	if len(f.Entries) != 2 || f.Entries[0].Fingerprint != fpA {
		t.Fatalf("entries = %+v", f.Entries)
	}
}

func TestApplyHidesOnlyMatching(t *testing.T) {
	f := &File{Version: 1, Entries: []Entry{{Fingerprint: fpA}, {Fingerprint: fpB}}}
	fs := []sdk.Finding{
		{RuleID: "R1", Fingerprint: fpA},
		{RuleID: "R1", Fingerprint: "cccccccccccccccccccccccccccccccc"}, // same rule, new fingerprint
		{RuleID: "R2", Fingerprint: ""},
	}
	res := f.Apply(fs)
	if !fs[0].Baselined || fs[1].Baselined || fs[2].Baselined {
		t.Fatalf("baselined flags wrong: %+v", fs)
	}
	if res.Baselined != 1 || len(res.Stale) != 1 || res.Stale[0] != fpB {
		t.Fatalf("result = %+v", res)
	}
	var nilFile *File
	fs2 := []sdk.Finding{{Fingerprint: fpA}}
	if r := nilFile.Apply(fs2); r.Baselined != 0 || fs2[0].Baselined {
		t.Fatal("nil baseline must change nothing")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\nentries:\n  - fingerprint: " + fpA + "\n"))
	f.Add([]byte("version: [\n"))
	f.Add([]byte("&a [*a]"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := Parse(data)
		if err != nil {
			return
		}
		out, err := got.Marshal()
		if err != nil {
			t.Fatalf("parsed baseline failed to marshal: %v", err)
		}
		if _, err := Parse(out); err != nil {
			t.Fatalf("re-parse failed: %v", err)
		}
	})
}
