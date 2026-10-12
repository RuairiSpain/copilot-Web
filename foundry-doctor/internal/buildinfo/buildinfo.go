// Package buildinfo holds the version of the running binary. The variables are set at link time:
//
//	-ldflags "-X github.com/ruairispain/copilot-web/foundry-doctor/internal/buildinfo.Version=v1.2.3"
//
// scripts/ci/build-matrix.sh sets Version. Commit and Date are optional. They are the only package
// variables that are not immutable metadata, and only the linker writes them.
package buildinfo

import "fmt"

// Link-time variables.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// ToolName is the name written in every report.
const ToolName = "foundry-doctor"

// String returns a one-line description such as "foundry-doctor dev (commit unknown, built unknown)".
func String() string {
	return Describe(Version, Commit, Date)
}

// Describe formats the three values; it exists so tests do not depend on the link-time variables.
func Describe(version, commit, date string) string {
	return fmt.Sprintf("%s %s (commit %s, built %s)", ToolName, version, commit, date)
}
