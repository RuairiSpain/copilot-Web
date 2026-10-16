package baseline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrExists is returned by WriteFile when the target exists and force is false.
var ErrExists = errors.New("baseline: file already exists (use --force to overwrite)")

// ErrUnsafePath is returned when the target is absolute, escapes the project
// directory, or passes through a symbolic link.
var ErrUnsafePath = errors.New("baseline: unsafe baseline path")

// WriteFile writes the baseline to rel, a path relative to the project
// directory dir. The write is confined to dir with os.Root and never follows
// a symbolic link: any component of rel that is a link, and a link at the
// target itself, is refused. An existing regular file is replaced only when
// force is true. The content is Marshal output, so it is deterministic and
// holds fingerprints and rule IDs only (no evidence text, no paths).
func WriteFile(dir, rel string, f *File, force bool) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return fmt.Errorf("%w: %q must be relative to the project directory", ErrUnsafePath, rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %q escapes the project directory", ErrUnsafePath, rel)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("baseline: open project directory: %w", err)
	}
	defer root.Close()

	parts := strings.Split(clean, string(filepath.Separator))
	cur := ""
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := root.Lstat(cur)
		last := i == len(parts)-1
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if !last {
				if err := root.Mkdir(cur, 0o755); err != nil {
					return fmt.Errorf("baseline: create %s: %w", filepath.ToSlash(cur), err)
				}
			}
		case err != nil:
			return fmt.Errorf("baseline: %w", err)
		case fi.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("%w: %s is a symbolic link", ErrUnsafePath, filepath.ToSlash(cur))
		case last && !fi.Mode().IsRegular():
			return fmt.Errorf("%w: %s is not a regular file", ErrUnsafePath, filepath.ToSlash(cur))
		case last && !force:
			return fmt.Errorf("%w: %s", ErrExists, filepath.ToSlash(cur))
		case !last && !fi.IsDir():
			return fmt.Errorf("%w: %s is not a directory", ErrUnsafePath, filepath.ToSlash(cur))
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	out, err := root.OpenFile(clean, flags, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, filepath.ToSlash(clean))
		}
		return fmt.Errorf("baseline: write %s: %w", filepath.ToSlash(clean), err)
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		return fmt.Errorf("baseline: write %s: %w", filepath.ToSlash(clean), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("baseline: write %s: %w", filepath.ToSlash(clean), err)
	}
	return nil
}
