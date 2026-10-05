package config

import "slices"

// Profile is a curated, built-in defaults bundle. Profiles carry severity
// selection and tool defaults only; they never carry organisation values.
type Profile struct {
	// Name is the config value, for example foundry-prod.
	Name string
	// Key selects the severity column in the rule catalogue: dev, test or prod.
	Key string
	// Validation holds the default requirement level per plane.
	Validation Validation
	// PSRule is the default PSRule adapter setting.
	PSRule Tristate
	// Checkov is the default Checkov adapter setting.
	Checkov Tristate
	// Formats are the default report formats.
	Formats []string
}

// profiles is read-only after init and returned by value from lookups.
var profiles = []Profile{
	{Name: "foundry-dev", Key: "dev",
		Validation: Validation{Bicep: ModeOptional, Azure: ModeDisabled, Runtime: ModeDisabled},
		PSRule:     Auto, Checkov: False, Formats: []string{"console"}},
	{Name: "foundry-test", Key: "test",
		Validation: Validation{Bicep: ModeRequired, Azure: ModeOptional, Runtime: ModeDisabled},
		PSRule:     Auto, Checkov: False, Formats: []string{"console", "json"}},
	{Name: "foundry-prod", Key: "prod",
		Validation: Validation{Bicep: ModeRequired, Azure: ModeOptional, Runtime: ModeDisabled},
		PSRule:     Auto, Checkov: False, Formats: []string{"console", "json", "sarif"}},
}

// DefaultProfile is used when no layer names a profile.
const DefaultProfile = "foundry-prod"

// LookupProfile returns the named curated profile.
func LookupProfile(name string) (Profile, bool) {
	for _, p := range profiles {
		if p.Name == name {
			p.Formats = slices.Clone(p.Formats)
			return p, true
		}
	}
	return Profile{}, false
}

// ProfileNames lists the curated profile names in a stable order.
func ProfileNames() []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}
	return out
}
