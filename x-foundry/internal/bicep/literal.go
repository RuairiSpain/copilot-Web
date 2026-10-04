package bicep

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// propName renders an object property name, quoting it only when Bicep requires.
func propName(k string) string {
	if identifier.MatchString(k) {
		return k
	}
	return str(k)
}

// storageSuffix is how Bicep spells the cloud's storage DNS suffix; Azure's private DNS zone
// names for Storage embed it.
const storageSuffix = "core.windows.net"

// zoneName renders a private DNS zone name, using environment() for the storage suffix.
func zoneName(zone string) string {
	if strings.HasSuffix(zone, "."+storageSuffix) {
		quoted := str(strings.TrimSuffix(zone, storageSuffix))
		return quoted[:len(quoted)-1] + "${environment().suffixes.storage}'"
	}
	return str(zone)
}

func zoneNames(zones []string) string {
	if len(zones) == 0 {
		return "[]"
	}
	parts := make([]string, len(zones))
	for i, z := range zones {
		parts[i] = zoneName(z)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// str renders a Bicep string literal.
func str(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "\t", `\t`, "${", `\${`)
	return "'" + r.Replace(s) + "'"
}

// strs renders an array of string literals on one line.
func strs(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = str(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tagsObject renders a tags object with sorted, quoted keys. extra keys override.
func tagsObject(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", propName(k), str(tags[k]))
	}
	b.WriteString("}")
	return b.String()
}

// object renders ordered key/value pairs as a multi-line Bicep object; values are expressions.
func object(indent string, pairs [][2]string) string {
	var b strings.Builder
	b.WriteString("{\n")
	for _, kv := range pairs {
		fmt.Fprintf(&b, "%s  %s: %s\n", indent, kv[0], kv[1])
	}
	b.WriteString(indent + "}")
	return b.String()
}

func boolean(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// symbol turns a node id into a Bicep identifier; used keeps them unique.
func symbol(id string, used map[string]bool) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "n_" + name
	}
	candidate := name
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", name, i)
	}
	used[candidate] = true
	return candidate
}
