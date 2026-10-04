package bicep

import "embed"

// modulesFS holds the static, hand-written Bicep modules. They are compiled and linted in CI,
// so the generated resources.bicep only has to wire them together.
//
//go:embed modules/*.bicep
var modulesFS embed.FS
