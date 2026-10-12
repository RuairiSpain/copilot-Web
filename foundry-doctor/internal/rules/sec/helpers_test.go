package sec_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// tcase is one catalogue test scenario; name must equal the catalogue text exactly.
type tcase struct {
	name string
	fn   func(t *testing.T)
}

func runCases(t *testing.T, cases []tcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, c.fn)
	}
}

// obj is a JSON object.
type obj = map[string]any

// res builds an ARM resource in array form.
func res(typ, name string, props obj, extra ...obj) obj {
	r := obj{"type": typ, "apiVersion": "2025-01-01", "name": name}
	if props != nil {
		r["properties"] = props
	}
	for _, e := range extra {
		for k, v := range e {
			r[k] = v
		}
	}
	return r
}

// build normalises an ARM document with the real normaliser and wraps it in an Input.
func build(t testing.TB, params obj, supplied obj, resources ...obj) *model.Input {
	t.Helper()
	doc := obj{"contentVersion": "1.0.0.0", "resources": resources}
	if params != nil {
		doc["parameters"] = params
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return normalise(t, raw, supplied)
}

func normalise(t testing.TB, raw []byte, supplied obj) *model.Input {
	t.Helper()
	n, err := bicep.Normalise(raw, bicep.Options{File: "main.bicep", ParameterValues: supplied})
	if err != nil {
		t.Fatal(err)
	}
	return &model.Input{Project: model.Project{IaC: model.IaCBicep}, ARM: n.Template, Profile: rules.ProfileProd}
}

func ruleByID(t testing.TB, id string) rules.Rule {
	t.Helper()
	for _, r := range sec.Rules() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("rule %s is not registered", id)
	return nil
}

func eval(t testing.TB, id string, in *model.Input) []rules.Result {
	t.Helper()
	out, err := ruleByID(t, id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range out {
		if err := r.Validate(); err != nil {
			t.Fatalf("result %d: %v", i, err)
		}
	}
	return out
}

// tally counts results by status, ignoring the by-design live skip.
type tally struct {
	pass, fail, unc, skip int
	findings              []sdk.Finding
	skips                 []sdk.SkippedCheck
}

func count(out []rules.Result) tally {
	var c tally
	for _, r := range out {
		switch r.Status {
		case sdk.StatusPass:
			c.pass++
		case sdk.StatusFail:
			c.fail++
			c.findings = append(c.findings, *r.Finding)
		case sdk.StatusUncertain:
			c.unc++
			c.findings = append(c.findings, *r.Finding)
		case sdk.StatusSkipped:
			if r.Skip.Reason == rules.ReasonControlPlaneUnavailable {
				continue
			}
			c.skip++
			c.skips = append(c.skips, *r.Skip)
		}
	}
	return c
}

func (c tally) want(t testing.TB, pass, fail, unc, skip int) {
	t.Helper()
	if c.pass != pass || c.fail != fail || c.unc != unc || c.skip != skip {
		t.Fatalf("pass/fail/uncertain/skip = %d/%d/%d/%d, want %d/%d/%d/%d\nfindings: %+v\nskips: %+v",
			c.pass, c.fail, c.unc, c.skip, pass, fail, unc, skip, c.findings, c.skips)
	}
}

func (c tally) skipReason(reason string) bool {
	for _, s := range c.skips {
		if s.Reason == reason || strings.HasPrefix(s.Reason, reason) {
			return true
		}
	}
	return false
}

// liveSkipped reports whether the by-design control-plane skip is present.
func liveSkipped(out []rules.Result) bool {
	for _, r := range out {
		if r.Status == sdk.StatusSkipped && r.Skip.Reason == rules.ReasonControlPlaneUnavailable &&
			r.Skip.MissingCapability != "" {
			return true
		}
	}
	return false
}

// Resource builders for the common Foundry shapes.

func account(name string, props obj, extra ...obj) obj {
	return res("Microsoft.CognitiveServices/accounts", name, props, extra...)
}

func project(acct, name string) obj {
	return res("Microsoft.CognitiveServices/accounts/projects", acct+"/"+name, obj{"displayName": name})
}

func connection(acct, name, category, resourceID string) obj {
	md := obj{}
	if resourceID != "" {
		md["ResourceId"] = resourceID
	}
	return res("Microsoft.CognitiveServices/accounts/connections", acct+"/"+name,
		obj{"category": category, "target": "https://example.invalid/endpoint", "authType": "AAD", "metadata": md})
}

func capHost(acct, name string, lists obj) obj {
	return res("Microsoft.CognitiveServices/accounts/capabilityHosts", acct+"/"+name, lists)
}

func rid(typ, name string) string {
	return "[resourceId('" + typ + "', '" + name + "')]"
}

func search(name string, props obj, extra ...obj) obj {
	return res("Microsoft.Search/searchServices", name, props, extra...)
}

func cosmos(name string, props obj, extra ...obj) obj {
	return res("Microsoft.DocumentDB/databaseAccounts", name, props, extra...)
}

func storage(name string, props obj, extra ...obj) obj {
	return res("Microsoft.Storage/storageAccounts", name, props, extra...)
}

// foundryWith is a Foundry account with a project and a connection to the target.
func foundryWith(connCategory, targetType, targetName string, extra ...obj) []obj {
	out := []obj{
		account("acct", obj{"disableLocalAuth": true}),
		project("acct", "proj"),
		connection("acct", "conn1", connCategory, rid(targetType, targetName)),
	}
	return append(out, extra...)
}

func inputOf(t testing.TB, resources []obj) *model.Input {
	t.Helper()
	return build(t, nil, nil, resources...)
}

// Redaction canary assembled at run time, so no secret-shaped literal sits in the repository.
func canary() string { return "sk-" + strings.Repeat("canary", 4) + "-" + "0123456789" }
