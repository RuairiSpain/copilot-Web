package findings

import "github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"

// Stamp returns f with Profile set and Severity resolved for that profile. Platform-basis
// rules are errors in every profile. Otherwise the severity comes from severities[profile];
// a missing or invalid entry leaves the rule's own severity unchanged (the caller validates
// rule metadata, not this helper). f is not modified.
func Stamp(f sdk.Finding, profile string, severities map[string]sdk.Severity, platformBasis bool) sdk.Finding {
	f.Profile = profile
	if platformBasis {
		f.Severity = sdk.SeverityError
		return f
	}
	if s, ok := severities[profile]; ok && s.Valid() {
		f.Severity = s
	}
	return f
}
