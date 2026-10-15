package ops

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// maxRepoFileBytes caps one pipeline or eval file (256 KiB).
	maxRepoFileBytes = 256 << 10
	// maxRepoFiles caps the number of pipeline files read.
	maxRepoFiles = 100
	maxYAMLDepth = 64
)

// pipelineDirs maps a pipeline directory to the YAML keys whose scalar value is
// a shell command in that CI system. GitHub Actions uses run. Azure Pipelines
// uses the script, bash, pwsh and powershell step shortcuts.
var pipelineDirs = []struct {
	dir  string
	keys []string
}{
	{".github/workflows", []string{"run"}},
	{".azdo/pipelines", []string{"script", "bash", "pwsh", "powershell"}},
	{".azuredevops/pipelines", []string{"script", "bash", "pwsh", "powershell"}},
}

// repoFile is one parsed pipeline definition.
type repoFile struct {
	Path     string
	Commands []string
	// Flat is the document's keys and scalar values in document order with
	// comments removed. It is used for ordering heuristics only.
	Flat string
}

// repoScan is the result of reading pipeline files.
type repoScan struct {
	Files []repoFile
	// Skipped counts files that exist but were not read (symlink, oversize,
	// unreadable, invalid YAML, or beyond the file-count cap). A non-zero value
	// means an empty Files list is not proof that no pipeline exists.
	Skipped int
}

// gitRoot walks up from start to the first directory that contains .git (a
// directory or a file, as in worktrees and submodules). When none is found it
// returns the absolute start directory and false.
func gitRoot(start string) (string, bool) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return start, false
	}
	dir := abs
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, false
		}
		dir = parent
	}
}

// readCapped reads a regular, non-symlink file through root. ok is false when the
// file is absent. skipped is true when it exists but was refused.
func readCapped(root *os.Root, rel string) (body []byte, ok, skipped bool) {
	fi, err := root.Lstat(rel)
	if err != nil {
		return nil, false, !errors.Is(err, fs.ErrNotExist)
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxRepoFileBytes {
		return nil, false, true
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, false, true
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRepoFileBytes+1))
	if err != nil || len(b) > maxRepoFileBytes {
		return nil, false, true
	}
	return b, true, false
}

// dirSafe reports whether every component of rel is a real directory inside root.
func dirSafe(root *os.Root, rel string) (exists, safe bool) {
	cur := ""
	for _, part := range strings.Split(rel, "/") {
		cur = path.Join(cur, part)
		fi, err := root.Lstat(cur)
		if err != nil {
			return false, true
		}
		if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
			return true, false
		}
	}
	return true, true
}

// scanPipelines reads .yml/.yaml pipeline files below base. Reads go through
// os.Root, symlinks are skipped, and size and count are capped.
func scanPipelines(base string) repoScan {
	var scan repoScan
	root, err := os.OpenRoot(base)
	if err != nil {
		return scan
	}
	defer root.Close()
	count := 0
	for _, pd := range pipelineDirs {
		exists, safe := dirSafe(root, pd.dir)
		if !exists {
			continue
		}
		if !safe {
			scan.Skipped++
			continue
		}
		d, err := root.Open(pd.dir)
		if err != nil {
			scan.Skipped++
			continue
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			scan.Skipped++
			continue
		}
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, e := range entries {
			ext := strings.ToLower(path.Ext(e.Name()))
			if ext != ".yml" && ext != ".yaml" {
				continue
			}
			rel := pd.dir + "/" + e.Name()
			if count >= maxRepoFiles {
				scan.Skipped++
				continue
			}
			count++
			body, ok, skipped := readCapped(root, rel)
			if !ok {
				if skipped {
					scan.Skipped++
				}
				continue
			}
			var n yaml.Node
			if err := yaml.Unmarshal(body, &n); err != nil {
				scan.Skipped++
				continue
			}
			rf := repoFile{Path: rel}
			var flat strings.Builder
			walkYAML(&n, pd.keys, &rf.Commands, &flat, 0)
			rf.Flat = strings.ToLower(flat.String())
			scan.Files = append(scan.Files, rf)
		}
	}
	return scan
}

// walkYAML collects command scalars (values of the given keys) and a flat text.
// Aliases are not followed.
func walkYAML(n *yaml.Node, keys []string, cmds *[]string, flat *strings.Builder, depth int) {
	if n == nil || depth > maxYAMLDepth {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			walkYAML(c, keys, cmds, flat, depth+1)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			flat.WriteString(k.Value)
			flat.WriteByte('\n')
			if v.Kind == yaml.ScalarNode && slices.Contains(keys, k.Value) {
				*cmds = append(*cmds, v.Value)
			}
			walkYAML(v, keys, cmds, flat, depth+1)
		}
	case yaml.ScalarNode:
		flat.WriteString(n.Value)
		flat.WriteByte('\n')
	}
}

func matchLines(cmd string, re *regexp.Regexp) bool {
	for _, line := range strings.Split(cmd, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// readRootFile reads one file below base through os.Root, refusing symlinks and
// oversize files.
func readRootFile(base, rel string) (body []byte, ok, skipped bool) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, false, false
	}
	defer root.Close()
	return readCapped(root, rel)
}
