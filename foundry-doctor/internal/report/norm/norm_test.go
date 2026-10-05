package norm_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/norm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestFilePath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""}, {".", ""}, {"./a/b.yaml", "a/b.yaml"}, {`a\b\c.bicep`, "a/b/c.bicep"},
		{"/etc/passwd", "passwd"}, {`C:\Users\x\main.bicep`, "main.bicep"}, {"a/../b", "b"}, {"/", ""},
	}
	for _, tc := range tests {
		if got := norm.FilePath(tc.in); got != tc.want {
			t.Errorf("FilePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		in      string
		oneLine bool
		want    string
	}{
		{"plain", true, "plain"},
		{"a\x1b[31mred\x1b[0mb", true, "aredb"},
		{"a\x1b]0;title\x07b", true, "ab"},
		{"a\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\b", true, "alinkb"},
		{"a\u009b31mb", true, "ab"},
		{"x\x1b", true, "x"},
		{"a\x00b\x07c\x7fd", true, "abcd"},
		{"a\r\nb\tc", true, "a b c"},
		{"a\r\nb\tc", false, "a\nb c"},
		{"\u202eevil\u202c", true, "evil"},
		{"bad\xffutf", true, "bad\uFFFDutf"},
		{"日本語 \U0001F600", true, "日本語 \U0001F600"},
		{"  spaced   out  ", true, "spaced out"},
		{"a\u2028b", true, "a b"},
	}
	for _, tc := range tests {
		if got := norm.Text(tc.in, tc.oneLine); got != tc.want {
			t.Errorf("Text(%q,%v) = %q, want %q", tc.in, tc.oneLine, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := norm.TruncateUTF16("ab\U0001F600c", 3); got != "ab" {
		t.Errorf("got %q", got)
	}
	if got := norm.TruncateUTF16("abc", 10); got != "abc" {
		t.Errorf("got %q", got)
	}
	if got := norm.TruncateRunes("abcdef", 3); got != "abc..." {
		t.Errorf("got %q", got)
	}
	if got := norm.TruncateRunes("ab", 3); got != "ab" {
		t.Errorf("got %q", got)
	}
}

func TestSortedFindings(t *testing.T) {
	in := []sdk.Finding{
		{RuleID: "B", Severity: sdk.SeverityInfo, Location: sdk.Location{File: "a"}},
		{RuleID: "A", Severity: sdk.SeverityError},
		{RuleID: "C", Severity: sdk.SeverityError, Location: sdk.Location{File: "a"}},
		{RuleID: "D", Severity: sdk.SeverityError, Location: sdk.Location{File: "0"}},
	}
	got := norm.SortedFindings(in)
	var ids []string
	for _, f := range got {
		ids = append(ids, f.RuleID)
	}
	if want := []string{"D", "C", "B", "A"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
	if in[0].RuleID != "B" {
		t.Error("input modified")
	}
}

func TestRedactPolicy(t *testing.T) {
	in := map[string]any{
		"ok": "v", "ApiToken": "s1", "list": []any{map[string]any{"password": "s2"}, "x"},
		"m": map[string]string{"clientSecret": "s3", "a": "b"}, "s": []string{"q"},
	}
	out := norm.RedactPolicy(in)
	flat := strings.Join(func() []string {
		var s []string
		for _, kv := range norm.PolicyLines(in) {
			s = append(s, kv.Key+"="+kv.Value)
		}
		return s
	}(), ";")
	for _, secret := range []string{"s1", "s2", "s3"} {
		if strings.Contains(flat, secret) {
			t.Errorf("%s leaked: %s", secret, flat)
		}
	}
	if out["ok"] != "v" || out["ApiToken"] != norm.Redacted {
		t.Errorf("out = %v", out)
	}
	if in["ApiToken"] != "s1" {
		t.Error("input modified")
	}
	if norm.RedactPolicy(nil) == nil {
		t.Error("nil result")
	}
}

func TestPolicyLines(t *testing.T) {
	got := norm.PolicyLines(map[string]any{
		"b": map[string]any{"y": 2, "x": true}, "a": []any{"p", 1}, "c": nil, "d": map[string]any{}, "e": []string{"s"},
		"f": []any{map[string]any{"k": "v"}, map[string]any{}},
	})
	var s []string
	for _, kv := range got {
		s = append(s, kv.Key+"="+kv.Value)
	}
	want := []string{"a=[p, 1]", "b.x=true", "b.y=2", "c=null", "d={}", "e=[s]", "f=[{k=v}, {}]"}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %v, want %v", s, want)
	}
}

func TestTopRulesAndMissingKeys(t *testing.T) {
	var fs []sdk.Finding
	for i, id := range []string{"R1", "R2", "R2", "R3", "R3", "R4", "R5", "R6", "R7"} {
		fs = append(fs, sdk.Finding{RuleID: id, Severity: sdk.SeverityWarning, Fingerprint: string(rune('a' + i))})
	}
	fs = append(fs, sdk.Finding{RuleID: "R1", Baselined: true}, sdk.Finding{RuleID: "R9", Suppressed: &sdk.Suppression{}})
	top := norm.TopRules(fs, 5)
	if len(top) != 5 || top[0].RuleID != "R2" || top[1].RuleID != "R3" || top[2].RuleID != "R1" {
		t.Errorf("top = %+v", top)
	}
	mk := norm.MissingProfileKeys([]sdk.SkippedCheck{
		{RuleID: "B", Reason: "profile-key-missing:k2"}, {RuleID: "A", Reason: "profile-key-missing:k2"},
		{RuleID: "A", Reason: "profile-key-missing:k2"}, {RuleID: "C", Reason: "profile-key-missing:k1"}, {RuleID: "D", Reason: "other"},
	})
	if len(mk) != 2 || mk[0].Key != "k1" || !reflect.DeepEqual(mk[1].Rules, []string{"A", "B"}) {
		t.Errorf("mk = %+v", mk)
	}
}

func TestMiscHelpers(t *testing.T) {
	if norm.Position(sdk.Location{Line: 3, Column: 4}) != "3:4" || norm.Position(sdk.Location{Line: 3}) != "3" || norm.Position(sdk.Location{Pointer: "/a"}) != "/a" || norm.Position(sdk.Location{}) != "" {
		t.Error("Position")
	}
	if got := norm.ResourceLabel(sdk.ResourceRef{Type: "T", Name: "N", Pointer: "/p"}); got != "T N /p" {
		t.Errorf("label = %q", got)
	}
	if norm.SkippedCount(&sdk.Report{Summary: sdk.Summary{Skipped: 1}, Skipped: make([]sdk.SkippedCheck, 2)}) != 2 {
		t.Error("SkippedCount")
	}
	if !norm.SensitiveKey("MyConnectionString") || norm.SensitiveKey("profile") {
		t.Error("SensitiveKey")
	}
	g := norm.GroupByFile(norm.SortedFindings([]sdk.Finding{{Location: sdk.Location{File: "b"}}, {}, {Location: sdk.Location{File: "a"}}, {Location: sdk.Location{File: "a"}}}))
	if len(g) != 3 || g[0].File != "a" || len(g[0].Findings) != 2 || g[2].File != norm.NoFile {
		t.Errorf("groups = %+v", g)
	}
}

func TestNormalisedNil(t *testing.T) {
	r := norm.Normalised(nil)
	if r.SchemaVersion != sdk.ReportSchemaVersion || r.Findings == nil || r.Skipped == nil || r.Tools == nil || r.EffectivePolicy == nil || r.Summary.SkippedByReason == nil {
		t.Errorf("not normalised: %+v", r)
	}
}
