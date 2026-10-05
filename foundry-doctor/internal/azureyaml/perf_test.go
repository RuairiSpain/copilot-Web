package azureyaml

import (
	"strings"
	"testing"
	"time"
)

// Hostile inputs at the default 1 MiB size cap must finish quickly.
func TestHostileInputsAreFast(t *testing.T) {
	const mib = 1 << 20
	inputs := map[string]string{
		"open brackets":    strings.Repeat("[", mib-1),
		"seq of seq":       strings.Repeat("- ", mib/2-1),
		"nested block seq": strings.Repeat("- ", 20000) + "x",
		"flow mappings":    strings.Repeat("{a: ", 200000),
		"many short keys":  strings.Repeat("a: 1\n", mib/5),
		"many distinct keys": func() string {
			var sb strings.Builder
			for i := 0; sb.Len() < mib-20; i++ {
				sb.WriteString("k")
				sb.WriteString(strings.Repeat("x", i%50))
				sb.WriteString(": 1\n")
			}
			return sb.String()
		}(),
		"dollars":         "v: '" + strings.Repeat("$", mib-20) + "'\n",
		"open refs":       "v: '" + strings.Repeat("${", mib/2-20) + "'\n",
		"nested defaults": "v: '" + strings.Repeat("${A:-", 100000) + "'\n",
		"many aliases":    "a: &x 1\nb: [" + strings.Repeat("*x,", mib/3-20) + "]\n",
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, _ = Parse([]byte(src), Options{})
			if d := time.Since(start); d > 10*time.Second {
				t.Errorf("took %v", d)
			}
		})
	}
}
