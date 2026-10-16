package armmodel

import (
	"reflect"
	"testing"
)

func TestSecretFacts(t *testing.T) {
	const tpl = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
"contentVersion":"1.0.0.0",
"parameters":{"pw":{"type":"securestring"},"cfg":{"type":"secureObject"},"name":{"type":"string"}},
"variables":{"V":"[parameters('pw')]"},
"resources":[],
"outputs":{"a":{"type":"string","value":"[variables('V')]"},"b":{"type":"object","value":{"x":1}}}}`
	m, err := FromARM([]byte(tpl), Options{Entry: "main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.SecureParams(), []string{"cfg", "pw"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SecureParams = %v, want %v", got, want)
	}
	outs := m.OutputDetails()
	if len(outs) != 2 || outs[0].Name != "a" || outs[0].Value != "[variables('V')]" || outs[1].Value != `{"x":1}` {
		t.Errorf("OutputDetails = %+v", outs)
	}
	if v, ok := m.VariableText("v"); !ok || v != `"[parameters('pw')]"` {
		t.Errorf("VariableText = %q, %v", v, ok)
	}
	if _, ok := m.VariableText("missing"); ok {
		t.Error("missing variable reported present")
	}
	var nilModel *Model
	if nilModel.OutputDetails() != nil || nilModel.SecureParams() != nil {
		t.Error("nil model must return nil")
	}
}
