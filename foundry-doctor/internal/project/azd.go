package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// azd layout, verified in docs/development/phase-1-fact-check.md (fact 2b):
// .azure/config.json holds "defaultEnvironment"; each environment is a subdirectory of .azure
// with its values in .env.
const (
	azureDir   = ".azure"
	configFile = "config.json"
	envFile    = ".env"
)

// StandaloneAzdContext implements model.AzdContext over the file system for the standalone
// binary. It is safe for concurrent use. Call Close to release the root handle.
type StandaloneAzdContext struct {
	root   string
	c      *confined
	limits Limits
}

var _ model.AzdContext = (*StandaloneAzdContext)(nil)

// NewStandaloneAzdContext opens the project root directory. root is made absolute; it is not
// required to contain azure.yaml (use Discover to find the root).
func NewStandaloneAzdContext(root string, opts ...Option) (*StandaloneAzdContext, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	c, err := openConfined(abs)
	if err != nil {
		return nil, err
	}
	return &StandaloneAzdContext{root: abs, c: c, limits: buildLimits(opts)}, nil
}

// Close releases the root handle.
func (s *StandaloneAzdContext) Close() error { return s.c.close() }

// ProjectRoot implements model.AzdContext.
func (s *StandaloneAzdContext) ProjectRoot(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.root, nil
}

// ReadFile implements model.AzdContext. rel uses either slash style. It is refused when it is
// absolute, contains "..", names a Windows reserved device, resolves through a symlink that leaves
// the root, is not a regular file, or is larger than Limits.MaxFileBytes.
func (s *StandaloneAzdContext) ReadFile(ctx context.Context, rel string) ([]byte, error) {
	return s.c.readFile(ctx, rel, s.limits.MaxFileBytes)
}

// Environments implements model.AzdContext: the sorted names of the real subdirectories of
// .azure (symbolic links are not directories, as in azd). A missing .azure yields an empty list.
// Names that are not safe single path segments are skipped.
func (s *StandaloneAzdContext) Environments(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, fi, err := s.c.stat(azureDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("list environments: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("list environments: %w", ErrNotDirectory)
	}
	ents, err := s.c.readDir(ctx, azureDir)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	names := []string{}
	for _, e := range ents {
		if e.IsDir() && ValidateName(e.Name()) == nil {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

type azdConfig struct {
	DefaultEnvironment string `json:"defaultEnvironment"`
}

// CurrentEnvironment implements model.AzdContext. A missing, unreadable or empty
// .azure/config.json, or an unsafe defaultEnvironment, yields an error wrapping
// model.ErrUnavailable so dependent checks skip.
func (s *StandaloneAzdContext) CurrentEnvironment(ctx context.Context) (string, error) {
	data, err := s.c.readFile(ctx, azureDir+"/"+configFile, s.limits.MaxFileBytes)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", fmt.Errorf("default environment (%s/%s): %w: %w", azureDir, configFile, model.ErrUnavailable, err)
	}
	var cfg azdConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("default environment (%s/%s): %w: invalid JSON", azureDir, configFile, model.ErrUnavailable)
	}
	if cfg.DefaultEnvironment == "" {
		return "", fmt.Errorf("default environment: %w: defaultEnvironment not set", model.ErrUnavailable)
	}
	if err := ValidateName(cfg.DefaultEnvironment); err != nil {
		return "", fmt.Errorf("default environment: %w: defaultEnvironment is not a safe name", model.ErrUnavailable)
	}
	return cfg.DefaultEnvironment, nil
}

// EnvValues implements model.AzdContext. The environment name must be a safe single segment. A
// missing .env wraps fs.ErrNotExist (the environment exists but was never provisioned, or does
// not exist). A syntax error is a *ParseError. The .env size limit is Limits.MaxEnvBytes.
func (s *StandaloneAzdContext) EnvValues(ctx context.Context, name string) (model.Environment, error) {
	if err := ValidateName(name); err != nil {
		return model.Environment{}, fmt.Errorf("environment name: %w", err)
	}
	data, err := s.c.readFile(ctx, azureDir+"/"+name+"/"+envFile, s.limits.MaxEnvBytes)
	if err != nil {
		return model.Environment{}, fmt.Errorf("environment %q: %w", name, err)
	}
	res, err := ParseEnv(ctx, name, data)
	if err != nil {
		return model.Environment{}, fmt.Errorf("environment %q: %w", name, err)
	}
	return res.Env, nil
}

// Versions implements model.AzdContext. The standalone binary does not run azd or the Bicep CLI
// from this package, so versions are unavailable here.
func (s *StandaloneAzdContext) Versions(ctx context.Context) (model.Versions, error) {
	if err := ctx.Err(); err != nil {
		return model.Versions{}, err
	}
	return model.Versions{}, model.ErrUnavailable
}

// slashJoin joins forward-slash path elements, skipping empties.
func slashJoin(parts ...string) string {
	var p []string
	for _, s := range parts {
		if s != "" && s != "." {
			p = append(p, strings.Trim(s, "/"))
		}
	}
	return strings.Join(p, "/")
}
