// Package project discovers a Foundry azd project on the file system, reads its environments and
// implements model.AzdContext for the standalone binary.
//
// Every read of project content goes through an os.Root so a symlink or ".." cannot leave the
// project root. Environment values are held only in model.Environment, which never prints them;
// errors from this package carry key names, relative paths and line numbers, never values.
//
// IaC mode rule (ADR-004), evaluated in order against the infra path (azure.yaml infra.path,
// default "infra") and module (infra.module, default "main"):
//
//  1. <infra>/<module>.bicep or <infra>/<module>.bicepparam exists: "bicep".
//  2. else at least one *.json under <infra> has an ARM deployment-template $schema: "arm-json".
//  3. else, when infra.provider is empty or "microsoft.foundry" and at least one service uses host
//     "azure.ai.*" or "microsoft.foundry": "synthetic" (the provider synthesises the template).
//  4. else "none".
//
// infra.layers is not inspected (its field names are not verified); a warning is raised when present.
package project

import (
	"errors"
)

// Sentinel errors. Callers use errors.Is.
var (
	// ErrUnsafePath is returned for a relative path that is empty, absolute, contains "..", a NUL
	// byte, a control character, a drive or stream colon, or a Windows reserved device name.
	ErrUnsafePath = errors.New("unsafe path")
	// ErrSymlinkEscape is returned when resolving a path would leave the project root.
	ErrSymlinkEscape = errors.New("path escapes the project root")
	// ErrSymlinkLoop is returned for a symlink cycle or an excessively deep link chain.
	ErrSymlinkLoop = errors.New("too many levels of symbolic links")
	// ErrTooLarge is returned when a file exceeds the configured size limit.
	ErrTooLarge = errors.New("file exceeds size limit")
	// ErrNotRegular is returned when a path is not a regular file (directory, device, pipe).
	ErrNotRegular = errors.New("not a regular file")
	// ErrNoProject is returned by Discover when no azure.yaml or azure.yml exists in the start
	// directory or any parent.
	ErrNoProject = errors.New("no azure.yaml found")
	// ErrNotDirectory is returned when .azure exists but is not a directory.
	ErrNotDirectory = errors.New(".azure is not a directory")
)

// Limits bounds what the package reads. A zero field means the default.
type Limits struct {
	MaxFileBytes   int64 // any file read through ReadFile or Discover; default 4 MiB
	MaxEnvBytes    int64 // one .env file; default 1 MiB
	MaxListedFiles int   // entries visited while walking the infra directory; default 5000
	MaxARMSniffs   int   // JSON files opened to test for an ARM template; default 200
	MaxAncestors   int   // directories examined walking up from the start; default 128
}

// DefaultLimits returns the default limits.
func DefaultLimits() Limits {
	return Limits{MaxFileBytes: 4 << 20, MaxEnvBytes: 1 << 20, MaxListedFiles: 5000, MaxARMSniffs: 200, MaxAncestors: 128}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxEnvBytes <= 0 {
		l.MaxEnvBytes = d.MaxEnvBytes
	}
	if l.MaxListedFiles <= 0 {
		l.MaxListedFiles = d.MaxListedFiles
	}
	if l.MaxARMSniffs <= 0 {
		l.MaxARMSniffs = d.MaxARMSniffs
	}
	if l.MaxAncestors <= 0 {
		l.MaxAncestors = d.MaxAncestors
	}
	return l
}

// Option configures Discover and NewStandaloneAzdContext.
type Option func(*Limits)

// WithLimits overrides the limits; zero fields keep their defaults.
func WithLimits(l Limits) Option { return func(dst *Limits) { *dst = l } }

func buildLimits(opts []Option) Limits {
	var l Limits
	for _, o := range opts {
		o(&l)
	}
	return l.withDefaults()
}
