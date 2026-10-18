package annotate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrUnsafeOutput marks an --out value that is refused: outside the project,
// through a symbolic link or reparse point, overlapping a deployable input or
// an existing directory the tool does not own.
var ErrUnsafeOutput = errors.New("unsafe annotate output")

const (
	toolName = "foundry-doctor"
	// markerName marks a directory as a generated review copy, so a rerun
	// only ever deletes a tree the tool created.
	markerName = ".foundry-doctor-review"
	markerText = "This directory is a generated Foundry Doctor review copy (.review). Do not deploy it.\n"

	reviewDirPerm  = 0o750
	manifestPerm   = 0o640
	diffPerm       = 0o600
	markerPerm     = 0o640
	outputFilePerm = 0o644
)

// foldCase reports whether the default file systems of the host are
// case-insensitive (NTFS, APFS), so lexical path checks compare case-folded.
func foldCase() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func pathsOverlap(a, b string) bool {
	a = path.Clean(filepath.ToSlash(a))
	b = path.Clean(filepath.ToSlash(b))
	if a == "." || b == "." {
		return true
	}
	if foldCase() {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func validateOutputPlacement(outRel string, inputs ...string) error {
	for _, in := range inputs {
		if strings.TrimSpace(in) == "" {
			continue
		}
		if pathsOverlap(outRel, in) {
			return fmt.Errorf("%w: --out must not overlap deployable source inputs (%s)", ErrUnsafeOutput, path.Clean(filepath.ToSlash(in)))
		}
	}
	return nil
}

// ensureNoSymlinkComponents refuses an output path whose existing components
// include a symbolic link, a Windows junction or other reparse point (reported
// as ModeIrregular since Go 1.23), or a non-directory parent.
func ensureNoSymlinkComponents(root *os.Root, rel string) error {
	current := ""
	for _, component := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("stat output path %s: %w", current, err)
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return fmt.Errorf("%w: output must not traverse symlinks or reparse points: %s", ErrUnsafeOutput, current)
		}
		if !info.IsDir() && current != rel {
			return fmt.Errorf("%w: output parent is not a directory: %s", ErrUnsafeOutput, current)
		}
	}
	return nil
}

// statChain stats every existing prefix of rel through root, stopping at the
// first missing component. full reports whether rel itself exists.
func statChain(root *os.Root, rel string) (infos []fs.FileInfo, full bool) {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == "." {
		info, err := root.Stat(".")
		if err != nil {
			return nil, false
		}
		return []fs.FileInfo{info}, true
	}
	current := ""
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		current = path.Join(current, part)
		info, err := root.Stat(current)
		if err != nil {
			return infos, false
		}
		infos = append(infos, info)
		if i == len(parts)-1 {
			full = true
		}
	}
	return infos, full
}

// checkIdentityOverlap decides overlap by file identity rather than by
// string, so case aliases, hard links and in-root aliases are caught: the
// output (or an existing ancestor) must not be an input, and an input (or one
// of its ancestors) must not be the output.
func checkIdentityOverlap(root *os.Root, out string, inputs []string) error {
	outChain, outFull := statChain(root, out)
	for _, in := range inputs {
		if strings.TrimSpace(in) == "" {
			continue
		}
		inChain, inFull := statChain(root, in)
		if inFull {
			inInfo := inChain[len(inChain)-1]
			for _, oi := range outChain {
				if os.SameFile(oi, inInfo) {
					return fmt.Errorf("%w: --out resolves to or under deployable input %s", ErrUnsafeOutput, in)
				}
			}
		}
		if outFull {
			outInfo := outChain[len(outChain)-1]
			for _, ii := range inChain {
				if os.SameFile(outInfo, ii) {
					return fmt.Errorf("%w: deployable input %s is inside --out", ErrUnsafeOutput, in)
				}
			}
		}
	}
	return nil
}

// writeNewFile creates rel exclusively. O_EXCL refuses an existing path,
// including a symbolic link, so the write never follows a planted link.
func writeNewFile(root *os.Root, rel string, data []byte, perm fs.FileMode) error {
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WriteOutputFile writes a github or sarif feed to out (a project-relative,
// slash-separated path) under rootDir. It refuses a target that is, or is
// reached through, a symbolic link or reparse point, a target that is not a
// regular file, and any target that is the same file as, or lies inside, one
// of inputs.
func WriteOutputFile(rootDir, out string, inputs []string, data []byte) error {
	out = path.Clean(filepath.ToSlash(out))
	if out == "." || strings.HasPrefix(out, "/") || !filepath.IsLocal(filepath.FromSlash(out)) {
		return fmt.Errorf("%w: --out must stay inside the project directory", ErrUnsafeOutput)
	}
	if err := validateOutputPlacement(out, inputs...); err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	if err := ensureNoSymlinkComponents(root, out); err != nil {
		return err
	}
	if err := checkIdentityOverlap(root, out, inputs); err != nil {
		return err
	}
	info, err := root.Lstat(out)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: --out %s exists and is not a regular file", ErrUnsafeOutput, out)
		}
		if err := root.Remove(out); err != nil {
			return fmt.Errorf("replace --out %q: %w", out, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("stat --out %q: %w", out, err)
	}
	if err := root.MkdirAll(path.Dir(out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeNewFile(root, out, data, outputFilePerm); err != nil {
		return fmt.Errorf("write --out %q: %w", out, err)
	}
	return nil
}

// ownedReviewDir reports whether the existing directory rel may be replaced:
// it is empty, carries the review marker, or holds a manifest written by this
// tool.
func ownedReviewDir(root *os.Root, rel string) (bool, error) {
	entries, err := fs.ReadDir(root.FS(), rel)
	if err != nil {
		return false, fmt.Errorf("read review directory %s: %w", rel, err)
	}
	if len(entries) == 0 {
		return true, nil
	}
	if info, err := root.Lstat(path.Join(rel, markerName)); err == nil && info.Mode().IsRegular() {
		return true, nil
	}
	manifest := path.Join(rel, manifestName)
	if info, err := root.Lstat(manifest); err == nil && info.Mode().IsRegular() {
		data, err := root.ReadFile(manifest)
		if err == nil {
			var m struct {
				Tool string `json:"tool"`
			}
			if json.Unmarshal(data, &m) == nil && m.Tool == toolName {
				return true, nil
			}
		}
	}
	return false, nil
}

// resetReviewDir replaces outRel with an empty, marked review directory. An
// existing directory is deleted only when ownedReviewDir accepts it.
func resetReviewDir(root *os.Root, outRel string) error {
	info, err := root.Lstat(outRel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("stat review directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("%w: %s exists and is not a directory", ErrUnsafeOutput, outRel)
	default:
		owned, err := ownedReviewDir(root, outRel)
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("%w: %s exists and is not a Foundry Doctor review copy; remove it or choose another --out", ErrUnsafeOutput, outRel)
		}
		if err := root.RemoveAll(outRel); err != nil {
			return fmt.Errorf("reset review directory: %w", err)
		}
	}
	if err := root.MkdirAll(outRel, reviewDirPerm); err != nil {
		return fmt.Errorf("create review directory: %w", err)
	}
	if err := writeNewFile(root, path.Join(outRel, markerName), []byte(markerText), markerPerm); err != nil {
		return fmt.Errorf("write review marker: %w", err)
	}
	return nil
}

// RemoveReview deletes a review copy written by WriteReview. It refuses a
// path that is unsafe or a directory the tool does not own.
func RemoveReview(rootDir, outDir string) error {
	outRel, err := safeOutputPath(outDir)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	if err := ensureNoSymlinkComponents(root, outRel); err != nil {
		return err
	}
	info, err := root.Lstat(outRel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat review directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeOutput, outRel)
	}
	owned, err := ownedReviewDir(root, outRel)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("%w: %s is not a Foundry Doctor review copy", ErrUnsafeOutput, outRel)
	}
	return root.RemoveAll(outRel)
}
