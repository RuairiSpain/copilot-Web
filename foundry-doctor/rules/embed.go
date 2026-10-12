// Package ruledata embeds the rule catalogue (rules/catalog) into the binary so
// the CLI works without the source tree. It contains data only.
package ruledata

import "embed"

// FS holds the catalogue under the "catalog" directory.
//
//go:embed catalog
var FS embed.FS

// Root is the catalogue root inside FS.
const Root = "catalog"
