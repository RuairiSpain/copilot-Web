package validate

import "strings"

var knownRegions = func() map[string]bool {
	m := map[string]bool{}
	for _, r := range strings.Fields(`
		australiacentral australiaeast australiasoutheast austriaeast belgiumcentral brazilsouth
		brazilsoutheast canadacentral canadaeast centralindia centralus chilecentral eastasia eastus
		eastus2 francecentral germanywestcentral indonesiacentral israelcentral italynorth japaneast
		japanwest koreacentral koreasouth malaysiawest mexicocentral newzealandnorth northcentralus
		northeurope norwayeast polandcentral qatarcentral southafricanorth southcentralus southindia
		southeastasia spaincentral swedencentral switzerlandnorth switzerlandwest uaenorth uksouth
		ukwest westcentralus westeurope westindia westus westus2 westus3`) {
		m[r] = true
	}
	return m
}()

// canonicalRegion makes "West Europe" and "westeurope" compare equal.
func canonicalRegion(location string) string {
	return strings.ToLower(strings.Join(strings.Fields(location), ""))
}

func isKnownRegion(location string) bool { return knownRegions[canonicalRegion(location)] }
