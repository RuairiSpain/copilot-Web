package suppress

import (
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const fpA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var now = time.Date(2026, 10, 5, 23, 0, 0, 0, time.UTC)

func TestParse(t *testing.T) {
	ok := "version: 1\nentries:\n  - ruleId: FND-NET-001\n    reason: accepted\n    owner: team-a\n    expires: 2027-01-01\n"
	tests := []struct{ name, in, wantErr string }{
		{"valid", ok, ""},
		{"empty doc", "", "empty document"},
		{"bad version", strings.Replace(ok, "version: 1", "version: 3", 1), "unsupported version"},
		{"unknown key", ok + "    extra: x\n", "suppress:"},
		{"no selector", "version: 1\nentries:\n  - reason: r\n    owner: o\n    expires: 2027-01-01\n", "ruleId or fingerprint"},
		{"bad rule id", strings.Replace(ok, "FND-NET-001", "nope", 1), "invalid ruleId"},
		{"bad fingerprint", "version: 1\nentries:\n  - fingerprint: zz\n    reason: r\n    owner: o\n    expires: 2027-01-01\n", "invalid fingerprint"},
		{"missing reason", strings.Replace(ok, "    reason: accepted\n", "", 1), "reason is required"},
		{"blank owner", strings.Replace(ok, "team-a", "' '", 1), "owner is required"},
		{"missing expiry", strings.Replace(ok, "expires: 2027-01-01\n", "", 1), "expires is required"},
		{"bad date", strings.Replace(ok, "2027-01-01", "01/01/2027", 1), "YYYY-MM-DD"},
		{"two docs", ok + "---\nversion: 1\n", "multiple documents"},
		{"too large", strings.Repeat("#", MaxBytes+1), "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestExpired(t *testing.T) {
	tests := []struct {
		expires string
		want    bool
	}{
		{"2026-10-05", false}, // expiry day still valid
		{"2026-10-04", true},
		{"2026-10-06", false},
		{"garbage", true}, // fail closed
	}
	for _, tt := range tests {
		if got := (Entry{Expires: tt.expires}).Expired(now); got != tt.want {
			t.Errorf("Expired(%s) = %v, want %v", tt.expires, got, tt.want)
		}
	}
}

func TestApply(t *testing.T) {
	f := &File{Version: 1, Entries: []Entry{
		{RuleID: "FND-NET-001", Path: "a.bicep", Reason: "r1", Owner: "o1", Expires: "2027-01-01"},
		{Fingerprint: fpA, Reason: "r2", Owner: "o2", Expires: "2027-01-01"},
		{RuleID: "FND-SEC-002", Reason: "r3", Owner: "o3", Expires: "2026-01-01"}, // expired
	}}
	fs := []sdk.Finding{
		{RuleID: "FND-NET-001", Location: sdk.Location{File: "a.bicep"}},
		{RuleID: "FND-NET-001", Location: sdk.Location{File: "b.bicep"}}, // path differs
		{RuleID: "FND-X-001", Fingerprint: fpA},
		{RuleID: "FND-SEC-002"}, // only an expired suppression: stays visible
	}
	res := f.Apply(now, fs)
	if fs[0].Suppressed == nil || fs[0].Suppressed.Owner != "o1" {
		t.Errorf("f0 not suppressed: %+v", fs[0])
	}
	if fs[1].Suppressed != nil {
		t.Error("path mismatch must not suppress")
	}
	if fs[2].Suppressed == nil {
		t.Error("fingerprint match must suppress")
	}
	if fs[3].Suppressed != nil {
		t.Error("expired suppression must not suppress")
	}
	if res.Suppressed != 2 || len(res.Expired) != 1 || res.Expired[0] != 2 {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	ef := res.Findings[0]
	if ef.RuleID != ExpiredRuleID || ef.Severity != sdk.SeverityError || ef.Fingerprint == "" {
		t.Errorf("bad expired finding: %+v", ef)
	}
}

func TestApplyNilAndEmpty(t *testing.T) {
	var nilFile *File
	fs := []sdk.Finding{{RuleID: "FND-A-001"}}
	if r := nilFile.Apply(now, fs); r.Suppressed != 0 || len(r.Findings) != 0 || fs[0].Suppressed != nil {
		t.Fatal("nil file must change nothing")
	}
	if r := (&File{Version: 1}).Apply(now, fs); r.Suppressed != 0 {
		t.Fatal("empty file must change nothing")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	f := &File{Version: 1, Entries: []Entry{
		{RuleID: "FND-B-001", Reason: "r", Owner: "o", Expires: "2027-01-01"},
		{RuleID: "FND-A-001", Reason: "r", Owner: "o", Expires: "2027-01-01"},
	}}
	a, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(a), "FND-A-001") > strings.Index(string(a), "FND-B-001") {
		t.Fatal("not sorted")
	}
	if _, err := Parse(a); err != nil {
		t.Fatal(err)
	}
	if _, err := (&File{Version: 1, Entries: []Entry{{}}}).Marshal(); err == nil {
		t.Fatal("invalid must not marshal")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\nentries:\n  - ruleId: FND-NET-001\n    reason: r\n    owner: o\n    expires: 2027-01-01\n"))
	f.Add([]byte("version: [\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := Parse(data)
		if err != nil {
			return
		}
		got.Apply(now, []sdk.Finding{{RuleID: "FND-NET-001"}})
		out, err := got.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := Parse(out); err != nil {
			t.Fatalf("reparse: %v", err)
		}
	})
}
