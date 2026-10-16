package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Bounds for environment value files. They exist so a hostile or huge file
// cannot exhaust memory; larger files are ignored, not partially trusted.
const (
	maxEnvFileBytes = 256 << 10
	maxEnvKeys      = 2000
)

// layoutProvider answers directory questions for the cfg rules. Paths are
// resolved relative to the directory holding azure.yaml, as azd does, and can
// never leave the project root.
type layoutProvider struct {
	root *os.Root
	base string // slash-separated directory of azure.yaml, "" for the root
}

func newLayoutProvider(src Source) (*layoutProvider, func()) {
	if src.Dir == "" {
		return nil, func() {}
	}
	root, err := os.OpenRoot(src.Dir)
	if err != nil {
		return nil, func() {}
	}
	base := path.Dir(src.AzureYAMLAt)
	if base == "." {
		base = ""
	}
	return &layoutProvider{root: root, base: base}, func() { _ = root.Close() }
}

func (l *layoutProvider) resolve(rel string) (string, bool) {
	if l == nil || l.root == nil {
		return "", false
	}
	if rel == "" || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return "", false
	}
	p := path.Join(l.base, rel)
	if p == ".." || strings.HasPrefix(p, "../") || p == "" {
		return "", false
	}
	return p, true
}

func (l *layoutProvider) DirExists(rel string) bool {
	p, ok := l.resolve(rel)
	if !ok {
		return false
	}
	fi, err := l.root.Stat(p)
	return err == nil && fi.IsDir()
}

func (l *layoutProvider) FileExists(rel string) bool {
	p, ok := l.resolve(rel)
	if !ok {
		return false
	}
	fi, err := l.root.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// envStore reads azd environment files on demand. Values are read only because
// the FND-CFG/ENV rules compare them; secret-looking values are replaced with a
// salted digest so equality still works but no secret can reach a finding, log
// or report.
type envStore struct {
	src  Source
	view sdk.AzureYAMLView

	once   sync.Once
	envs   []env.Environment
	byEn   map[string]map[string]string
	expand map[string]map[string]bool
	shapes map[string]map[string]env.Shape
	status map[string]env.Status
}

// literalResourceGroups lists the azure.yaml locations whose resourceGroup is
// a non-empty literal without a variable reference: the top-level key and the
// key of each service.
func literalResourceGroups(view sdk.AzureYAMLView) []string {
	if view == nil {
		return nil
	}
	var at []string
	literal := func(path ...string) bool {
		v, _, ok := view.Lookup(path...)
		return ok && strings.TrimSpace(v) != "" && !strings.Contains(v, "${")
	}
	if literal("resourceGroup") {
		at = append(at, "resourceGroup")
	}
	for _, n := range view.ServiceNames() {
		if literal("services", n, "resourceGroup") {
			at = append(at, "services."+n+".resourceGroup")
		}
	}
	return at
}

func (s *envStore) load() {
	s.once.Do(func() {
		s.byEn = map[string]map[string]string{}
		s.expand = map[string]map[string]bool{}
		s.shapes = map[string]map[string]env.Shape{}
		s.status = map[string]env.Status{}
		if s.src.Dir == "" {
			return
		}
		names := slices.Clone(s.src.Environments)
		slices.Sort(names)
		root, rootErr := os.OpenRoot(s.src.Dir)
		if rootErr == nil {
			defer root.Close()
		}
		literalRG := literalResourceGroups(s.view)
		for _, n := range names {
			vals, st := map[string]string{}, env.StatusUnreadable
			var shapes map[string]env.Shape
			var expanded map[string]bool
			if rootErr == nil {
				vals, shapes, expanded, st = readEnvFile(root, path.Join(project.AzureDir, n, ".env"))
			}
			s.expand[n] = expanded
			s.byEn[n] = vals
			s.shapes[n] = shapes
			s.status[n] = st
			s.envs = append(s.envs, env.Environment{
				Name: n, Subscription: vals["AZURE_SUBSCRIPTION_ID"], ResourceGroup: vals["AZURE_RESOURCE_GROUP"],
				LiteralRGAt: literalRG, Status: st, Values: vals, Shapes: shapes,
			})
		}
	})
}

// Environments implements env.Provider.
func (s *envStore) Environments() []env.Environment {
	s.load()
	return slices.Clone(s.envs)
}

// Selected implements cfg.EnvView.
func (s *envStore) Selected() (string, bool) {
	s.load()
	if s.src.Environment == "" {
		return "", false
	}
	if _, ok := s.byEn[s.src.Environment]; !ok || s.status[s.src.Environment] != env.StatusOK {
		return "", false
	}
	return s.src.Environment, true
}

// SelectionReason implements cfg.EnvSelection. It says why Selected reports
// false: no environment chosen, the chosen environment's .env file is
// missing or unusable, or the chosen environment is not a directory under .azure.
func (s *envStore) SelectionReason() string {
	s.load()
	name := s.src.Environment
	if name == "" {
		if s.src.DefaultEnvironment != "" {
			return "default-environment-not-found"
		}
		return "no-environment-selected"
	}
	if _, ok := s.byEn[name]; !ok {
		return "default-environment-not-found"
	}
	switch s.status[name] {
	case env.StatusMissing:
		return "env-file-missing"
	case env.StatusUnparseable:
		return "env-file-unparseable"
	case env.StatusOversize, env.StatusTruncated:
		return "env-file-oversize"
	default:
		return "env-file-unreadable"
	}
}

// SelectedByName implements cfg.EnvNameSelection. The environment counts as
// selected by name when a name was given and it is not just the default from
// .azure/config.json. A name that equals the default is treated as the default,
// which only makes the rules less certain, never more.
func (s *envStore) SelectedByName() bool {
	if _, ok := s.Selected(); !ok {
		return false
	}
	return s.src.Environment != s.src.DefaultEnvironment
}

// ExpandedKeys implements cfg.ExpandedKeys.
func (s *envStore) ExpandedKeys() map[string]bool {
	s.load()
	out := map[string]bool{}
	for k := range s.expand[s.src.Environment] {
		out[k] = true
	}
	return out
}

// Values implements cfg.EnvView.
func (s *envStore) Values() map[string]string {
	s.load()
	out := map[string]string{}
	for k, v := range s.byEn[s.src.Environment] {
		out[k] = v
	}
	return out
}

func secretishKey(k string) bool {
	u := strings.ToUpper(k)
	for _, s := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "PWD", "CONNECTIONSTRING", "CREDENTIAL", "SAS", "SIG"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

func digest(v string) string {
	h := sha256.Sum256([]byte("foundry-doctor/envfile/v1\x00" + v))
	return "sha256:" + hex.EncodeToString(h[:8])
}

// readEnvFile parses an azd .env file with the godotenv v1.5.1 grammar
// (project.ParseDotEnv). It returns the status of the file with the values:
// anything other than env.StatusOK comes with an empty map, because a partial
// view would let a comparison pass on missing data. Secret-looking values are
// replaced by a digest after parsing, so variable expansion sees real values.
func readEnvFile(root *os.Root, rel string) (map[string]string, map[string]env.Shape, map[string]bool, env.Status) {
	none := map[string]string{}
	f, err := root.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return none, nil, nil, env.StatusMissing
		}
		return none, nil, nil, env.StatusUnreadable
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return none, nil, nil, env.StatusUnreadable
	}
	if fi.Size() > maxEnvFileBytes {
		return none, nil, nil, env.StatusOversize
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEnvFileBytes+1))
	if err != nil {
		return none, nil, nil, env.StatusUnreadable
	}
	if len(data) > maxEnvFileBytes {
		return none, nil, nil, env.StatusOversize
	}
	parsed, err := project.ParseDotEnv(data)
	if err != nil {
		return none, nil, nil, env.StatusUnparseable
	}
	if len(parsed) > maxEnvKeys {
		return none, nil, nil, env.StatusTruncated
	}
	delete(parsed, "") // godotenv yields an empty key for a last line without separator
	var shapes map[string]env.Shape
	for k, v := range parsed {
		if v != "" && secretishKey(k) {
			// Classify before digesting: a resource ID or bare endpoint URL
			// under a key such as AZURE_KEYVAULT_ID keeps its shape.
			if sh := env.ClassifyShape(v); sh != env.ShapePlain {
				if shapes == nil {
					shapes = map[string]env.Shape{}
				}
				shapes[k] = sh
			}
			parsed[k] = digest(v)
		}
	}
	return parsed, shapes, expandedKeys(data), env.StatusOK
}

// envAssignRe matches the start of a .env statement: optional export, the key
// and the separator, then the raw value text on that line.
var envAssignRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?([A-Za-z0-9_.]+)[ \t]*[=:][ \t]*(.*)$`)

// expandedKeys returns the keys whose last assignment has a raw value that
// can trigger variable expansion in the godotenv grammar: a '$' outside a
// single-quoted value. It scans lines only; a double-quoted value that spans
// lines is judged by its first line, which is where the key and any '$' that
// matters for a short value like AZURE_LOCATION sit.
func expandedKeys(data []byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range envAssignRe.FindAllSubmatch(data, -1) {
		raw := strings.TrimSpace(string(m[2]))
		out[string(m[1])] = strings.Contains(raw, "$") && !strings.HasPrefix(raw, "'")
	}
	for k, v := range out {
		if !v {
			delete(out, k)
		}
	}
	return out
}
