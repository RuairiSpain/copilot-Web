package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const testdataRulestest = "../rules/rulestest/testdata/"

// scriptRunner is a bicep.Runner that serves canned compiler output, so the
// loader is tested without a Bicep CLI. Keys are project-relative paths.
type scriptRunner struct {
	root    string
	builds  map[string][]byte // bicep build
	params  map[string]string // bicep build-params stdout
	paramEx map[string]int    // build-params exit codes
	calls   []string
}

func (s *scriptRunner) Run(_ context.Context, _ string, args []string, dir string) (bicep.RunResult, error) {
	if args[0] == "--version" {
		return bicep.RunResult{Stdout: []byte("Bicep CLI version 0.48.1 (test)")}, nil
	}
	rel, err := filepath.Rel(s.root, filepath.Join(dir, args[1]))
	if err != nil {
		return bicep.RunResult{}, err
	}
	rel = filepath.ToSlash(rel)
	s.calls = append(s.calls, args[0]+" "+rel)
	switch args[0] {
	case "build":
		if arm, ok := s.builds[rel]; ok {
			return bicep.RunResult{Stdout: arm}, nil
		}
		return bicep.RunResult{ExitCode: 1, Stderr: []byte("error: no such file")}, nil
	case "build-params":
		if code := s.paramEx[rel]; code != 0 {
			return bicep.RunResult{ExitCode: code}, nil
		}
		return bicep.RunResult{Stdout: []byte(s.params[rel])}, nil
	}
	return bicep.RunResult{ExitCode: 2}, nil
}

func (s *scriptRunner) called(prefix string) bool {
	for _, c := range s.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func readTestdata(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdataRulestest, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

type loaded struct {
	model sdk.ARMModel
	err   error
	log   string
	run   *scriptRunner
}

// loadProject loads dir with the scripted runner. env is the selected azd
// environment ("" for none).
func loadProject(t *testing.T, dir, env string, run *scriptRunner) loaded {
	t.Helper()
	run.root = dir
	fakeExe := filepath.Join(t.TempDir(), "bicep")
	if err := os.WriteFile(fakeExe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	l := armLoader{disc: bicep.DiscoverOptions{Path: fakeExe, Runner: run}, runner: run, stderr: &log}
	m, err := l.Load(context.Background(), Source{
		Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml",
		InfraPath: "infra", Environment: env,
	})
	return loaded{model: m, err: err, log: log.String(), run: run}
}

func resource(t *testing.T, m sdk.ARMModel, typ, name string) sdk.ARMResource {
	t.Helper()
	var have []string
	for _, r := range m.Resources() {
		if strings.EqualFold(r.Type, typ) && strings.EqualFold(r.Name, name) {
			return r
		}
		have = append(have, r.Type+" "+r.Name)
	}
	t.Fatalf("no %s %q in %v", typ, name, have)
	return sdk.ARMResource{}
}

const (
	tSearch = "Microsoft.Search/searchServices"
	tAI     = "Microsoft.CognitiveServices/accounts"
)

func paramsProject(t *testing.T) (string, *scriptRunner) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "// compiled by the script runner\n")
	return dir, &scriptRunner{
		builds: map[string][]byte{"infra/main.bicep": readTestdata(t, "params/main.json")},
		params: map[string]string{},
	}
}

func TestParameterFileResolvedFromSelectedEnvironment(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{
	  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#",
	  "contentVersion": "1.0.0.0",
	  "parameters": {
	    "prefix": {"value": "${AZURE_ENV_NAME}"},
	    "location": {"value": "${AZURE_LOCATION}"},
	    "deploySearch": {"value": true},
	    "publicNetworkAccess": {"value": "${PNA=Disabled}"}
	  }
	}`)
	writeFile(t, dir, ".azure/dev/.env", "AZURE_ENV_NAME=\"dev\"\nAZURE_LOCATION=\"swedencentral\"\n")
	got := loadProject(t, dir, "dev", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	ai := resource(t, got.model, tAI, "dev-ai")
	if ai.Region != "swedencentral" {
		t.Errorf("${AZURE_LOCATION} should resolve from the environment, region %q", ai.Region)
	}
	if ai.Properties["publicNetworkAccess"] != "Disabled" {
		t.Errorf("${PNA=Disabled}: %v", ai.Properties["publicNetworkAccess"])
	}
	srch := resource(t, got.model, tSearch, "[variables('searchName')]")
	if armexpr.PresenceOf(srch.Conditional, srch.Condition, srch.Looped) != armexpr.Deployed {
		t.Errorf("deploySearch=true overrides the false default: condition %q", srch.Condition)
	}
	if got.log != "" {
		t.Errorf("a fully resolved file must not warn: %q", got.log)
	}
	if len(got.model.(rawModel).FromDefault(ai)) != 0 {
		t.Error("values come from the file, not from defaults")
	}
}

func TestParameterFileUnresolvedWithoutEnvironment(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{
	  "prefix": {"value": "demo"},
	  "location": {"value": "${AZURE_LOCATION}"},
	  "publicNetworkAccess": {"value": "${PNA=Disabled}"}
	}}`)
	got := loadProject(t, dir, "", run) // no environment selected
	if got.err != nil {
		t.Fatal(got.err)
	}
	ai := resource(t, got.model, tAI, "demo-ai")
	if !armexpr.IsExpression(ai.Region) {
		t.Errorf("unresolved ${AZURE_LOCATION} must stay unresolved, region %q", ai.Region)
	}
	// Without a readable environment even a ${VAR=default} is not a fact, and
	// the Bicep default of an unresolved value is not used either.
	if v := ai.Properties["publicNetworkAccess"]; v != "[parameters('publicNetworkAccess')]" {
		t.Errorf("publicNetworkAccess %v", v)
	}
	if !strings.Contains(got.log, "location, publicNetworkAccess") || !strings.Contains(got.log, "could not be resolved") {
		t.Errorf("the unresolved parameters must be named: %q", got.log)
	}
	// deploySearch is not set: the literal default false applies, as a default.
	srch := resource(t, got.model, tSearch, "[variables('searchName')]")
	if armexpr.PresenceOf(srch.Conditional, srch.Condition, srch.Looped) != armexpr.NotDeployed {
		t.Errorf("default false: %q", srch.Condition)
	}
	if d := got.model.(rawModel).FromDefault(srch); strings.Join(d, ",") != "capacity,deploySearch" {
		t.Errorf("provenance %v, want [capacity deploySearch]", d)
	}
}

func TestParameterFileNeverSubstitutesSecretKeys(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{
	  "prefix": {"value": "${AZURE_API_KEY}"}
	}}`)
	writeFile(t, dir, ".azure/dev/.env", "AZURE_API_"+"KEY"+"="+"fake-test-value\n")
	got := loadProject(t, dir, "dev", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, r := range got.model.Resources() {
		if strings.Contains(r.Name, "fake-test-value") || strings.Contains(r.Name, "sha256") {
			t.Errorf("secret-looking environment value reached the model: %q", r.Name)
		}
	}
	if !armexpr.IsExpression(got.model.Resources()[0].Name) {
		t.Errorf("name should stay unresolved: %q", got.model.Resources()[0].Name)
	}
}

func TestMalformedAndOversizeParameterFilesAreNamedSkips(t *testing.T) {
	cases := map[string]struct {
		body, reason string
	}{
		"malformed": {`{"parameters": {`, bicep.ParamMalformed},
		"array":     {`[1]`, bicep.ParamMalformed},
		"no params": {`{}`, bicep.ParamMalformed},
		"oversize":  {`{"parameters":{"a":{"value":"` + strings.Repeat("x", bicep.MaxParameterFileBytes) + `"}}}`, bicep.ParamTooLarge},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, run := paramsProject(t)
			writeFile(t, dir, "infra/main.parameters.json", c.body)
			got := loadProject(t, dir, "", run)
			if got.err != nil {
				t.Fatalf("a bad parameter file must not fail the load: %v", got.err)
			}
			if !strings.Contains(got.log, "not used ("+c.reason+")") || !strings.Contains(got.log, "infra/main.parameters.json") {
				t.Errorf("want a named skip %q, log %q", c.reason, got.log)
			}
			ai := resource(t, got.model, tAI, "[format('{0}-ai', parameters('prefix'))]")
			if ai.Properties["publicNetworkAccess"] != "[parameters('publicNetworkAccess')]" {
				t.Errorf("no parameter source: defaults are not facts: %v", ai.Properties["publicNetworkAccess"])
			}
		})
	}
}

func TestBicepparamTakesPrecedenceOverParametersJSON(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"prefix":{"value":"fromjson"},"deploySearch":{"value":true}}}`)
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\nparam prefix = 'frombicepparam'\n")
	run.params["infra/main.bicepparam"] = `{"parametersJson":"{\"parameters\":{\"prefix\":{\"value\":\"frombicepparam\"},\"deploySearch\":{\"value\":false}}}"}`
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	resource(t, got.model, tAI, "frombicepparam-ai")
	srch := resource(t, got.model, tSearch, "[variables('searchName')]")
	if armexpr.PresenceOf(srch.Conditional, srch.Condition, srch.Looped) != armexpr.NotDeployed {
		t.Errorf("deploySearch=false from the bicepparam must win: %q", srch.Condition)
	}
	if !run.called("build-params infra/main.bicepparam") {
		t.Errorf("calls %v", run.calls)
	}
}

func TestBicepparamFailureIsNamedAndDoesNotFallBack(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"prefix":{"value":"fromjson"}}}`)
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\n")
	run.paramEx = map[string]int{"infra/main.bicepparam": 1}
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !strings.Contains(got.log, "infra/main.bicepparam not used ("+bicep.ParamBicepparamFailed+")") {
		t.Errorf("log %q", got.log)
	}
	if r := got.model.Resources()[0]; !armexpr.IsExpression(r.Name) {
		t.Errorf("azd would use the bicepparam, so the JSON file must not be used: %q", r.Name)
	}
}

func TestBicepparamReadingEnvironmentNeedsEnvironment(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\nparam prefix = readEnvironmentVariable('AZURE_ENV_NAME', 'dflt')\n")
	run.params["infra/main.bicepparam"] = `{"parametersJson":"{\"parameters\":{\"prefix\":{\"value\":\"dflt\"}}}"}`
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !strings.Contains(got.log, "not used ("+bicep.ParamNeedsEnv+")") {
		t.Errorf("log %q", got.log)
	}
	if run.called("build-params") {
		t.Error("build-params must not run when its answer would be the default of an unknown environment")
	}
	// With an environment the file is compiled.
	writeFile(t, dir, ".azure/dev/.env", "AZURE_ENV_NAME=dev\n")
	run2 := &scriptRunner{builds: run.builds, params: run.params}
	got = loadProject(t, dir, "dev", run2)
	if got.err != nil || !run2.called("build-params") {
		t.Errorf("err %v calls %v", got.err, run2.calls)
	}
}

func TestBicepEnvExcludesSecretsAndBadNames(t *testing.T) {
	_, known := selectedEnv(nil, Source{})
	if known {
		t.Error("no environment")
	}
	got := bicepEnv(map[string]string{"AZURE_LOCATION": "x", "bad name": "y", "NUL": "a\x00b", "A": "1"})
	if strings.Join(got, ",") != "A=1,AZURE_LOCATION=x" {
		t.Errorf("env %v", got)
	}
}

// R5: a finding's location is the module file (twice scenario: one module
// file deployed twice, plus a second module file).
func TestModuleFilesLocatedByTemplateHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "// entry\n")
	writeFile(t, dir, "infra/modules/search.bicep", "// module\n")
	writeFile(t, dir, "infra/modules/connection.bicep", "// module\n")
	writeFile(t, dir, "infra/modules/unrelated.bicep", "// not referenced\n")
	run := &scriptRunner{builds: map[string][]byte{
		"infra/main.bicep":               readTestdata(t, "twice/main.json"),
		"infra/modules/search.bicep":     readTestdata(t, "twice/modules/search.json"),
		"infra/modules/connection.bicep": readTestdata(t, "twice/modules/connection.json"),
		"infra/modules/unrelated.bicep":  []byte(`{"metadata":{"_generator":{"templateHash":"1"}},"resources":[]}`),
	}}
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	files := map[string]string{}
	for _, r := range got.model.Resources() {
		files[r.Type] = r.Location.File
		switch r.Type {
		case tSearch:
			if r.Location.File != "infra/modules/search.bicep" {
				t.Errorf("search %s located at %q", r.Name, r.Location.File)
			}
		case tAI:
			if r.Location.File != "infra/main.bicep" {
				t.Errorf("entry resource located at %q", r.Location.File)
			}
		}
	}
	var conn string
	for _, r := range got.model.Resources() {
		if strings.Contains(strings.ToLower(r.Type), "connections") {
			conn = r.Location.File
		}
	}
	if conn != "infra/modules/connection.bicep" {
		t.Errorf("connection located at %q (types %v)", conn, files)
	}
	if got.log != "" {
		t.Errorf("unexpected notes: %q", got.log)
	}
}

func TestModuleLocationBoundsAndAmbiguity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "// entry\n")
	// Two byte-identical module files compile to the same hash: ambiguous.
	writeFile(t, dir, "infra/modules/a-search.bicep", "// module\n")
	writeFile(t, dir, "infra/modules/b-search.bicep", "// module\n")
	search := readTestdata(t, "twice/modules/search.json")
	run := &scriptRunner{builds: map[string][]byte{
		"infra/main.bicep":             readTestdata(t, "twice/main.json"),
		"infra/modules/a-search.bicep": search,
		"infra/modules/b-search.bicep": search,
	}}
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, r := range got.model.Resources() {
		if r.Type == tSearch && r.Location.File != "infra/main.bicep" {
			t.Errorf("an ambiguous module must keep the entry file, got %q", r.Location.File)
		}
	}
	if !strings.Contains(got.log, "match more than one identical file") {
		t.Errorf("log %q", got.log)
	}

	// More candidates than MaxModuleFiles: only the bound is compiled.
	dir2 := t.TempDir()
	writeFile(t, dir2, "azure.yaml", "name: demo\n")
	writeFile(t, dir2, "infra/main.bicep", "// entry\n")
	builds := map[string][]byte{"infra/main.bicep": readTestdata(t, "twice/main.json")}
	for i := 0; i < MaxModuleFiles+10; i++ {
		rel := fmt.Sprintf("infra/modules/m%03d.bicep", i)
		writeFile(t, dir2, rel, "// module\n")
		builds[rel] = []byte(`{"metadata":{"_generator":{"templateHash":"0"}},"resources":[]}`)
	}
	run2 := &scriptRunner{builds: builds}
	got = loadProject(t, dir2, "", run2)
	if got.err != nil {
		t.Fatal(got.err)
	}
	n := 0
	for _, c := range run2.calls {
		if strings.HasPrefix(c, "build ") && c != "build infra/main.bicep" {
			n++
		}
	}
	if n != MaxModuleFiles {
		t.Errorf("compiled %d module files, want the bound %d", n, MaxModuleFiles)
	}
	if !strings.Contains(got.log, "only the first 64 of 74") {
		t.Errorf("log %q", got.log)
	}
}

func TestModuleFileTooLargeOrBrokenIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "// entry\n")
	writeFile(t, dir, "infra/modules/search.bicep", strings.Repeat("// pad\n", maxModuleFileBytes/7+10))
	writeFile(t, dir, "infra/modules/broken.bicep", "// does not compile\n")
	run := &scriptRunner{builds: map[string][]byte{"infra/main.bicep": readTestdata(t, "twice/main.json")}}
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if run.called("build infra/modules/search.bicep") {
		t.Error("an oversize module file must not be compiled")
	}
	if !strings.Contains(got.log, "2 .bicep file(s) could not be compiled alone") {
		t.Errorf("log %q", got.log)
	}
}

func TestTruncatedModelIsWarnedByName(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"resources":[`)
	for i := 0; i < 20001; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-05-01","name":"s%d"}`, i)
	}
	b.WriteString(`]}`)
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.json", b.String())
	var log bytes.Buffer
	m, err := armLoader{stderr: &log}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.(rawModel).Truncated() {
		t.Fatal("expected a truncated model")
	}
	if !strings.Contains(log.String(), "model-truncated") {
		t.Errorf("log %q", log.String())
	}
}

func TestParametersJSONAlsoAppliesToARMJSONEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.json", string(readTestdata(t, "params/main.json")))
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"prefix":{"value":"jsonentry"},"deploySearch":{"value":true}}}`)
	m, err := armLoader{}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	resource(t, m, tAI, "jsonentry-ai")
	// A bicepparam beside an ARM JSON entry cannot be compiled: named skip.
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\n")
	var log bytes.Buffer
	if _, err := (armLoader{stderr: &log}).Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "not used ("+bicep.ParamBicepparamFailed+")") {
		t.Errorf("log %q", log.String())
	}
}
