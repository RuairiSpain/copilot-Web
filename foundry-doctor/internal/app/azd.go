package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// azdVersionTimeout bounds the `azd version` probe.
const azdVersionTimeout = 10 * time.Second

var semverRE = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?`)

// detectAzdVersion asks the installed azd for its version
// (`azd version --output json`, see docs/tool-compatibility.md). It returns ""
// when azd is absent, slow or answers in an unrecognised shape, in which case
// rules with an azd compatibility range skip as input-unavailable. The probe
// is local and offline and its output is never echoed.
func detectAzdVersion(ctx context.Context) string {
	exe, err := exec.LookPath("azd")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, azdVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version", "--output", "json")
	cmd.Env = append(os.Environ(), "AZURE_DEV_COLLECT_TELEMETRY=no", "NO_COLOR=1")
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, n: 64 << 10}
	if cmd.Run() != nil {
		return ""
	}
	return parseAzdVersion(out.Bytes())
}

// parseAzdVersion accepts {"azd":{"version":"x"}}, {"version":"x"} or plain
// text containing a semantic version. The first documented shape is
// unverified, hence the lenient parsing.
func parseAzdVersion(b []byte) string {
	var v struct {
		Version string `json:"version"`
		Azd     struct {
			Version string `json:"version"`
		} `json:"azd"`
	}
	if json.Unmarshal(b, &v) == nil {
		for _, s := range []string{v.Azd.Version, v.Version} {
			if m := semverRE.FindString(s); m != "" {
				return m
			}
		}
		return ""
	}
	return semverRE.FindString(string(b))
}

type limitedWriter struct {
	w *bytes.Buffer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	total := len(p)
	if total > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	_, _ = l.w.Write(p)
	return total, nil
}
