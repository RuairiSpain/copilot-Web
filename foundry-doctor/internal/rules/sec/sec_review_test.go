package sec

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Review M3: a rule with findings keeps its unevaluated resources visible.
func TestSEC006FindingsKeepUnevaluatedVisible(t *testing.T) {
	conn := func(target string) sdk.ARMResource {
		return res(typeAccounts+"/connections", "acct/c-"+target, props{"target": target})
	}
	in := &sdk.Input{ARM: model{
		conn("st"), conn("db"),
		res(typeStorage, "st", props{"networkAcls": props{"ipRules": []any{props{"value": "0.0.0.0/0"}}}}),
		res(typeCosmos, "db", props{"ipRules": "[parameters('rules')]"}),
	}}
	out := run(t, "FND-SEC-006", in)
	if out.Skipped != nil || len(out.Findings) != 1 {
		t.Fatalf("got %+v", out)
	}
	ev := out.Findings[0].Evidence
	if strings.Contains(ev, "not evaluated") {
		t.Errorf("evidence must not carry a note: %q", ev)
	}
	if len(out.Skips) != 1 || !strings.Contains(out.Skips[0].Reason, "db: ipRules is an unresolved expression") {
		t.Errorf("the unevaluated resource must be returned as a skip: %+v", out.Skips)
	}
}
