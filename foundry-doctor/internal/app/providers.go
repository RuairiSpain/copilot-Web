package app

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
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

	once sync.Once
	envs []env.Environment
	byEn map[string]map[string]string
}

func (s *envStore) load() {
	s.once.Do(func() {
		s.byEn = map[string]map[string]string{}
		if s.src.Dir == "" {
			return
		}
		root, err := os.OpenRoot(s.src.Dir)
		if err != nil {
			return
		}
		defer root.Close()
		names := slices.Clone(s.src.Environments)
		slices.Sort(names)
		literalRG := false
		if s.view != nil {
			if v, _, ok := s.view.Lookup("resourceGroup"); ok {
				literalRG = v != "" && !strings.Contains(v, "${")
			}
		}
		for _, n := range names {
			vals := readEnvFile(root, path.Join(project.AzureDir, n, ".env"))
			s.byEn[n] = vals
			s.envs = append(s.envs, env.Environment{
				Name: n, Subscription: vals["AZURE_SUBSCRIPTION_ID"], ResourceGroup: vals["AZURE_RESOURCE_GROUP"],
				LiteralRGInYAML: literalRG, Values: vals,
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
	if _, ok := s.byEn[s.src.Environment]; !ok {
		return "", false
	}
	return s.src.Environment, true
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

// readEnvFile parses KEY=VALUE lines. It returns an empty map when the file is
// missing, unreadable, not regular or too large.
func readEnvFile(root *os.Root, rel string) map[string]string {
	out := map[string]string{}
	f, err := root.Open(rel)
	if err != nil {
		return out
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() > maxEnvFileBytes {
		return out
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEnvFileBytes+1))
	if err != nil || len(data) > maxEnvFileBytes {
		return out
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() && len(out) < maxEnvKeys {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "\ufeff"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if v != "" && secretishKey(k) {
			v = digest(v)
		}
		out[k] = v
	}
	return out
}

var _ = errors.Is
var _ = fs.ErrNotExist
