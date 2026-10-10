// Package project discovers the files of an azd project: azure.yaml, the
// infrastructure directory and the azd environments under .azure.
//
// Discovery is strictly read-only and path-safe. All access goes through
// os.Root so a path or symlink cannot escape the project root. Symlinks inside
// the tree are skipped and reported as warnings. Sizes and counts are bounded.
// Environment value files (.azure/<env>/.env) can hold secrets, so their
// contents are never read; only their presence is recorded.
package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Defaults and bounds.
const (
	DefaultAzureYAML = "azure.yaml"
	DefaultInfraPath = "infra"
	AzureDir         = ".azure"
	MaxAzureYAMLSize = 1 << 20
	DefaultMaxFiles  = 5000
	maxDepth         = 10
)

// Errors returned by Discover. Use errors.Is.
var (
	ErrNoAzureYAML   = errors.New("azure.yaml not found")
	ErrUnsafePath    = errors.New("unsafe path")
	ErrTooLarge      = errors.New("file too large")
	ErrBadEnvName    = errors.New("invalid environment name")
	ErrNotRegularYML = errors.New("azure.yaml is not a regular file")
)

var envNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Options select non-default locations. Paths are relative to the root.
type Options struct {
	AzureYAML   string
	InfraPath   string
	Environment string
	// MaxFiles bounds the number of infra files listed (0 = DefaultMaxFiles).
	MaxFiles int
}

// Project is the discovery result. All paths are slash-separated and relative
// to the root; no absolute path appears in it.
type Project struct {
	AzureYAML     string
	AzureYAMLData []byte
	InfraPath     string
	InfraExists   bool
	// InfraFiles lists *.bicep and *.bicepparam files, sorted.
	InfraFiles []string
	// Environments lists directory names under .azure, sorted.
	Environments []string
	// Environment is the selected environment ("" when none was requested).
	Environment string
	// EnvDirExists reports whether .azure/<Environment> exists.
	EnvDirExists bool
	// EnvFilePresent reports whether .azure/<Environment>/.env exists (never read).
	EnvFilePresent bool
	// Warnings are deterministic, sorted notes (skipped symlinks, truncation).
	Warnings []string
}

// cleanRel validates a root-relative path.
func cleanRel(p, def string) (string, error) {
	if p == "" {
		p = def
	}
	p = filepath.ToSlash(p)
	if strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || !filepath.IsLocal(filepath.FromSlash(p)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, p)
	}
	return path.Clean(p), nil
}

// Discover inspects the project under root.
func Discover(ctx context.Context, root string, opts Options) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ayml, err := cleanRel(opts.AzureYAML, DefaultAzureYAML)
	if err != nil {
		return nil, err
	}
	infra, err := cleanRel(opts.InfraPath, DefaultInfraPath)
	if err != nil {
		return nil, err
	}
	if opts.Environment != "" && !envNameRE.MatchString(opts.Environment) {
		return nil, fmt.Errorf("%w: %q", ErrBadEnvName, opts.Environment)
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	defer r.Close()

	p := &Project{AzureYAML: ayml, InfraPath: infra, Environment: opts.Environment}
	if p.AzureYAMLData, err = readAzureYAML(r, ayml); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warn := map[string]bool{}
	if err := p.scanInfra(ctx, r, maxFiles, warn); err != nil {
		return nil, err
	}
	if err := p.scanEnvs(r, warn); err != nil {
		return nil, err
	}
	for w := range warn {
		p.Warnings = append(p.Warnings, w)
	}
	slices.Sort(p.Warnings)
	return p, nil
}

func readAzureYAML(r *os.Root, rel string) ([]byte, error) {
	fi, err := r.Lstat(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoAzureYAML, rel)
		}
		return nil, fmt.Errorf("stat %s: %w", rel, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegularYML, rel)
	}
	if fi.Size() > MaxAzureYAMLSize {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, rel, MaxAzureYAMLSize)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", rel, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxAzureYAMLSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if len(data) > MaxAzureYAMLSize {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, rel, MaxAzureYAMLSize)
	}
	return data, nil
}

func (p *Project) scanInfra(ctx context.Context, r *os.Root, maxFiles int, warn map[string]bool) error {
	fi, err := r.Lstat(p.InfraPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", p.InfraPath, err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		warn["skipped symlink: "+p.InfraPath] = true
		return nil
	}
	if !fi.IsDir() {
		warn["infra path is not a directory: "+p.InfraPath] = true
		return nil
	}
	p.InfraExists = true
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > maxDepth {
			warn["directory depth limit reached: "+dir] = true
			return nil
		}
		entries, err := readDir(r, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			rel := path.Join(dir, e.Name())
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				warn["skipped symlink: "+rel] = true
			case e.IsDir():
				if err := walk(rel, depth+1); err != nil {
					return err
				}
			case e.Type().IsRegular() && isBicep(e.Name()):
				if len(p.InfraFiles) >= maxFiles {
					warn[fmt.Sprintf("infra file limit %d reached", maxFiles)] = true
					return nil
				}
				p.InfraFiles = append(p.InfraFiles, rel)
			}
		}
		return nil
	}
	if err := walk(p.InfraPath, 0); err != nil {
		return err
	}
	slices.Sort(p.InfraFiles)
	return nil
}

func isBicep(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".bicep" || ext == ".bicepparam"
}

func readDir(r *os.Root, dir string) ([]fs.DirEntry, error) {
	f, err := r.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (p *Project) scanEnvs(r *os.Root, warn map[string]bool) error {
	fi, err := r.Lstat(AzureDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", AzureDir, err)
	}
	if !fi.IsDir() {
		warn[AzureDir+" is not a directory"] = true
		return nil
	}
	entries, err := readDir(r, AzureDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 {
			warn["skipped symlink: "+path.Join(AzureDir, e.Name())] = true
			continue
		}
		if !e.IsDir() {
			continue
		}
		if !envNameRE.MatchString(e.Name()) {
			warn["skipped environment with invalid name: "+path.Join(AzureDir, e.Name())] = true
			continue
		}
		p.Environments = append(p.Environments, e.Name())
	}
	if p.Environment != "" && slices.Contains(p.Environments, p.Environment) {
		p.EnvDirExists = true
		efi, err := r.Lstat(path.Join(AzureDir, p.Environment, ".env"))
		p.EnvFilePresent = err == nil && efi.Mode().IsRegular()
	}
	return nil
}
