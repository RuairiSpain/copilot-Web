package azureyaml

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

const fixtureDir = "../../test/fixtures/azureyaml"

func mustParse(t *testing.T, src string) *Result {
	t.Helper()
	r, err := Parse([]byte(src), Options{File: "azure.yaml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return r
}

func fixture(t *testing.T, name string) *Result {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(b, Options{File: "azure.yaml"})
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return r
}

func errorsOf(r *Result) []Issue {
	var out []Issue
	for _, i := range r.Issues {
		if i.Level == LevelError {
			out = append(out, i)
		}
	}
	return out
}

func codePaths(is []Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, string(i.Code)+" "+i.Path)
	}
	slices.Sort(out)
	return out
}

func TestParseErrors(t *testing.T) {
	deepBlock := func(n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(strings.Repeat(" ", i) + "k:\n")
		}
		sb.WriteString(strings.Repeat(" ", n) + "leaf: 1\n")
		return sb.String()
	}
	tests := []struct {
		name    string
		src     string
		opt     Options
		is      error
		line    int // expected SyntaxError line, 0 to skip
		limit   string
		mention string
	}{
		{name: "empty", src: "", is: ErrEmpty},
		{name: "whitespace only", src: " \n\n", is: ErrEmpty},
		{name: "comment only", src: "# nothing here\n", is: ErrEmpty},
		{name: "bom only", src: "\xef\xbb\xbf", is: ErrEmpty},
		{name: "unclosed flow", src: "name: x\nservices: [a\n", is: ErrSyntax},
		{name: "tab indentation", src: "services:\n\tapi: 1\n", is: ErrSyntax, line: 2},
		{name: "bad mapping", src: "a: b: c\n", is: ErrSyntax},
		{name: "two documents", src: "name: a\n---\nname: b\n", is: ErrMultiDocument},
		{name: "trailing document marker", src: "name: a\n---\n", is: ErrMultiDocument},
		{name: "second document broken", src: "name: a\n---\n[\n", is: ErrSyntax},
		{name: "scalar root", src: "just text\n", is: ErrNotMapping},
		{name: "null root", src: "~\n", is: ErrNotMapping},
		{name: "list root", src: "- a\n- b\n", is: ErrNotMapping},
		{name: "too many bytes", src: "name: " + strings.Repeat("a", 200) + "\n", opt: Options{Limits: Limits{MaxBytes: 100}}, is: ErrLimit, limit: "bytes"},
		{name: "huge scalar", src: "name: " + strings.Repeat("a", 300) + "\n", opt: Options{Limits: Limits{MaxScalarBytes: 100}}, is: ErrLimit, limit: "scalar"},
		{name: "huge key", src: strings.Repeat("k", 300) + ": 1\n", opt: Options{Limits: Limits{MaxScalarBytes: 100}}, is: ErrLimit, limit: "scalar"},
		{name: "too many nodes", src: func() string {
			var sb strings.Builder
			for i := 0; i < 100; i++ {
				fmt.Fprintf(&sb, "k%d: v\n", i)
			}
			return sb.String()
		}(), opt: Options{Limits: Limits{MaxNodes: 50}}, is: ErrLimit, limit: "nodes"},
		{name: "deep block nesting", src: deepBlock(100), is: ErrLimit, limit: "depth"},
		{name: "deep flow nesting", src: "a: " + strings.Repeat("[", 200) + strings.Repeat("]", 200) + "\n", is: ErrLimit, limit: "depth"},
		{name: "absurd flow nesting", src: "a: " + strings.Repeat("[", 50000) + strings.Repeat("]", 50000) + "\n", opt: Options{Limits: Limits{MaxBytes: 1 << 20}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Parse([]byte(tc.src), tc.opt)
			if tc.is == nil {
				if err == nil && r == nil {
					t.Fatal("nil result without error")
				}
				return // only requires: no panic, no hang
			}
			if err == nil {
				t.Fatalf("want error %v, got result %+v", tc.is, r)
			}
			if r != nil {
				t.Errorf("result must be nil on error")
			}
			if !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, want Is %v", err, tc.is)
			}
			if tc.line > 0 {
				var se *SyntaxError
				if !errors.As(err, &se) || se.Line != tc.line || se.Pos().Line != tc.line {
					t.Errorf("syntax error = %v, want line %d", err, tc.line)
				}
			}
			if tc.limit != "" {
				var le *LimitError
				if !errors.As(err, &le) || le.Limit != tc.limit {
					t.Errorf("limit error = %v, want limit %q", err, tc.limit)
				}
			}
		})
	}
}

func TestErrorMessagesDoNotEchoContent(t *testing.T) {
	_, err := Parse([]byte("name: x\ncanary: sentinel-content-xyz\n  bad: [\n"), Options{})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "sentinel-content") {
		t.Errorf("error leaks document content: %v", err)
	}
	le := &LimitError{Limit: "nodes", Max: 5, Pos: model.Pos{Line: 3}}
	if !strings.Contains(le.Error(), "line 3") {
		t.Errorf("limit error without position: %v", le)
	}
}

func TestAliasBombIsCheap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("name: bomb\nlol0: &a0 [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&sb, "lol%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d,", i-1), 9), ","))
	}
	start := time.Now()
	r := mustParse(t, sb.String())
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("alias bomb took %v", d)
	}
	var aliases, anchors int
	for _, i := range r.Issues {
		switch i.Code {
		case CodeAlias:
			aliases++
		case CodeAnchor:
			anchors++
		}
	}
	if aliases != 81 || anchors != 10 {
		t.Errorf("aliases = %d (want 81), anchors = %d (want 10)", aliases, anchors)
	}
}

func TestAnchorsAliasesAndMergeKeysAreReported(t *testing.T) {
	r := fixture(t, "anchors-aliases-merge.azure.yaml")
	got := map[IssueCode][]model.Pos{}
	for _, i := range r.Issues {
		got[i.Code] = append(got[i.Code], i.Pos)
	}
	want := map[IssueCode][]model.Pos{
		CodeAnchor:   {{Line: 3, Column: 9}},
		CodeMergeKey: {{Line: 7, Column: 5}},
		CodeAlias:    {{Line: 7, Column: 9}, {Line: 9, Column: 9}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("yaml-level issues = %v\nwant %v", got, want)
	}
	// A mapping that merges keys cannot be judged for absent keys: no false missing-required.
	for _, i := range r.Issues {
		if i.Code == CodeMissingRequired && strings.HasPrefix(i.Path, "services.derived") {
			t.Errorf("unexpected %s for a service with a merge key", i.Message)
		}
	}
	if len(r.YAML.Services) != 3 || r.YAML.Services[0].Name != "base" {
		t.Errorf("services = %+v", r.YAML.Services)
	}
}

func TestAliasKeyAndComplexKey(t *testing.T) {
	r := mustParse(t, "name: x\nk: &k key\n*k : 1\n? [a, b]\n: 2\n")
	var codes []IssueCode
	for _, i := range r.Issues {
		codes = append(codes, i.Code)
	}
	for _, want := range []IssueCode{CodeAlias, CodeNonScalarKey} {
		if !slices.Contains(codes, want) {
			t.Errorf("missing %s in %v", want, codes)
		}
	}
}

func TestDuplicateKeys(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Duplicate
	}{
		{"none", "name: a\nservices:\n  a:\n    host: x\n", nil},
		{"top level", "name: a\nname: b\n", []Duplicate{{Path: "name", Key: "name", Positions: []model.Pos{{Line: 1, Column: 1}, {Line: 2, Column: 1}}}}},
		{"three times", "a: 1\nb: 2\na: 3\na: 4\n", []Duplicate{{Path: "a", Key: "a", Positions: []model.Pos{{Line: 1, Column: 1}, {Line: 3, Column: 1}, {Line: 4, Column: 1}}}}},
		{"nested", "name: a\nservices:\n  api:\n    host: x\n    host: y\n", []Duplicate{{Path: "services.api.host", Key: "host", Positions: []model.Pos{{Line: 4, Column: 5}, {Line: 5, Column: 5}}}}},
		{"inside list item", "items:\n  - k: 1\n    k: 2\n", []Duplicate{{Path: "items[0].k", Key: "k", Positions: []model.Pos{{Line: 2, Column: 5}, {Line: 3, Column: 5}}}}},
		{"same key in different mappings is fine", "a:\n  k: 1\nb:\n  k: 2\n", nil},
		{"quoted and plain are the same key", "a: 1\n\"a\": 2\n", []Duplicate{{Path: "a", Key: "a", Positions: []model.Pos{{Line: 1, Column: 1}, {Line: 2, Column: 1}}}}},
		{"two groups ordered by first position", "x:\n  q: 1\n  q: 2\nb: 1\nb: 2\n", []Duplicate{
			{Path: "x.q", Key: "q", Positions: []model.Pos{{Line: 2, Column: 3}, {Line: 3, Column: 3}}},
			{Path: "b", Key: "b", Positions: []model.Pos{{Line: 4, Column: 1}, {Line: 5, Column: 1}}},
		}},
		{"key with a dot is quoted in the path", "\"a.b\": 1\n\"a.b\": 2\n", []Duplicate{{Path: `["a.b"]`, Key: "a.b", Positions: []model.Pos{{Line: 1, Column: 1}, {Line: 2, Column: 1}}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := mustParse(t, tc.src)
			if !reflect.DeepEqual(r.Duplicates, tc.want) {
				t.Errorf("duplicates = %+v\nwant %+v", r.Duplicates, tc.want)
			}
		})
	}
}

func TestDuplicateServiceKeepsBothInRootAndFirstInTyped(t *testing.T) {
	r := fixture(t, "duplicate-service-key.azure.yaml")
	if len(r.YAML.Services) != 1 || r.YAML.Services[0].Project != "src/api" {
		t.Fatalf("typed services = %+v, want only the first api", r.YAML.Services)
	}
	var n int
	for _, c := range r.YAML.Root.Lookup("services").Children {
		if c.Key == "api" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("root keeps %d api entries, want 2", n)
	}
	if len(r.Duplicates) != 1 || r.Duplicates[0].Path != "services.api" {
		t.Errorf("duplicates = %+v", r.Duplicates)
	}
}

func TestPositions(t *testing.T) {
	doc := "name: demo\nservices:\n  api:\n    host: containerapp\n    uses:\n      - db\n      - \"cache\"\nresources:\n  db:\n    type: db.postgres\n"
	variants := map[string]string{
		"lf":   doc,
		"crlf": strings.ReplaceAll(doc, "\n", "\r\n"),
		"bom":  "\xef\xbb\xbf" + doc,
	}
	for vn, src := range variants {
		t.Run(vn, func(t *testing.T) {
			r := mustParse(t, src)
			root := r.YAML.Root
			checks := []struct {
				what string
				got  model.Pos
				want model.Pos
			}{
				{"name key", root.Lookup("name").KeyPos, model.Pos{Line: 1, Column: 1}},
				{"name value", root.Lookup("name").Pos, model.Pos{Line: 1, Column: 7}},
				{"services key", root.Lookup("services").KeyPos, model.Pos{Line: 2, Column: 1}},
				{"api key", root.Lookup("services", "api").KeyPos, model.Pos{Line: 3, Column: 3}},
				{"host key", root.Lookup("services", "api", "host").KeyPos, model.Pos{Line: 4, Column: 5}},
				{"host value", root.Lookup("services", "api", "host").Pos, model.Pos{Line: 4, Column: 11}},
				{"service pos", r.YAML.Services[0].Pos, model.Pos{Line: 3, Column: 3}},
				{"uses[0]", r.YAML.Services[0].Uses[0].Pos, model.Pos{Line: 6, Column: 9}},
				{"uses[1]", r.YAML.Services[0].Uses[1].Pos, model.Pos{Line: 7, Column: 9}},
				{"resource key", r.Resources[0].Pos, model.Pos{Line: 9, Column: 3}},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %v, want %v", c.what, c.got, c.want)
				}
			}
			if r.YAML.Services[0].Uses[1].Name != "cache" {
				t.Errorf("quoted uses entry = %q", r.YAML.Services[0].Uses[1].Name)
			}
		})
	}
}

func TestUnicodeKeysAndColumns(t *testing.T) {
	r := mustParse(t, "name: x\nключ: значение\n\"é\": v\n")
	k := r.YAML.Root.Lookup("ключ")
	if k == nil || k.Value != "значение" {
		t.Fatalf("unicode key not found: %+v", r.YAML.Root.Children)
	}
	if k.KeyPos != (model.Pos{Line: 2, Column: 1}) || k.Pos != (model.Pos{Line: 2, Column: 7}) {
		t.Errorf("unicode key positions = %v / %v (columns count characters)", k.KeyPos, k.Pos)
	}
	if r.YAML.Root.Lookup("é") == nil {
		t.Error("quoted non-ASCII key lost")
	}
	if len(r.UnknownTopLevel) != 2 {
		t.Errorf("unknown top-level = %v", r.UnknownTopLevel)
	}
}

func TestEmptyAndOddServices(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		services int
		want     []string // code path of error issues
	}{
		{"null services", "name: app\nservices:\n", 0, []string{"min-properties services"}},
		{"empty map services", "name: app\nservices: {}\n", 0, []string{"min-properties services"}},
		{"services as list", "name: app\nservices:\n  - api\n", 0, []string{"invalid-type services"}},
		{"services as scalar", "name: app\nservices: nope\n", 0, []string{"invalid-type services"}},
		{"service is null", "name: app\nservices:\n  api:\n", 1, []string{"missing-required services.api"}},
		{"service is a list", "name: app\nservices:\n  api:\n    - x\n", 1, []string{"invalid-type services.api"}},
		{"no services key", "name: app\n", 0, nil},
		{"missing name", "services:\n  a:\n    host: x\n", 1, []string{"missing-required "}},
		{"null name", "name:\nservices:\n  a:\n    host: x\n", 1, []string{"missing-required name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := mustParse(t, tc.src)
			if len(r.YAML.Services) != tc.services {
				t.Errorf("services = %d, want %d", len(r.YAML.Services), tc.services)
			}
			got := codePaths(errorsOf(r))
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("errors = %v, want %v", got, want)
			}
		})
	}
}

func TestTypedProjection(t *testing.T) {
	src := `name: demo
infra:
  provider: microsoft.foundry
  path: infra
  module: main
hooks:
  postprovision:
    shell: sh
    run: ./scripts/post.sh
  predeploy:
    - shell: pwsh
      run: echo one
    - run: echo two
services:
  agent:
    host: azure.ai.agent
    project: ./src/agent
    language: python
    uses: [proj, db]
    hooks:
      postdeploy:
        run: ./svc.sh
  proj:
    host: azure.ai.project
resources:
  db:
    type: db.postgres
azure.ai.custom:
  anything: 1
`
	r := mustParse(t, src)
	a := r.YAML
	if a.File != "azure.yaml" || a.Name != "demo" {
		t.Errorf("file/name = %q/%q", a.File, a.Name)
	}
	if a.Infra.Provider != "microsoft.foundry" || a.Infra.Path != "infra" || a.Infra.Module != "main" || a.Infra.Pos != (model.Pos{Line: 2, Column: 1}) {
		t.Errorf("infra = %+v", a.Infra)
	}
	wantHooks := []model.Hook{
		{Event: "postprovision", Shell: "sh", Run: "./scripts/post.sh", Pos: model.Pos{Line: 7, Column: 3}},
		{Event: "predeploy", Shell: "pwsh", Run: "echo one", Pos: model.Pos{Line: 11, Column: 7}},
		{Event: "predeploy", Run: "echo two", Pos: model.Pos{Line: 13, Column: 7}},
	}
	if !reflect.DeepEqual(a.Hooks, wantHooks) {
		t.Errorf("hooks = %+v\nwant %+v", a.Hooks, wantHooks)
	}
	if len(a.Services) != 2 {
		t.Fatalf("services = %+v", a.Services)
	}
	s, ok := a.Service("agent")
	if !ok || s.Host != "azure.ai.agent" || s.Project != "./src/agent" || len(s.Uses) != 2 || s.Uses[1].Name != "db" {
		t.Errorf("agent = %+v", s)
	}
	if len(s.Hooks) != 1 || s.Hooks[0].Event != "postdeploy" || s.Hooks[0].Run != "./svc.sh" {
		t.Errorf("service hooks = %+v", s.Hooks)
	}
	if got := scalarOf(s.Node, "language"); got != "python" {
		t.Errorf("language via node = %q", got)
	}
	if len(r.Resources) != 1 || r.Resources[0].Name != "db" {
		t.Errorf("resources = %+v", r.Resources)
	}
	var ext []string
	for _, e := range r.Extensions {
		ext = append(ext, e.Service+"|"+e.Host+"|"+e.Path)
		if e.Node == nil || e.Pos.Line == 0 {
			t.Errorf("extension %q lacks node or position", e.Path)
		}
	}
	wantExt := []string{"agent|azure.ai.agent|services.agent", "proj|azure.ai.project|services.proj", `||["azure.ai.custom"]`}
	if !slices.Equal(ext, wantExt) {
		t.Errorf("extensions = %v, want %v", ext, wantExt)
	}
	if _, ok := a.Service("nope"); ok {
		t.Error("Service(nope) found")
	}
}

func TestUndefinedUsesAndUnresolvedRefs(t *testing.T) {
	r := mustParse(t, `name: a
resources:
  db:
    type: db.postgres
services:
  one:
    host: containerapp
    project: src/one
    uses: [two, db, ghost, "${DYNAMIC}"]
    env:
      A: ${HAVE}
      B: ${MISSING}
      C: ${MISSING_DEFAULTED:-x}
      D: ${MUST:?needs a value}
      E: ${ASSIGNED:=x}
      F: $${ESCAPED}
      G: ${{project.endpoint}}
  two:
    host: containerapp
    project: src/two
`)
	uu := r.UndefinedUses()
	if len(uu) != 1 || uu[0].Name != "ghost" || uu[0].Service != "one" || uu[0].Index != 2 || uu[0].Pos != (model.Pos{Line: 9, Column: 21}) {
		t.Errorf("undefined uses = %+v", uu)
	}
	has := func(n string) bool { return n == "HAVE" }
	var names []string
	for _, i := range r.UnresolvedRefs(has) {
		names = append(names, i.Name)
	}
	if want := []string{"DYNAMIC", "MISSING", "MUST"}; !slices.Equal(names, want) {
		t.Errorf("unresolved = %v, want %v", names, want)
	}
	var nilRes *Result
	if nilRes.UndefinedUses() != nil || nilRes.UnresolvedRefs(has) != nil {
		t.Error("nil result should yield nothing")
	}
	if (&Result{}).UndefinedUses() != nil {
		t.Error("result without YAML should yield nothing")
	}
}

func TestInterpolationForms(t *testing.T) {
	r := fixture(t, "interpolation-forms.azure.yaml")
	type row struct {
		path string
		form Form
		name string
		op   string
		nest bool
	}
	var got []row
	for _, i := range r.Interpolations {
		got = append(got, row{strings.TrimPrefix(i.Path, "services.api.env."), i.Form, i.Name, i.Operator, i.Nested})
	}
	want := []row{
		{"PLAIN", FormEnv, "PLAIN_VAR", "", false},
		{"DEFAULTED", FormEnvDefault, "DEFAULTED_VAR", ":-", false},
		{"DASH", FormEnvDefault, "DASH_VAR", "-", false},
		{"ASSIGN", FormEnvOperator, "ASSIGN_VAR", ":=", false},
		{"REQUIRED", FormEnvOperator, "REQUIRED_VAR", ":?", false},
		{"ESCAPED", FormEscapedEnv, "ESCAPED_VAR", "", false},
		{"FOUNDRY", FormFoundry, "", "", false},
		{"ESCAPED_FOUNDRY", FormEscapedFoundry, "", "", false},
		{"TWO", FormEnv, "A_VAR", "", false},
		{"TWO", FormEnv, "B_VAR", "", false},
		{"NESTED", FormEnvDefault, "OUTER", ":-", true},
		{"BROKEN", FormMalformed, "", "", false},
		{"NAMELESS", FormMalformed, "", "", false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("interpolations:\n got %+v\nwant %+v", got, want)
	}
}

func TestScanEdgeCases(t *testing.T) {
	tests := []struct {
		val  string
		want []Form
	}{
		{"plain text", nil},
		{"cost is $5", nil},
		{"$", nil},
		{"trailing $$", nil},
		{"$NAME", nil},
		{"${A}", []Form{FormEnv}},
		{"$$${A}", []Form{FormEnv}},         // escaped $, then a live reference
		{"$$$${A}", []Form{FormEscapedEnv}}, // two escapes
		{"${A:-b}", []Form{FormEnvDefault}},
		{"${A:-}", []Form{FormEnvDefault}},
		{"${A#pre}", []Form{FormEnvOperator}},
		{"${A##pre}", []Form{FormEnvOperator}},
		{"${A%suf}", []Form{FormEnvOperator}},
		{"${A/x/y}", []Form{FormEnvOperator}},
		{"${A:+b}", []Form{FormEnvOperator}},
		{"${A^^}", []Form{FormEnvOperator}},
		{"${A,,}", []Form{FormEnvOperator}},
		{"${A:x}", []Form{FormMalformed}},
		{"${A", []Form{FormMalformed}},
		{"${A:-unterminated", []Form{FormMalformed}},
		{"${1BAD}", []Form{FormMalformed}},
		{"${{ a }", []Form{FormMalformed}},
		{"$${{ a }}", []Form{FormEscapedFoundry}},
		{"${{ a }} and ${B}", []Form{FormFoundry, FormEnv}},
		{"$${A:-${B}}", []Form{FormEscapedEnv, FormEnv}},
		{"$${1}", nil},
		{"${_a1}", []Form{FormEnv}},
	}
	for _, tc := range tests {
		t.Run(tc.val, func(t *testing.T) {
			src := "name: x\nv: '" + strings.ReplaceAll(tc.val, "'", "''") + "'\n"
			r := mustParse(t, src)
			var got []Form
			for _, i := range r.Interpolations {
				got = append(got, i.Form)
				if i.Path != "v" || i.Pos != (model.Pos{Line: 2, Column: 4}) {
					t.Errorf("path/pos = %q %v", i.Path, i.Pos)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("forms = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInterpolationNeverStoresValues(t *testing.T) {
	r := mustParse(t, "name: x\nk: ${A:-sentinel-one}\nj: ${B:?sentinel-two}\n")
	for _, i := range r.Interpolations {
		blob := fmt.Sprintf("%+v", i)
		if strings.Contains(blob, "sentinel") {
			t.Errorf("interpolation stores operand text: %s", blob)
		}
	}
}

func TestDeterministic(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixtureDir, "full-example-from-reference.azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Parse(b, Options{File: "azure.yaml"})
	for range 3 {
		c, _ := Parse(b, Options{File: "azure.yaml"})
		if !reflect.DeepEqual(a, c) {
			t.Fatal("Parse is not deterministic")
		}
	}
}

func TestConcurrentParse(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixtureDir, "full-example-from-reference.azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := Parse(b, Options{}); err != nil {
				t.Error(err)
			}
		}()
	}
	for range 8 {
		<-done
	}
}

func TestLimitDefaults(t *testing.T) {
	d := DefaultLimits()
	z := Limits{}.withDefaults()
	if d != z {
		t.Errorf("zero Limits should equal defaults: %+v vs %+v", z, d)
	}
	custom := Limits{MaxBytes: 1, MaxDepth: 2, MaxNodes: 3, MaxScalarBytes: 4}.withDefaults()
	if custom != (Limits{MaxBytes: 1, MaxDepth: 2, MaxNodes: 3, MaxScalarBytes: 4}) {
		t.Errorf("custom limits overwritten: %+v", custom)
	}
}
