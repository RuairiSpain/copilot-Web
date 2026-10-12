package psrule_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters/psrule"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func inputs(t *testing.T) []psrule.Input {
	t.Helper()
	return []psrule.Input{{Name: "infra/main.bicep", Template: []byte(fixture(t, "arm-main.json"))}}
}

func byRule(fs []sdk.Finding, id string) []sdk.Finding {
	var out []sdk.Finding
	for _, f := range fs {
		if f.RuleID == id {
			out = append(out, f)
		}
	}
	return out
}

func TestParseBasic(t *testing.T) {
	got, err := psrule.Parse([]byte(fixture(t, "results-basic.sarif.json")), inputs(t), overlapIndex())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d findings: %+v", len(got), got)
	}
	kv := byRule(got, "FND-SEC-005")
	if len(kv) != 1 {
		t.Fatalf("FND-SEC-005: %+v", kv)
	}
	f := kv[0]
	wantRes := sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.KeyVault/vaults", Name: "kv1", Pointer: "/resources/1"}
	if f.Resource != wantRes {
		t.Errorf("resource %+v, want %+v", f.Resource, wantRes)
	}
	if f.Location != (sdk.Location{File: "infra/main.bicep", Pointer: "/resources/1"}) {
		t.Errorf("location %+v", f.Location)
	}
	if f.Adapter != "psrule" || f.Key != "Azure.KeyVault.SoftDelete" || f.Confidence != sdk.ConfidenceLikely {
		t.Errorf("finding %+v", f)
	}
	if !strings.Contains(f.Evidence, "PSRule Azure.KeyVault.SoftDelete") || !strings.Contains(f.Evidence, "Soft delete is not enabled") {
		t.Errorf("evidence %q", f.Evidence)
	}
	if f.Severity != "" || f.Category != "" {
		t.Errorf("mapped findings leave severity and category to the engine: %+v", f)
	}

	// One PSRule rule mapped to two catalogue rules gives one finding per rule; the two messages on the
	// same resource are joined into one finding each (no duplicate fingerprints).
	for _, id := range []string{"FND-SEC-001", "FND-SEC-002"} {
		ai := byRule(got, id)
		if len(ai) != 1 {
			t.Fatalf("%s: %+v", id, ai)
		}
		if ai[0].Resource.Type != "Microsoft.CognitiveServices/accounts" || ai[0].Resource.Pointer != "/resources/0" {
			t.Errorf("%s resource %+v", id, ai[0].Resource)
		}
		if !strings.Contains(ai[0].Evidence, "Keys are enabled. | Local authentication should be disabled.") {
			t.Errorf("messages not joined in order: %q", ai[0].Evidence)
		}
	}

	// Unmapped rule: psrule/ namespace, likely confidence, unresolved resource keeps the reported name.
	un := byRule(got, "psrule/Azure.Storage.SoftDelete")
	if len(un) != 1 {
		t.Fatalf("unmapped: %+v", got)
	}
	u := un[0]
	if u.Confidence != sdk.ConfidenceLikely || u.Key != "Azure.Storage.SoftDelete" || u.Severity != sdk.SeverityInfo ||
		u.Category != sdk.CategoryNiceToHave || u.RuleVersion != 1 {
		t.Errorf("unmapped finding %+v", u)
	}
	if u.Resource != (sdk.ResourceRef{Kind: "arm-resource", Name: "stg-from-parameter"}) || u.Location.File != "infra/main.bicep" {
		t.Errorf("unmapped resource %+v location %+v", u.Resource, u.Location)
	}
}

func TestParseNilMapperAndLevels(t *testing.T) {
	got, err := psrule.Parse([]byte(fixture(t, "results-basic.sarif.json")), inputs(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	sev := map[string]sdk.Severity{}
	for _, f := range got {
		if !strings.HasPrefix(f.RuleID, "psrule/") {
			t.Errorf("nil mapper must leave everything unmapped: %s", f.RuleID)
		}
		sev[f.Key] = f.Severity
	}
	want := map[string]sdk.Severity{"Azure.KeyVault.SoftDelete": sdk.SeverityError, "Azure.AI.DisableLocalAuth": sdk.SeverityWarning, "Azure.Storage.SoftDelete": sdk.SeverityInfo}
	if !reflect.DeepEqual(sev, want) {
		t.Errorf("severity hints %v, want %v", sev, want)
	}
	// Default level (none given) is a warning hint.
	doc := `{"version":"2.1.0","runs":[{"results":[{"ruleId":"Vendor.X","message":{"text":"m"}}]}]}`
	got, err = psrule.Parse([]byte(doc), nil, nil)
	if err != nil || len(got) != 1 || got[0].Severity != sdk.SeverityWarning || got[0].Resource != (sdk.ResourceRef{}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

type badMapper struct{}

func (badMapper) Lookup(string, string) []string {
	return []string{"../../etc", "FND-SEC-009", "FND-SEC-009", "psrule/x", "FND-SEC-003"}
}

func TestParseMapperOutputIsConstrained(t *testing.T) {
	doc := `{"version":"2.1.0","runs":[{"results":[{"ruleId":"Azure.X","message":{"text":"m"}}]}]}`
	got, err := psrule.Parse([]byte(doc), nil, badMapper{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range got {
		ids = append(ids, f.RuleID)
	}
	if !reflect.DeepEqual(ids, []string{"FND-SEC-003", "FND-SEC-009"}) {
		t.Errorf("ids %v", ids)
	}
}

func TestParseEmptyAndSkippedKinds(t *testing.T) {
	for _, name := range []string{"results-empty.sarif.json"} {
		got, err := psrule.Parse([]byte(fixture(t, name)), nil, nil)
		if err != nil || len(got) != 0 {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
	doc := `{"version":"2.1.0","runs":[{"results":[
		{"ruleId":"A.B","kind":"pass","message":{"text":"m"}},
		{"ruleId":"A.C","kind":"notApplicable","message":{"text":"m"}},
		{"ruleId":"A.D","level":"none","message":{"text":"m"}},
		{"ruleId":"A.E","kind":"review","message":{"text":"m"}}]}]}`
	got, err := psrule.Parse([]byte(doc), nil, nil)
	if err != nil || len(got) != 1 || got[0].Key != "A.E" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestParseProtocolErrors(t *testing.T) {
	big := &strings.Builder{}
	big.WriteString(`{"version":"2.1.0","runs":[{"results":[`)
	for i := 0; i <= 10000; i++ {
		if i > 0 {
			big.WriteByte(',')
		}
		fmt.Fprintf(big, `{"ruleId":"A.R%d","message":{"text":"m"}}`, i)
	}
	big.WriteString(`]}]}`)

	tests := map[string]string{
		"malformed fixture":  fixture(t, "results-malformed.sarif.json"),
		"wrong version":      fixture(t, "results-bad-version.sarif.json"),
		"hostile rule name":  fixture(t, "results-bad-rulename.sarif.json"),
		"empty":              ``,
		"not an object":      `[1,2]`,
		"wrong types":        `{"version":"2.1.0","runs":"x"}`,
		"no runs":            `{"version":"2.1.0"}`,
		"missing rule id":    `{"version":"2.1.0","runs":[{"results":[{"message":{"text":"m"}}]}]}`,
		"empty rule id":      `{"version":"2.1.0","runs":[{"results":[{"ruleId":"","message":{"text":"m"}}]}]}`,
		"rule id with space": `{"version":"2.1.0","runs":[{"results":[{"ruleId":"A B","message":{"text":"m"}}]}]}`,
		"rule id too long":   `{"version":"2.1.0","runs":[{"results":[{"ruleId":"` + strings.Repeat("a", 129) + `","message":{"text":"m"}}]}]}`,
		"unknown level":      `{"version":"2.1.0","runs":[{"results":[{"ruleId":"A.B","level":"fatal","message":{"text":"m"}}]}]}`,
		"unknown kind":       `{"version":"2.1.0","runs":[{"results":[{"ruleId":"A.B","kind":"bogus","message":{"text":"m"}}]}]}`,
		"too many results":   big.String(),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := psrule.Parse([]byte(doc), nil, nil)
			if !errors.Is(err, adapters.ErrProtocol) || got != nil {
				t.Errorf("got %v, %v; want ErrProtocol", got, err)
			}
		})
	}
}

func TestParseHostileAndRedaction(t *testing.T) {
	// Canaries are assembled at run time so no secret-shaped literal sits in the repository.
	canaries := map[string]string{
		"{{CANARY_ACCOUNTKEY}}": "Account" + "Key=" + strings.Repeat("Zq9x", 24) + "==",
		"{{CANARY_BEARER}}":     "Bear" + "er " + strings.Repeat("Tk4m", 8),
		"{{CANARY_PASSWORD}}":   "pass" + "word: " + strings.Repeat("Pw7r", 8),
	}
	raw := fixture(t, "results-hostile.sarif.json")
	for k, v := range canaries {
		raw = strings.ReplaceAll(raw, k, strings.NewReplacer(`"`, `\"`).Replace(v))
	}
	if !json.Valid([]byte(raw)) {
		t.Fatal("hostile fixture is not valid JSON after substitution")
	}
	got, err := psrule.Parse([]byte(raw), inputs(t), overlapIndex())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	all, _ := json.Marshal(got)
	for _, secret := range []string{"Zq9xZq9x", "Tk4mTk4m", "Pw7rPw7r"} {
		if strings.Contains(string(all), secret) {
			t.Errorf("secret %q survived redaction: %s", secret, all)
		}
	}
	for _, f := range got {
		for _, s := range []string{f.Evidence, f.Recommendation, f.Key, f.Resource.Name, f.Location.File} {
			for _, r := range s {
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune("<>`\n\r", r) {
					t.Errorf("unsafe rune %U in %q", r, s)
				}
			}
		}
	}
	kv := byRule(got, "FND-SEC-005")[0]
	if !strings.Contains(kv.Evidence, "‹script›alert(1)‹/script›") {
		t.Errorf("markup was not neutralised: %q", kv.Evidence)
	}
	un := byRule(got, "psrule/Vendor.Custom.Rule")[0]
	if !strings.Contains(un.Resource.Name, "‹img") || strings.Contains(un.Resource.Name, "<") {
		t.Errorf("hostile resource name %q", un.Resource.Name)
	}
	if !strings.Contains(un.Evidence, "line1 line2") {
		t.Errorf("newlines were not collapsed: %q", un.Evidence)
	}
}

func TestParseTruncatesAfterRedaction(t *testing.T) {
	secret := "Account" + "Key=" + strings.Repeat("Ab3d", 30)
	msg := strings.Repeat("x ", 240) + secret
	doc := fmt.Sprintf(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"A.B","message":{"text":%q}}]}]}`, msg)
	got, err := psrule.Parse([]byte(doc), nil, nil)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if strings.Contains(got[0].Evidence, "Ab3dAb3d") {
		t.Errorf("partial secret leaked: %q", got[0].Evidence)
	}
	long := fmt.Sprintf(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"A.B","message":{"text":%q}},{"ruleId":"A.B","message":{"text":"b"}}]}]}`, strings.Repeat("y", 900))
	got, _ = psrule.Parse([]byte(long), nil, nil)
	if len(got) != 1 || len([]rune(got[0].Evidence)) > 600 || !strings.Contains(got[0].Evidence, "...") {
		t.Errorf("evidence not bounded: %d runes", len([]rune(got[0].Evidence)))
	}
}

func TestParseJoinsAtMostFiveMessages(t *testing.T) {
	var rs []string
	for i := 0; i < 8; i++ {
		rs = append(rs, fmt.Sprintf(`{"ruleId":"A.B","message":{"text":"m%d"}}`, i))
	}
	doc := `{"version":"2.1.0","runs":[{"results":[` + strings.Join(rs, ",") + `]}]}`
	got, _ := psrule.Parse([]byte(doc), nil, nil)
	if len(got) != 1 || !strings.HasSuffix(got[0].Evidence, "m0 | m1 | m2 | m3 | m4 | ...") {
		t.Errorf("%+v", got)
	}
}

func TestParseResourceResolution(t *testing.T) {
	v2 := `{"languageVersion":"2.0","resources":{"kv/a":{"type":"Microsoft.KeyVault/vaults","name":"kvx"},"acct~1":{"type":"Microsoft.CognitiveServices/accounts","name":"ai1"}}}`
	ambiguous := `{"resources":[{"type":"Microsoft.KeyVault/vaults","name":"dup"},{"type":"Microsoft.Network/virtualNetworks","name":"dup"}]}`
	badTpl := `{"resources":"nope"}`
	noRes := `{"parameters":{}}`
	ins := []psrule.Input{
		{Name: "a.bicep", Template: []byte(v2)}, {Name: "b.bicep", Template: []byte(ambiguous)},
		{Name: "c.bicep", Template: []byte(badTpl)}, {Name: "d.bicep", Template: []byte(noRes)},
	}
	res := func(file, logical string) string {
		return fmt.Sprintf(`{"ruleId":"A.B","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":%q}},"logicalLocations":[{"name":%q}]}]}`, file, logical)
	}
	doc := `{"version":"2.1.0","runs":[{"results":[` + strings.Join([]string{
		res(`input/0001.json`, "kvx"), res(`input\0001.parameters.json`, "ai1"), res("input/0002.json", "dup"),
		res("input/0003.json", "n"), res("input/0004.json", "n"), res("input/0009.json", "kvx"), res("somewhere/else.json", "kvx"),
	}, ",") + `]}]}`
	got, err := psrule.Parse([]byte(doc), ins, nil)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ file, typ, name, ptr string }
	var rows []row
	for _, f := range got {
		rows = append(rows, row{f.Location.File, f.Resource.Type, f.Resource.Name, f.Location.Pointer})
	}
	want := []row{
		{"", "", "kvx", ""}, // artifact outside the work files and several inputs: no file, no resolution
		{"", "", "kvx", ""}, // index 0009 is out of range
		{"a.bicep", "Microsoft.CognitiveServices/accounts", "ai1", "/resources/acct~01"},
		{"a.bicep", "Microsoft.KeyVault/vaults", "kvx", "/resources/kv~1a"},
		{"b.bicep", "", "dup", ""},
		{"c.bicep", "", "n", ""},
		{"d.bicep", "", "n", ""},
	}
	// Collapse the two identical "no file" rows: grouping merges them into one finding.
	want = want[1:]
	sortRows := func(r []row) { sort.Slice(r, func(i, j int) bool { return r[i].file+r[i].name < r[j].file+r[j].name }) }
	sortRows(rows)
	sortRows(want)
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows\n got %+v\nwant %+v", rows, want)
	}
}

func TestParseDeterministic(t *testing.T) {
	first, err := psrule.Parse([]byte(fixture(t, "results-basic.sarif.json")), inputs(t), overlapIndex())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	for i := 0; i < 20; i++ {
		again, err := psrule.Parse([]byte(fixture(t, "results-basic.sarif.json")), inputs(t), overlapIndex())
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := json.Marshal(again); string(a) != string(b) {
			t.Fatalf("run %d differs:\n%s\n%s", i, a, b)
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].RuleID > first[i].RuleID {
			t.Errorf("not sorted by rule id: %s before %s", first[i-1].RuleID, first[i].RuleID)
		}
	}
}

func TestSameRuleDifferentResourcesStaySeparate(t *testing.T) {
	doc := `{"version":"2.1.0","runs":[{"results":[
		{"ruleId":"A.B","level":"warning","message":{"text":"x"},"locations":[{"logicalLocations":[{"name":"r1"}]}]},
		{"ruleId":"A.B","level":"error","message":{"text":"x"},"locations":[{"logicalLocations":[{"name":"r2"}]}]},
		{"ruleId":"A.B","level":"error","message":{"text":"y"},"locations":[{"logicalLocations":[{"name":"r2"}]}]}]}]}`
	got, err := psrule.Parse([]byte(doc), nil, nil)
	if err != nil || len(got) != 2 || got[0].Resource.Name != "r1" || got[1].Resource.Name != "r2" || got[1].Severity != sdk.SeverityError {
		t.Errorf("got %+v, %v", got, err)
	}
}
