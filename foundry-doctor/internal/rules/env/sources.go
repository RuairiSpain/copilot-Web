package env

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Bounds for parameter files. Larger or more numerous inputs are not
// partially trusted.
const (
	maxParamFileBytes = 256 << 10
	maxParamEnvs      = 50
	maxParamsPerFile  = 2000
	maxInfraListing   = 500
)

// StatusNotRead marks an environment source that is recognised but not read
// (a .bicepparam file needs the Bicep CLI; the doctor has no Bicep parser).
const StatusNotRead Status = "not read (needs the Bicep CLI)"

var (
	// envNameRe is the azd environment name expression (verified in the azd
	// source, see the rule catalogue).
	envNameRe = regexp.MustCompile(`^[a-zA-Z0-9\-()_.]{1,64}$`)

	// Environment-named parameter files next to the entry template. UNVERIFIED
	// as a convention: azd reads only <module>.parameters.json and
	// <module>.bicepparam (bicep_provider.go) and defines no per-environment
	// file naming, so these names are inferences and their environments are
	// Origin parameter-file (notes, never findings).
	paramFilePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^main\.parameters\.([a-zA-Z0-9\-()_]{1,64})\.json$`),
		regexp.MustCompile(`(?i)^main\.([a-zA-Z0-9\-()_]{1,64})\.parameters\.json$`),
	}
	bicepparamPattern = regexp.MustCompile(`(?i)^main\.([a-zA-Z0-9\-()_]{1,64})\.bicepparam$`)
)

// ParameterFileEnvironments discovers environments from environment-named
// parameter files in infraDir (repo-relative, slash-separated) under root. It
// is read-only, never follows a symlink out of root, and keeps only
// non-secret, identifier-shaped values (ARM resource IDs and Azure endpoint
// URLs); no other parameter value is retained. Parameter values that are
// ${VAR} placeholders, ARM expressions, Key Vault references or objects are
// ignored.
func ParameterFileEnvironments(root *os.Root, infraDir string) []Environment {
	if root == nil {
		return nil
	}
	dir, err := root.Open(infraDir)
	if err != nil {
		return nil
	}
	entries, err := dir.ReadDir(maxInfraListing)
	_ = dir.Close()
	if err != nil && err != io.EOF {
		return nil
	}
	type found struct {
		name, file string
		bicep      bool
	}
	var fs []found
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue // directories, symlinks and devices are never read
		}
		n := e.Name()
		for _, re := range paramFilePatterns {
			if m := re.FindStringSubmatch(n); m != nil {
				fs = append(fs, found{name: m[1], file: n})
			}
		}
		if m := bicepparamPattern.FindStringSubmatch(n); m != nil {
			fs = append(fs, found{name: m[1], file: n, bicep: true})
		}
	}
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].name != fs[j].name {
			return fs[i].name < fs[j].name
		}
		return fs[i].file < fs[j].file
	})
	var out []Environment
	seen := map[string]bool{}
	for _, f := range fs {
		key := strings.ToLower(f.name)
		if seen[key] || !envNameRe.MatchString(f.name) {
			continue // the first file (in name order) wins; the rest are not merged
		}
		seen[key] = true
		if len(out) >= maxParamEnvs {
			break
		}
		e := Environment{Name: f.name, Origin: OriginParameterFile, Values: map[string]string{}}
		switch {
		case f.bicep:
			e.Status = StatusNotRead
		default:
			vals, st := readParameterFile(root, path.Join(infraDir, f.file))
			e.Status = st
			if st == StatusOK {
				e.Values = vals
				e.Values["AZURE_ENV_NAME"] = f.name
			}
		}
		out = append(out, e)
	}
	return out
}

// readParameterFile parses an ARM parameters file and returns only the
// identifier-shaped string values, keyed by parameter name.
func readParameterFile(root *os.Root, rel string) (map[string]string, Status) {
	fi, err := root.Lstat(rel)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, StatusUnreadable
	}
	if fi.Size() > maxParamFileBytes {
		return nil, StatusOversize
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, StatusUnreadable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxParamFileBytes+1))
	if err != nil {
		return nil, StatusUnreadable
	}
	if len(data) > maxParamFileBytes {
		return nil, StatusOversize
	}
	var doc struct {
		Parameters map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, StatusUnparseable
	}
	if len(doc.Parameters) > maxParamsPerFile {
		return nil, StatusTruncated
	}
	out := map[string]string{}
	for name, p := range doc.Parameters {
		var v string
		if json.Unmarshal(p.Value, &v) != nil {
			continue // not a string: an object (for example a Key Vault reference), number, array or null
		}
		v = strings.TrimSpace(v)
		if v == "" || strings.Contains(v, "${") || strings.HasPrefix(v, "[") || ClassifyShape(v) == ShapePlain {
			continue
		}
		out[name] = v
	}
	return out, StatusOK
}

type withParameterFiles struct {
	base     Provider
	dir      string
	infraDir string
}

// WithParameterFiles returns a Provider that adds, to the environments of
// base, the environments named by environment-specific parameter files under
// infraDir of the project directory dir. An environment that base already
// lists (any status) is not added again. With an empty dir it is base.
func WithParameterFiles(base Provider, dir, infraDir string) Provider {
	if base == nil || dir == "" {
		return base
	}
	return withParameterFiles{base: base, dir: dir, infraDir: infraDir}
}

func (w withParameterFiles) Environments() []Environment {
	envs := w.base.Environments()
	root, err := os.OpenRoot(w.dir)
	if err != nil {
		return envs
	}
	defer root.Close()
	have := map[string]bool{}
	for _, e := range envs {
		have[strings.ToLower(e.Name)] = true
	}
	for _, e := range ParameterFileEnvironments(root, w.infraDir) {
		if !have[strings.ToLower(e.Name)] {
			envs = append(envs, e)
		}
	}
	return envs
}
