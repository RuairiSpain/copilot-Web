// Package rulestest loads golden ARM JSON compiled from a small Bicep project
// (main.bicep, compiled with Bicep CLI 0.48.1) into the rule input model, so
// rule tests run on real compiled expression shapes: format(), uniqueString(),
// resourceGroup().location, subscriptionResourceId(), conditional resources and
// reference(...).subnets[0].id. It is used by tests only.
package rulestest

import (
	_ "embed"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

//go:embed main.json
var mainJSON []byte

// Model returns the compiled golden template as an ARM model.
func Model(t testing.TB) sdk.ARMModel {
	t.Helper()
	m, err := armmodel.FromARM(mainJSON, armmodel.Options{Entry: "main.bicep"})
	if err != nil {
		t.Fatalf("load golden ARM: %v", err)
	}
	return m
}

// JSON returns a copy of the golden compiled ARM template.
func JSON() []byte { return append([]byte(nil), mainJSON...) }

// Without returns the model's resources minus those whose type and name text
// satisfy drop. It is for building variants of the golden template.
func Without(m sdk.ARMModel, drop func(sdk.ARMResource) bool) sdk.ARMModel {
	var out model
	for _, r := range m.Resources() {
		if !drop(r) {
			out = append(out, r)
		}
	}
	return out
}

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return m }
