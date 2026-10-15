package azure

import (
	"sort"
	"strings"
)

// Documented Foundry Agent Service feature-region matrix (FND-DEP-012 check 3).
//
// Source: https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions
// (page ms.date 2026-09-07). This is documentation-derived data, not a live
// API: consumers must report findings from it as uncertain and cite the data
// date. Regions absent from the table are "not documented", not "unsupported".
const (
	FoundryFeatureDataDate   = "2026-09-07"
	FoundryFeatureDataSource = "https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions"
)

// FoundryTools lists the tool columns of the documented tool-support table.
var FoundryTools = []string{
	"Agent2Agent", "Azure AI Search", "Browser Automation", "Code Interpreter", "Computer Use",
	"Fabric Data Agent", "File Search", "Function", "Bing Custom Search", "Bing Search",
	"Image Generation", "MCP", "OpenAPI", "SharePoint", "Web Search",
}

// FoundryRegionFeatures is the documented support for one region.
type FoundryRegionFeatures struct {
	Region string // normalised name, e.g. "eastus2"
	// VoiceAgents is the documented "Voice-based agents (preview)" column.
	VoiceAgents bool
	// UnsupportedTools lists FoundryTools entries documented as not supported.
	UnsupportedTools []string
}

// ToolSupported reports whether tool is documented as supported in the region.
func (f FoundryRegionFeatures) ToolSupported(tool string) bool {
	for _, u := range f.UnsupportedTools {
		if strings.EqualFold(u, tool) {
			return false
		}
	}
	return true
}

var foundryRegionTable = map[string]FoundryRegionFeatures{}

func init() {
	add := func(voice bool, unsupported []string, regions ...string) {
		for _, r := range regions {
			foundryRegionTable[NormalizeLocation(r)] = FoundryRegionFeatures{
				Region: NormalizeLocation(r), VoiceAgents: voice, UnsupportedTools: unsupported,
			}
		}
	}
	cu := []string{"Computer Use"}
	add(true, cu, "Australia East", "Canada East", "East US", "France Central", "Germany West Central", "Japan East",
		"Korea Central", "Norway East", "South Africa North", "Southeast Asia", "Switzerland North", "UAE North", "UK South", "West US 3")
	add(false, cu, "Poland Central", "Spain Central")
	add(true, []string{"Computer Use", "File Search", "Function"}, "Brazil South")
	add(true, []string{"Computer Use", "File Search"}, "Italy North")
	add(true, []string{"Browser Automation"}, "Japan West", "Switzerland West", "West Central US")
	add(true, []string{"Computer Use", "Function"}, "North Central US", "South Central US", "West US")
	add(true, nil, "Canada Central", "Central US", "East US 2", "South India", "Sweden Central", "West Europe")
	for _, f := range foundryRegionTable {
		sort.Strings(f.UnsupportedTools)
	}
}

// FoundryRegionSupport looks up the documented feature support of a location
// (name or display name). ok is false when the region is not in the table.
func FoundryRegionSupport(location string) (FoundryRegionFeatures, bool) {
	f, ok := foundryRegionTable[NormalizeLocation(location)]
	return f, ok
}

// FoundryRegions returns the documented regions (normalised), sorted.
func FoundryRegions() []string {
	out := make([]string, 0, len(foundryRegionTable))
	for k := range foundryRegionTable {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var bingGroundingRegions = map[string]bool{}

func init() {
	for _, r := range []string{"West Europe", "Canada East", "Switzerland North", "Spain Central", "UAE North", "Korea Central",
		"Poland Central", "Southeast Asia", "West US", "West US 2", "West US 3", "East US", "East US 2", "Central US",
		"South India", "Japan East", "UK South", "France Central", "Norway East", "Australia East", "Canada Central",
		"Sweden Central", "South Africa North", "Italy North", "Brazil South"} {
		bingGroundingRegions[NormalizeLocation(r)] = true
	}
}

// BingGroundingDocumented reports whether the region is documented as
// supporting Grounding with Bing Search (same source and data date).
func BingGroundingDocumented(location string) bool {
	return bingGroundingRegions[NormalizeLocation(location)]
}

func sortBy[T any](s []T, key func(T) string) {
	sort.Slice(s, func(i, j int) bool { return key(s[i]) < key(s[j]) })
}
