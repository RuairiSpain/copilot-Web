package runtime

import (
	"fmt"
	"regexp"
	"strings"
)

var dataPlaneNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,59}$`)

// ValidateDataPlaneName rejects hostile or non-canonical names before they are
// interpolated into data-plane hosts or paths.
func ValidateDataPlaneName(kind, name string) error {
	if !dataPlaneNameRe.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	return nil
}

func hostMatchesSuffix(hostname, suffix string) bool {
	if suffix == "" {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(hostname))
	want := strings.ToLower(strings.TrimSpace(suffix))
	if host == "" || want == "" {
		return false
	}
	if strings.HasPrefix(want, ".") {
		return strings.HasSuffix(host, want)
	}
	return host == want || strings.HasSuffix(host, "."+want)
}
