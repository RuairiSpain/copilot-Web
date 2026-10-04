// Package schemas embeds the published x-foundry JSON Schema.
package schemas

import _ "embed"

// SchemaVersion is the schema version this build supports. Documents with a different
// major version are rejected.
const SchemaVersion = "1.0"

// XFoundry is the JSON Schema for the value of the x-foundry key.
//
//go:embed x-foundry.schema.json
var XFoundry []byte
