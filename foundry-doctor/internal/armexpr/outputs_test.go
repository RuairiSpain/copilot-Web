package armexpr

import "testing"

func TestRewriteDeploymentOutputs(t *testing.T) {
	repl := func(dep, out string) (string, bool) {
		if dep == "s1" && out == "id" {
			return "resourceId('T/x', 'n')", true
		}
		if dep == "s1" && out == "lit" {
			return "'lit'", true
		}
		return "", false
	}
	read := "reference(resourceId('Microsoft.Resources/deployments', 's1'), '2025-04-01').outputs.id.value"
	cases := []struct{ name, in, want string }{
		{"plain", "[" + read + "]", "[resourceId('T/x', 'n')]"},
		{"embedded", "[format('{0}/x', " + read + ")]", "[format('{0}/x', resourceId('T/x', 'n'))]"},
		{"symbolic name", "[reference('s1').outputs.id.value]", "[resourceId('T/x', 'n')]"},
		{"literal result", "[reference('s1').outputs.lit.value]", "lit"},
		{"literal result with property", "[reference('s1').outputs.lit.value.p]", "[reference('s1').outputs.lit.value.p]"},
		{"unknown output", "[reference('s1').outputs.zz.value]", "[reference('s1').outputs.zz.value]"},
		{"unknown deployment", "[reference('s2').outputs.id.value]", "[reference('s2').outputs.id.value]"},
		{"wrong type", "[reference(resourceId('Microsoft.Storage/storageAccounts', 's1')).outputs.id.value]", "[reference(resourceId('Microsoft.Storage/storageAccounts', 's1')).outputs.id.value]"},
		{"inside a string", "['reference(\\'s1\\').outputs.id.value']", "['reference(\\'s1\\').outputs.id.value']"},
		{"not an expression", "reference('s1').outputs.id.value", "reference('s1').outputs.id.value"},
		{"missing .value", "[reference('s1').outputs.id]", "[reference('s1').outputs.id]"},
	}
	for _, tc := range cases {
		if got := RewriteDeploymentOutputs(tc.in, repl); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
