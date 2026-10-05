// Package rules embeds the rule catalogue and the rule packs so the binary needs no files at run time.
// The directory layout is rules/catalog/<group>/<ID>.yaml and rules/packs/<pack>.yaml.
package rules

import "embed"

// FS holds the embedded catalogue and packs. Use the roots CatalogRoot and PacksRoot.
//
//go:embed catalog packs
var FS embed.FS

// Roots of the embedded trees inside FS.
const (
	CatalogRoot = "catalog"
	PacksRoot   = "packs"
)
