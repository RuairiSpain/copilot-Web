package cfg

import (
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// azureRegionNames are the public, US Government and China cloud location
// names that the Azure SDK for .NET declares as static AzureLocation values
// (sdk/core/Azure.Core/src/AzureLocation.cs, "Public Cloud Locations" region,
// retrieved 2026-10-19 from Azure/azure-sdk-for-net main, MIT licence).
//
// The list is a point-in-time SDK subset, not a live subscription list: a
// region added to Azure after the SDK was last updated is absent. It is
// therefore used to recognise a name, never to reject one. An alphanumeric
// name that is not here stays uncertain (FND-CFG-012).
var azureRegionNames = []string{
	"eastasia", "southeastasia", "centralus", "eastus", "eastus2", "westus", "westus2", "westus3",
	"northcentralus", "southcentralus", "northeurope", "westeurope", "japanwest", "japaneast",
	"brazilsouth", "australiaeast", "australiasoutheast", "southindia", "centralindia", "westindia",
	"canadacentral", "canadaeast", "uksouth", "ukwest", "westcentralus", "koreacentral", "koreasouth",
	"francecentral", "francesouth", "australiacentral", "australiacentral2", "uaecentral", "uaenorth",
	"southafricanorth", "southafricawest", "swedencentral", "swedensouth", "switzerlandnorth",
	"switzerlandwest", "germanynorth", "germanywestcentral", "germanycentral", "germanynortheast",
	"norwaywest", "norwayeast", "brazilsoutheast", "chinanorth", "chinaeast", "chinanorth2",
	"chinanorth3", "chinaeast2", "chinaeast3", "qatarcentral", "usdodcentral", "usdodeast",
	"usgovarizona", "usgovtexas", "usgovvirginia", "usgoviowa", "israelcentral", "italynorth",
	"polandcentral", "mexicocentral", "spaincentral", "indonesiacentral", "newzealandnorth",
	"malaysiawest", "denmarkeast",
}

// regionNameShape is the form every name in azureRegionNames and in the
// documented Foundry region table has: lowercase ASCII letters and digits.
// That every Azure location name has this form is inferred from those lists;
// no source read states it.
var regionNameShape = regexp.MustCompile(`^[a-z0-9]+$`)

// knownRegions returns the sorted union of the SDK names and the Foundry
// documented regions (normalised).
func knownRegions() []string {
	set := map[string]bool{}
	for _, r := range azureRegionNames {
		set[r] = true
	}
	for _, r := range azure.FoundryRegions() {
		set[r] = true
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// regionKey lowercases and removes spaces and tabs, so "East US" and "eastus"
// compare equal.
func regionKey(v string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "\t", "").Replace(strings.TrimSpace(v)))
}

func isKnownRegion(key string) bool {
	for _, r := range knownRegions() {
		if r == key {
			return true
		}
	}
	return false
}

// nearestRegion returns the known region with the smallest edit distance to
// key and that distance. Ties go to the alphabetically first name.
func nearestRegion(key string) (string, int) {
	best, bestD := "", 1<<30
	for _, r := range knownRegions() {
		if d := editDistance(key, r); d < bestD {
			best, bestD = r, d
		}
	}
	return best, bestD
}

// maxSuggestDistance bounds a "did you mean" suggestion.
const maxSuggestDistance = 3

// suggestion returns " Did you mean "x"?" for a near region, or "". For a
// value with punctuation, each letters-and-digits segment (at least three
// characters) is measured as well as the joined letters, so "eastus-typo"
// suggests "eastus".
func suggestion(key string) string {
	cands := []string{alnumOnly(key)}
	for _, seg := range strings.FieldsFunc(key, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) {
		if len(seg) >= 3 {
			cands = append(cands, seg)
		}
	}
	best, bestD := "", 1<<30
	for _, c := range cands {
		if r, d := nearestRegion(c); d < bestD {
			best, bestD = r, d
		}
	}
	if best == "" || bestD > maxSuggestDistance {
		return ""
	}
	return " Did you mean \"" + best + "\"?"
}

// alnumOnly drops every character that is not a lowercase letter or digit, so
// the distance of "eastus-typo" is measured on its letters.
func alnumOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// editDistance is the Levenshtein distance over bytes.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
