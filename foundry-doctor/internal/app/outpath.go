package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsafeOutput is returned (wrapped) for an output path that is refused.
var ErrUnsafeOutput = errors.New("unsafe output path")

// writeOutput writes data to target.
//
//   - A relative target is resolved against base (the working directory) and must stay inside root,
//     the project directory. Missing parent directories are created, but only inside root.
//   - An absolute target is accepted only when given explicitly (it always is: the caller never builds one).
//     Inside root it behaves like a relative one. Outside root its parent directory must already exist;
//     no directory is created there.
//   - A symbolic link in any existing component below root (or, outside root, anywhere on the path) is
//     refused, and so is a final component that is a symbolic link or not a regular file.
//
// The file is opened through os.Root, so a link swapped in after the check cannot lead out either.
func writeOutput(root, base, target string, data []byte) error {
	if strings.TrimSpace(target) == "" || strings.ContainsRune(target, 0) {
		return fmt.Errorf("%w: empty or invalid path", ErrUnsafeOutput)
	}
	explicitAbs := filepath.IsAbs(target)
	abs := target
	if !explicitAbs {
		abs = filepath.Join(base, target)
	}
	abs = filepath.Clean(abs)
	root = filepath.Clean(root)

	rel, inside := within(root, abs)
	switch {
	case inside:
		return writeInside(root, rel, data)
	case !explicitAbs:
		return fmt.Errorf("%w: %q leaves the project directory; give an absolute path to write elsewhere", ErrUnsafeOutput, target)
	default:
		return writeOutside(abs, data)
	}
}

func within(root, abs string) (string, bool) {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// checkNoLinks Lstat-walks the segments of rel below dir. A missing component ends the walk (it will be created
// or reported by the open); an existing symlink, or a non-directory before the last segment, is refused.
func checkNoLinks(dir, rel string) error {
	cur := dir
	segs := strings.Split(rel, string(filepath.Separator))
	for i, s := range segs {
		cur = filepath.Join(cur, s)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsafeOutput, pathErr(err))
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is a symbolic link", ErrUnsafeOutput, filepath.ToSlash(strings.Join(segs[:i+1], "/")))
		}
		if i < len(segs)-1 && !fi.IsDir() {
			return fmt.Errorf("%w: %q is not a directory", ErrUnsafeOutput, filepath.ToSlash(strings.Join(segs[:i+1], "/")))
		}
		if i == len(segs)-1 && !fi.Mode().IsRegular() {
			return fmt.Errorf("%w: %q is not a regular file", ErrUnsafeOutput, filepath.ToSlash(rel))
		}
	}
	return nil
}

func pathErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func writeInside(root, rel string, data []byte) error {
	if err := checkNoLinks(root, rel); err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open project directory: %w", pathErr(err))
	}
	defer r.Close()
	if dir := filepath.Dir(rel); dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory %q: %w", filepath.ToSlash(dir), pathErr(err))
		}
	}
	return openAndWrite(r, rel, data)
}

func writeOutside(abs string, data []byte) error {
	vol := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs[len(vol):], string(filepath.Separator))
	if err := checkNoLinks(vol+string(filepath.Separator), rest); err != nil {
		return err
	}
	dir, name := filepath.Split(abs)
	if name == "" {
		return fmt.Errorf("%w: %q has no file name", ErrUnsafeOutput, abs)
	}
	r, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("%w: the parent directory must exist (directories are created only inside the project): %v", ErrUnsafeOutput, pathErr(err))
	}
	defer r.Close()
	return openAndWrite(r, name, data)
}

func openAndWrite(r *os.Root, rel string, data []byte) (err error) {
	f, err := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write %q: %w", filepath.ToSlash(rel), pathErr(err))
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close %q: %w", filepath.ToSlash(rel), cerr)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %q: %w", filepath.ToSlash(rel), err)
	}
	return nil
}
