package cfg_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	catalogfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

// messy builds an azure.yaml that trips most rules and plants the canary in every secret-bearing place.
func messy(secret string) string {
	return `name: messy
requiredVersions:
  extensions:
    azure.ai.agents: ">=1.0.0-beta.2"
services:
  ai-project:
    host: azure.ai.project
    deployments:
      - name: gpt-a
  conn:
    host: azure.ai.connection
    category: RemoteTool
    authType: ApiKey
    target: http://user:` + secret + `@169.254.169.254/x?sig=` + secret + `
    credentials:
      key: ` + secret + `
  one:
    host: azure.ai.agent
    kind: prompt
    name: same
    model: ` + secret + `
    uses: [ai-project, nowhere]
    toolboxes: [conn]
    env:
      API_TOKEN: ` + secret + `
      FOUNDRY_X: ${UNPRODUCED}
    config:
      a: b
  two:
    host: azure.ai.agent
    kind: hosted
    name: same
    uses: [ai-project]
    image: reg.example/a:1
    codeConfiguration:
      runtime: python
    container:
      resources:
        cpu: "9"
`
}

func messyInput(t *testing.T) *model.Input {
	return yamlInput(t, messy(canary()), withEnv(map[string]string{"AZURE_LOCATION": "West US"}))
}

func all(t *testing.T, in *model.Input) []rules.Result {
	t.Helper()
	var out []rules.Result
	for _, r := range cfg.Rules() {
		out = append(out, eval(t, r.ID(), in)...)
	}
	return out
}

func TestMessyInputTripsEveryRule(t *testing.T) {
	rs := all(t, messyInput(t))
	hit := map[string]bool{}
	for _, r := range rs {
		if r.Finding != nil {
			hit[r.Finding.RuleID] = true
		}
	}
	for _, r := range cfg.Rules() {
		if r.ID() == "FND-CFG-001" {
			continue // the messy file is schema-valid apart from what other rules read
		}
		if !hit[r.ID()] {
			t.Errorf("%s produced no finding on the messy input", r.ID())
		}
	}
}

func TestMissingInput(t *testing.T) {
	for _, r := range cfg.Rules() {
		for name, in := range map[string]*model.Input{
			"nil input":        nil,
			"no azure.yaml":    {},
			"yaml no report":   {AzureYAML: &model.AzureYAML{File: "azure.yaml"}},
			"empty azure.yaml": {AzureYAML: &model.AzureYAML{File: "azure.yaml"}, YAMLReport: &model.YAMLAnalysis{}},
		} {
			t.Run(r.ID()+"/"+name, func(t *testing.T) {
				rs, err := r.Evaluate(context.Background(), in)
				if err != nil || len(rs) == 0 {
					t.Fatalf("%v %v", rs, err)
				}
				for _, res := range rs {
					if err := res.Validate(); err != nil {
						t.Fatal(err)
					}
					if res.Status == sdk.StatusFail && in == nil {
						t.Fatal("a nil input cannot fail")
					}
				}
			})
		}
	}
	// azure.yaml missing is a skip, never a pass.
	for _, id := range []string{"FND-CFG-001", "FND-CFG-002", "FND-CFG-003", "FND-CFG-004", "FND-CFG-005", "FND-CFG-006", "FND-CFG-007", "FND-CFG-011"} {
		rs := eval(t, id, &model.Input{})
		wantSkip(t, rs, rules.ReasonNotApplicable)
		wantNoFindings(t, rs)
	}
}

func TestRedaction(t *testing.T) {
	secret := canary()
	rs := all(t, yamlInput(t, messy(secret), withEnv(map[string]string{"AZURE_LOCATION": "West US", "OTHER": secret})))
	if len(rs) == 0 {
		t.Fatal("no results")
	}
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), strings.TrimPrefix(secret, "sk-")) {
		t.Fatal("the canary secret appears in a result")
	}
	// Run through the engine's redactor too: it must find nothing left to mask in the free text.
	for _, r := range rs {
		if f := r.Finding; f != nil {
			for _, s := range []string{f.Evidence, f.Recommendation, f.Fix, f.Key} {
				if findings.Redact(s) != s {
					t.Errorf("%s: redactor changed %q -> %q", f.RuleID, s, findings.Redact(s))
				}
			}
		}
	}
}

func marshal(t *testing.T, rs []rules.Result) string {
	t.Helper()
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDeterminism(t *testing.T) {
	want := marshal(t, all(t, messyInput(t)))
	for i := 0; i < 5; i++ {
		if got := marshal(t, all(t, messyInput(t))); got != want {
			t.Fatal("results differ between runs")
		}
	}
	// Reverse and rotate every list the rules read.
	in := messyInput(t)
	slices.Reverse(in.AzureYAML.Services)
	slices.Reverse(in.YAMLReport.Interpolations)
	slices.Reverse(in.YAMLReport.Issues)
	slices.Reverse(in.YAMLReport.Duplicates)
	if got := marshal(t, all(t, in)); got != want {
		t.Fatalf("results depend on input order:\n%s\n%s", got, want)
	}
	in = messyInput(t)
	svcs := in.AzureYAML.Services
	in.AzureYAML.Services = append(slices.Clone(svcs[2:]), svcs[:2]...)
	if got := marshal(t, all(t, in)); got != want {
		t.Fatal("results depend on service rotation")
	}
}

func fingerprints(rs []rules.Result) []string {
	var out []string
	for _, r := range rs {
		if r.Finding != nil {
			out = append(out, findings.Fingerprint(*r.Finding))
		}
	}
	slices.Sort(out)
	return out
}

func TestFingerprintStableAcrossLineMoves(t *testing.T) {
	a := all(t, yamlInput(t, messy(canary()), withEnv(map[string]string{"AZURE_LOCATION": "West US"})))
	b := all(t, yamlInput(t, "# a comment\n\n\n"+messy(canary()), withEnv(map[string]string{"AZURE_LOCATION": "West US"})))
	fa, fb := fingerprints(a), fingerprints(b)
	if len(fa) == 0 || !slices.Equal(fa, fb) {
		t.Fatalf("fingerprints moved with the lines:\n%v\n%v", fa, fb)
	}
	lines := func(rs []rules.Result) (n int) {
		for _, r := range rs {
			if r.Finding != nil {
				n += r.Finding.Location.Line
			}
		}
		return n
	}
	if lines(a) == lines(b) {
		t.Fatal("test input did not move any line")
	}
	// Distinct findings have distinct fingerprints.
	if len(slices.Compact(slices.Clone(fa))) != len(fa) {
		t.Fatalf("fingerprint collision among %d findings", len(fa))
	}
}

func TestVersionsMatchCatalogue(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for _, m := range cat {
		byID[m.ID] = m.Version
	}
	for _, r := range cfg.Rules() {
		if r.Version() != byID[r.ID()] {
			t.Errorf("%s: version %d, catalogue %d", r.ID(), r.Version(), byID[r.ID()])
		}
	}
}

// stamper adapts the findings package to the executor.
type stamper struct{}

func (stamper) Fingerprint(f sdk.Finding) string { return findings.Fingerprint(f) }
func (stamper) Redact(s string) string           { return findings.Redact(s) }

func TestExecutorSmoke(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := rules.NewRegistry(cat, cfg.Rules(), rules.Phase1)
	if err != nil {
		t.Fatal(err)
	}
	var entries []rules.Entry
	for _, e := range reg.Executable() {
		if strings.HasPrefix(e.Meta.ID, "FND-CFG-") {
			entries = append(entries, e)
		}
	}
	if len(entries) != 9 {
		t.Fatalf("%d CFG rules executable", len(entries))
	}
	out, err := rules.Execute(context.Background(), messyInput(t), entries, rules.ExecConfig{Profile: rules.ProfileProd, Stamper: stamper{}})
	if err != nil || len(out.Errors) != 0 {
		t.Fatalf("%v %v", err, out.Errors)
	}
	for _, f := range out.Findings {
		if f.Severity == "" || f.Fingerprint == "" || f.Profile != rules.ProfileProd {
			t.Fatalf("not stamped: %+v", f)
		}
	}
	if len(out.Findings) == 0 || len(out.Skipped) == 0 {
		t.Fatalf("findings %d skipped %d", len(out.Findings), len(out.Skipped))
	}
}

func TestARMMetaHandling(t *testing.T) {
	const id = "FND-CFG-012"
	y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n"
	rs := eval(t, id, yamlInput(t, y, withARM(t, "rules/cfg/arm-location-badformat.json")))
	acct := wantFinding(t, rs, sdk.StatusFail, "format:Microsoft.CognitiveServices/accounts:acct")
	if acct.Location.Pointer == "" || acct.Resource.Kind != "arm-resource" {
		t.Fatalf("%+v", acct)
	}
	// A conditional resource may not deploy: uncertain, never a firm fail.
	wantFinding(t, rs, sdk.StatusUncertain, "format:Microsoft.Storage/storageAccounts:st")
	// A looped resource is deployed an unknown number of times, but its literal location is still wrong.
	wantFinding(t, rs, sdk.StatusFail, "format:Microsoft.Search/searchServices:")

	// Locations of resources that may not deploy do not count towards the AZURE_LOCATION comparison.
	in := yamlInput(t, y, withEnv(map[string]string{"AZURE_LOCATION": "eastus2"}), withARM(t, "rules/cfg/arm-location-westus3.json"))
	for i := range in.ARM.Resources {
		in.ARM.Resources[i].Meta.MayNotDeploy = true
	}
	for _, r := range withStatus(eval(t, id, in), sdk.StatusFail) {
		t.Fatalf("unexpected %s", r.Finding.Key)
	}
	// The same resources inside a module (uncertain confidence) are uncertain too.
	in = yamlInput(t, y, withARM(t, "rules/cfg/arm-location-badformat.json"))
	for i := range in.ARM.Resources {
		in.ARM.Resources[i].Meta.Confidence = "uncertain"
	}
	rs = eval(t, id, in)
	wantFinding(t, rs, sdk.StatusUncertain, "format:Microsoft.CognitiveServices/accounts:acct")
}

func TestUnresolvedARMValueIsNeverPassOrFail(t *testing.T) {
	y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n"
	rs := eval(t, "FND-CFG-012", yamlInput(t, y, withARM(t, "rules/cfg/arm-location-expression.json")))
	for _, r := range rs {
		if r.Status == sdk.StatusPass || r.Status == sdk.StatusFail {
			t.Fatalf("unresolved location produced %s", r.Status)
		}
	}
}

func TestUsesOfBicepResourceIsUncertain(t *testing.T) {
	y := "name: d\nservices:\n  a:\n    host: azure.ai.toolbox\n    uses: [some-bicep-resource]\n"
	rs := eval(t, "FND-CFG-003", yamlInput(t, y, withIaC(model.IaCBicep)))
	wantFinding(t, rs, sdk.StatusUncertain, "uses:a:some-bicep-resource")
	rs = eval(t, "FND-CFG-003", yamlInput(t, y, withARM(t, "rules/cfg/arm-location-westus3.json")))
	wantFinding(t, rs, sdk.StatusUncertain, "uses:a:some-bicep-resource")
}
