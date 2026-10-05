package project

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Defaults from the azure.yaml schema (ADR-004): infra.path "infra", infra.module "main".
const (
	defaultInfraPath   = "infra"
	defaultInfraModule = "main"
	foundryProvider    = "microsoft.foundry"
)

// Warning codes. A warning never fails discovery; it explains a weaker result.
const (
	WarnShadowedAzureYAML = "shadowed-azure-yaml"  // another azure.yaml exists in a parent directory
	WarnBothAzureYAML     = "azure-yaml-and-yml"   // azure.yaml and azure.yml both exist; azure.yaml is used
	WarnCaseVariant       = "azure-yaml-case"      // a differently-cased azure.yaml exists
	WarnAzureYAMLParse    = "azure-yaml-unparsed"  // infra settings fell back to defaults
	WarnInfraPathInvalid  = "infra-path-invalid"   // infra.path is not a safe relative path
	WarnInfraNotDir       = "infra-not-directory"  // the infra path exists but is not a directory
	WarnInfraLayers       = "infra-layers-ignored" // infra.layers present, not inspected
	WarnInfraModuleAbsent = "infra-module-absent"  // .bicep files exist but not <infra>/<module>.bicep
	WarnProviderOther     = "infra-provider-other" // infra.provider names a provider other than bicep/foundry with no files
	WarnListTruncated     = "listing-truncated"    // a limit stopped the infra walk
	WarnBrokenLink        = "unresolvable-link"    // a link in the infra tree could not be resolved inside the root
	WarnAzureDirNotDir    = "azure-dir-not-directory"
	WarnAzureDirUnread    = "azure-dir-unreadable"
	WarnEnvCaseCollision  = "environment-case-collision"
)

// Warning is a non-fatal discovery note. Path is root-relative with forward slashes.
type Warning struct {
	Code string
	Path string
}

// Capability names a fact discovery could not establish and why. A dependent check is skipped
// naming Name as the missing capability.
type Capability struct {
	Name   string
	Reason string
}

// Capability names.
const CapabilityTrackedFiles = "git tracked-file list"

// InfraFiles lists files under the infra path, root-relative, sorted.
type InfraFiles struct {
	Bicep      []string
	BicepParam []string
	ARMJSON    []string // *.json whose $schema is an ARM deployment template
	Truncated  bool
}

// GitignoreInfo summarises the root .gitignore. Only that file is read: nested, global and
// info/exclude rules are not evaluated, and a pattern's effect is judged by exact text.
type GitignoreInfo struct {
	Present      bool
	IgnoresAzure bool // a non-negated pattern ignores the .azure directory
	Negations    bool // the file has "!" patterns, so IgnoresAzure may be overridden
}

// RepoInfo is repository metadata used by OPS-007.
type RepoInfo struct {
	PipelineFiles []string // existing azd pipeline definitions, root-relative, sorted
	HasGit        bool     // a .git directory or file exists at the root
	Gitignore     GitignoreInfo
}

// Discovery is the result of Discover.
type Discovery struct {
	Project           model.Project
	ShadowedAzureYAML []string // parent azure.yaml/yml paths, nearest first, relative to the start directory
	Infra             InfraFiles
	Repo              RepoInfo
	Warnings          []Warning
	Unavailable       []Capability
	InfraProvider     string // infra.provider from azure.yaml, "" when unset
}

var azureYAMLNames = []string{"azure.yaml", "azure.yml"}

// pipelineCandidates are the azd pipeline definition locations (fact-check 5): GitHub workflows
// and the two Azure DevOps folders, each with .yml and .yaml.
var pipelineCandidates = []string{
	".github/workflows/azure-dev.yml", ".github/workflows/azure-dev.yaml",
	".azdo/pipelines/azure-dev.yml", ".azdo/pipelines/azure-dev.yaml",
	".azuredevops/pipelines/azure-dev.yml", ".azuredevops/pipelines/azure-dev.yaml",
}

// Discover finds the project that contains start (a directory, or a file whose directory is
// used): the nearest directory at or above start holding azure.yaml or azure.yml. It reads
// azure.yaml only for name, infra.path, infra.module, infra.provider and service hosts. It runs no
// subprocess (no git, no azd, no bicep).
func Discover(ctx context.Context, start string, opts ...Option) (*Discovery, error) {
	limits := buildLimits(opts)
	dir, err := startDir(start)
	if err != nil {
		return nil, err
	}
	rootDir, yamlName, shadowed, err := findProject(ctx, dir, limits.MaxAncestors)
	if err != nil {
		return nil, err
	}
	c, err := openConfined(rootDir)
	if err != nil {
		return nil, err
	}
	defer c.close()

	d := &Discovery{}
	d.Project.Root = rootDir
	d.Project.AzureYAMLPath = yamlName
	for _, s := range shadowed {
		d.ShadowedAzureYAML = append(d.ShadowedAzureYAML, s)
		d.warn(WarnShadowedAzureYAML, s)
	}
	d.checkAzureYAMLVariants(ctx, c, yamlName)

	data, err := c.readFile(ctx, yamlName, limits.MaxFileBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", yamlName, err)
	}
	cfg := readAzureYAML(data)
	if !cfg.ok {
		d.warn(WarnAzureYAMLParse, yamlName)
	}
	d.Project.Name = cfg.Name
	if d.Project.Name == "" {
		d.Project.Name = filepath.Base(rootDir)
	}
	d.InfraProvider = cfg.Provider
	if cfg.hasLayers {
		d.warn(WarnInfraLayers, yamlName)
	}

	infra := cfg.Path
	if infra == "" {
		infra = defaultInfraPath
	}
	module := cfg.Module
	if module == "" {
		module = defaultInfraModule
	}
	d.Project.InfraModule = module
	if clean, verr := ValidateRel(infra); verr != nil || clean == "." {
		d.warn(WarnInfraPathInvalid, yamlName)
	} else {
		d.Project.InfraPath = clean
		d.scanInfra(ctx, c, clean, limits)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.Project.IaC = d.detectIaC(cfg, module)

	d.scanRepo(ctx, c, limits)
	d.scanEnvironments(ctx, rootDir, limits)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.Unavailable = append(d.Unavailable, Capability{
		Name:   CapabilityTrackedFiles,
		Reason: "reading the git index is not implemented and git is never run; files cannot be shown to be tracked or untracked",
	})
	slices.SortFunc(d.Warnings, func(a, b Warning) int {
		if c := strings.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return d, nil
}

func (d *Discovery) warn(code, p string) {
	d.Warnings = append(d.Warnings, Warning{Code: code, Path: p})
}

func startDir(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start directory: %w", err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("start directory: %w", sanitizeErr(err))
	}
	if !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	return abs, nil
}

// findProject walks from dir upwards. The nearest directory with azure.yaml (preferred) or
// azure.yml is the root; farther ones are returned as slash paths relative to dir.
func findProject(ctx context.Context, dir string, maxUp int) (root, name string, shadowed []string, err error) {
	cur := dir
	for i := 0; i < maxUp; i++ {
		if err := ctx.Err(); err != nil {
			return "", "", nil, err
		}
		for _, n := range azureYAMLNames {
			fi, serr := os.Stat(filepath.Join(cur, n))
			if serr != nil || !fi.Mode().IsRegular() {
				continue
			}
			if root == "" {
				root, name = cur, n
			} else {
				rel, rerr := filepath.Rel(dir, filepath.Join(cur, n))
				if rerr == nil {
					shadowed = append(shadowed, filepath.ToSlash(rel))
				}
			}
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if root == "" {
		return "", "", nil, ErrNoProject
	}
	return root, name, shadowed, nil
}

func (d *Discovery) checkAzureYAMLVariants(ctx context.Context, c *confined, chosen string) {
	ents, err := c.readDir(ctx, ".")
	if err != nil {
		return
	}
	var exact int
	for _, e := range ents {
		n := e.Name()
		for _, want := range azureYAMLNames {
			switch {
			case n == want && e.Type().IsRegular():
				exact++
			case n != want && strings.EqualFold(n, want):
				d.warn(WarnCaseVariant, n)
			}
		}
	}
	if exact > 1 {
		d.warn(WarnBothAzureYAML, chosen)
	}
}

// azureYAMLLite is the minimum azure.yaml view discovery needs. The full, lossless reader is
// internal/azureyaml; this one never reports YAML diagnostics.
type azureYAMLLite struct {
	Name     string
	Provider string
	Path     string
	Module   string
	Hosts    []string
	ok       bool

	hasLayers bool
}

func readAzureYAML(data []byte) azureYAMLLite {
	var raw struct {
		Name  string `yaml:"name"`
		Infra *struct {
			Provider string `yaml:"provider"`
			Path     string `yaml:"path"`
			Module   string `yaml:"module"`
			Layers   any    `yaml:"layers"`
		} `yaml:"infra"`
		Services map[string]*struct {
			Host string `yaml:"host"`
		} `yaml:"services"`
	}
	var out azureYAMLLite
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return out // fall back to defaults; the azure.yaml reader reports the syntax error
	}
	out.ok = true
	out.Name = raw.Name
	if raw.Infra != nil {
		out.Provider = raw.Infra.Provider
		out.Path = raw.Infra.Path
		out.Module = raw.Infra.Module
		out.hasLayers = raw.Infra.Layers != nil
	}
	for _, s := range raw.Services {
		if s != nil && s.Host != "" {
			out.Hosts = append(out.Hosts, s.Host)
		}
	}
	slices.Sort(out.Hosts)
	return out
}

func isFoundryHost(h string) bool {
	return h == "microsoft.foundry" || strings.HasPrefix(h, "azure.ai.")
}

func (d *Discovery) detectIaC(cfg azureYAMLLite, module string) model.IaCMode {
	infra := d.Project.InfraPath
	if infra != "" {
		has := func(list []string, file string) bool { return slices.Contains(list, slashJoin(infra, file)) }
		if has(d.Infra.Bicep, module+".bicep") || has(d.Infra.BicepParam, module+".bicepparam") {
			return model.IaCBicep
		}
		if len(d.Infra.Bicep)+len(d.Infra.BicepParam) > 0 {
			d.warn(WarnInfraModuleAbsent, slashJoin(infra, module+".bicep"))
		}
		if len(d.Infra.ARMJSON) > 0 {
			return model.IaCARMJSON
		}
	}
	if cfg.Provider == "" || cfg.Provider == foundryProvider {
		if slices.ContainsFunc(cfg.Hosts, isFoundryHost) {
			return model.IaCSynthetic
		}
	} else if cfg.Provider != "bicep" {
		d.warn(WarnProviderOther, d.Project.AzureYAMLPath)
	}
	return model.IaCNone
}

func (d *Discovery) scanInfra(ctx context.Context, c *confined, infra string, l Limits) {
	_, fi, err := c.stat(infra)
	if err != nil {
		return // absent, or refused: no infra files
	}
	if !fi.IsDir() {
		d.warn(WarnInfraNotDir, infra)
		return
	}
	sub, err := c.root.OpenRoot(infra)
	if err != nil {
		d.warn(WarnBrokenLink, infra)
		return
	}
	defer sub.Close()
	visited, sniffs := 0, 0
	walkErr := fs.WalkDir(sub.FS(), ".", func(p string, e fs.DirEntry, werr error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if werr != nil {
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == "." || e.IsDir() {
			return nil
		}
		visited++
		if visited > l.MaxListedFiles {
			d.Infra.Truncated = true
			return fs.SkipAll
		}
		full := slashJoin(infra, p)
		if e.Type()&fs.ModeSymlink != 0 {
			if _, serr := c.root.Stat(full); serr != nil {
				d.warn(WarnBrokenLink, full)
				return nil
			}
		}
		switch strings.ToLower(path.Ext(p)) {
		case ".bicep":
			d.Infra.Bicep = append(d.Infra.Bicep, full)
		case ".bicepparam":
			d.Infra.BicepParam = append(d.Infra.BicepParam, full)
		case ".json":
			if sniffs >= l.MaxARMSniffs {
				d.Infra.Truncated = true
				return nil
			}
			sniffs++
			if data, rerr := c.readFile(ctx, full, l.MaxFileBytes); rerr == nil && isARMTemplate(data) {
				d.Infra.ARMJSON = append(d.Infra.ARMJSON, full)
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, ctx.Err()) {
		d.warn(WarnBrokenLink, infra)
	}
	if d.Infra.Truncated {
		d.warn(WarnListTruncated, infra)
	}
	slices.Sort(d.Infra.Bicep)
	slices.Sort(d.Infra.BicepParam)
	slices.Sort(d.Infra.ARMJSON)
}

// isARMTemplate reports whether data is a JSON object whose "$schema" names an ARM deployment
// template (".../deploymentTemplate.json#"). Parameter files and other JSON do not match.
func isARMTemplate(data []byte) bool {
	var doc struct {
		Schema string `json:"$schema"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	s := strings.ToLower(strings.TrimSuffix(doc.Schema, "#"))
	return strings.HasSuffix(s, "/deploymenttemplate.json")
}

func (d *Discovery) scanRepo(ctx context.Context, c *confined, l Limits) {
	for _, p := range pipelineCandidates {
		if _, fi, err := c.stat(p); err == nil && fi.Mode().IsRegular() {
			d.Repo.PipelineFiles = append(d.Repo.PipelineFiles, p)
		}
	}
	slices.Sort(d.Repo.PipelineFiles)
	if _, err := c.root.Lstat(".git"); err == nil {
		d.Repo.HasGit = true
	}
	data, err := c.readFile(ctx, ".gitignore", l.MaxFileBytes)
	if err != nil {
		return
	}
	d.Repo.Gitignore = parseGitignore(data)
}

// azureIgnorePatterns are the exact texts that ignore the whole .azure directory from the root.
var azureIgnorePatterns = map[string]struct{}{
	".azure": {}, ".azure/": {}, "/.azure": {}, "/.azure/": {}, ".azure/*": {}, "/.azure/*": {},
	"**/.azure": {}, "**/.azure/": {},
}

func parseGitignore(data []byte) GitignoreInfo {
	info := GitignoreInfo{Present: true}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			info.Negations = true
			continue
		}
		if _, ok := azureIgnorePatterns[line]; ok {
			info.IgnoresAzure = true
		}
	}
	return info
}

func (d *Discovery) scanEnvironments(ctx context.Context, root string, l Limits) {
	az, err := NewStandaloneAzdContext(root, WithLimits(l))
	if err != nil {
		return
	}
	defer az.Close()
	names, err := az.Environments(ctx)
	switch {
	case err == nil:
		d.Project.Environments = names
	case errors.Is(err, ErrNotDirectory):
		d.warn(WarnAzureDirNotDir, azureDir)
	default:
		d.warn(WarnAzureDirUnread, azureDir)
	}
	for _, g := range CaseCollisions(d.Project.Environments) {
		d.warn(WarnEnvCaseCollision, strings.Join(g, ","))
	}
	if cur, err := az.CurrentEnvironment(ctx); err == nil {
		d.Project.CurrentEnvironment = cur
	}
}
