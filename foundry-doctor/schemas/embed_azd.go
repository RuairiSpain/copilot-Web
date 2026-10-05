// Package schemas embeds schema files that Go code reads at run time.
package schemas

import (
	"embed"
	"io/fs"
)

//go:embed vendor/azd/azure.yaml.json vendor/azd/azure.ai.*/*.json
var azd embed.FS

// AzdFS returns the vendored azd azure.yaml schemas (see vendor/azd/README.md for provenance and licence).
// Paths are relative to vendor/azd: azure.yaml.json and azure.ai.<ext>/<file>.json. $id and $ref values inside the
// files use the same relative paths.
//
// The package lives here because Go refuses to import a package under a directory named vendor.
func AzdFS() fs.FS {
	sub, err := fs.Sub(azd, "vendor/azd")
	if err != nil {
		panic(err) // the path is a constant that the embed directive above guarantees
	}
	return sub
}
