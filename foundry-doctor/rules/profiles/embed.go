// Package profiles embeds the curated severity profiles foundry-dev, foundry-test and foundry-prod.
// The YAML files are generated from rules/catalog by a test in internal/config; do not edit them by hand.
package profiles

import "embed"

// FS holds foundry-dev.yaml, foundry-test.yaml and foundry-prod.yaml at its root.
//
//go:embed foundry-dev.yaml foundry-test.yaml foundry-prod.yaml
var FS embed.FS
