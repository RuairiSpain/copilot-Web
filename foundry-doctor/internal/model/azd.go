package model

import (
	"context"
	"errors"
)

// ErrUnavailable is returned by an AzdContext method when the information is not available,
// for example in the standalone binary with no azd project context. Callers skip dependent checks.
var ErrUnavailable = errors.New("azd context unavailable")

// Versions are the detected tool versions the compatibility check compares against rule ranges.
type Versions struct {
	Azd        string
	Extensions map[string]string // extension id to version
	Bicep      string
}

// AzdContext is how core logic reads azd project state. It uses core types only (ADR-005);
// the extension module implements it with azdext, the standalone binary with the file system.
type AzdContext interface {
	// ProjectRoot returns the absolute project directory.
	ProjectRoot(ctx context.Context) (string, error)
	// ReadFile reads a file by path relative to the project root. It rejects absolute paths,
	// ".." escapes and symlinks that leave the root.
	ReadFile(ctx context.Context, rel string) ([]byte, error)
	// Environments lists environment names, sorted.
	Environments(ctx context.Context) ([]string, error)
	// CurrentEnvironment returns the default environment name, or ErrUnavailable.
	CurrentEnvironment(ctx context.Context) (string, error)
	// EnvValues returns the values of one environment. Values never leave the Environment type unredacted.
	EnvValues(ctx context.Context, name string) (Environment, error)
	// Versions returns detected tool versions.
	Versions(ctx context.Context) (Versions, error)
}
