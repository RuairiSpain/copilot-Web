package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
)

var (
	kvReference = regexp.MustCompile(`^(?:@Microsoft\.KeyVault\(.+\)|keyvault:[A-Za-z0-9-]{1,127}|https://[A-Za-z0-9-]{3,24}\.vault\.azure\.net/secrets/[A-Za-z0-9-]{1,127}(?:/[0-9a-f]{32})?)$`)
	secretName  = regexp.MustCompile(`^[A-Za-z0-9-]{1,127}$`)
)

type rawSecret struct {
	re   *regexp.Regexp
	kind string
}

var rawSecrets = []rawSecret{
	{regexp.MustCompile(`(?i)(AccountKey|SharedAccessKey|SharedAccessSignature)\s*=`), "a storage or bus key"},
	{regexp.MustCompile(`(?i)[?&]sig=[A-Za-z0-9%+/=]{20,}`), "a SAS signature"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), "a private key"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), "a JWT"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`), "an API key"},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`), "a GitHub token"},
	{regexp.MustCompile(`(?i)\bpassword\s*=\s*\S+`), "a password"},
	{regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`), "credentials embedded in a URL"},
}

// IsKeyVaultReference reports whether value is an explicit Key Vault reference.
func IsKeyVaultReference(value string) bool { return kvReference.MatchString(value) }

// IsSecretNameOrReference accepts a bare secret name (resolved in the extension's Key
// Vault) or an explicit reference.
func IsSecretNameOrReference(value string) bool {
	return IsKeyVaultReference(value) || secretName.MatchString(value)
}

func rawSecretKind(value string) string {
	for _, r := range rawSecrets {
		if r.re.MatchString(value) {
			return r.kind
		}
	}
	return ""
}

// findRawSecrets scans the configuration (as a camelCase mapping) for raw secret material
// (rule 17).
func findRawSecrets(doc map[string]any) []diag.Diagnostic {
	var out []diag.Diagnostic
	scanSecrets(doc, nil, &out)
	return out
}

func pointerPath(parts []string) string {
	p := root
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			p += "[" + part + "]"
		} else {
			p += "." + part
		}
	}
	return p
}

func scanSecrets(node any, parts []string, out *[]diag.Diagnostic) {
	switch t := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := append(append([]string{}, parts...), k)
			scanSecrets(t[k], child, out)
		}
	case []any:
		for i, x := range t {
			scanSecrets(x, append(append([]string{}, parts...), fmt.Sprint(i)), out)
		}
	case string:
		if kind := rawSecretKind(t); kind != "" {
			*out = append(*out, diag.Err("XF017", pointerPath(parts),
				"value appears to contain %s; store it in Key Vault and reference it", kind))
		}
	}
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}
