package cfg

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func inputFor(t *testing.T, y string) *sdk.Input {
	t.Helper()
	return &sdk.Input{AzureYAML: doc(t, y)}
}

// FND-CFG-003 version 5: a uses entry that names neither a service nor a
// top-level resource is a finding.
func TestCFG003UsesUnknownName(t *testing.T) {
	const head = "name: app\n"
	runReviewCases(t, []reviewCase{
		{name: "uses-unknown-service-name-is-a-finding", id: "FND-CFG-003",
			yaml: head + "services:\n  proj:\n    host: azure.ai.project\n  a:\n    host: azure.ai.agent\n    uses: [ai-projct]\n",
			want: 1, contains: "ai-projct"},
		{name: "uses-known-service-passes", id: "FND-CFG-003",
			yaml: head + "services:\n  proj:\n    host: azure.ai.project\n  a:\n    host: azure.ai.agent\n    uses: [proj]\n"},
		{name: "uses-names-resources-entry", id: "FND-CFG-003",
			yaml: head + "resources:\n  db:\n    type: db.postgres\nservices:\n  a:\n    host: azure.ai.agent\n    uses: [db]\n"},
		{name: "uses-reference-is-not-judged", id: "FND-CFG-003",
			yaml: head + "services:\n  a:\n    host: azure.ai.agent\n    uses: ['${DEP}']\n"},
		{name: "uses-unknown-on-non-agent-service", id: "FND-CFG-003",
			yaml: head + "services:\n  c:\n    host: azure.ai.connection\n    uses: [nowhere]\n", want: 1, contains: "nowhere"},
		{name: "uses-unknown-reported-per-entry", id: "FND-CFG-003",
			yaml: head + "services:\n  a:\n    host: azure.ai.agent\n    uses: [x1, x2]\n", want: 2},
		{name: "uses-absent-passes", id: "FND-CFG-003",
			yaml: head + "services:\n  a:\n    host: azure.ai.agent\n"},
		{name: "uses-with-resources-not-a-mapping-is-uncertain", id: "FND-CFG-003",
			yaml: head + "resources: ${RES}\nservices:\n  a:\n    host: azure.ai.agent\n    uses: [x1]\n", skip: "the resources section is not a literal mapping"},
	})
}

func TestCFG003UsesUnknownKeyStableAndDistinct(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    uses: [x1, x2]\n"
	res := run(t, find(Register(), "FND-CFG-003"), inputFor(t, y))
	if len(res.Findings) != 2 || res.Findings[0].Key == res.Findings[1].Key {
		t.Fatalf("keys not distinct: %+v", res.Findings)
	}
	if res.Findings[0].Key != "uses-unknown:x1" {
		t.Fatalf("key = %q", res.Findings[0].Key)
	}
}
