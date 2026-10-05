package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// confined wraps an os.Root. os.Root resolves every component inside the root and refuses
// symlinks, ".." and absolute names that would leave it, so a hostile repository cannot make the
// doctor read files outside the project.
type confined struct {
	root *os.Root
}

func openConfined(dir string) (*confined, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", sanitizeErr(err))
	}
	return &confined{root: r}, nil
}

func (c *confined) close() error { return c.root.Close() }

// mapErr converts os.Root failures into the package sentinels. It keeps the underlying error
// text only for fs.PathError values, whose paths are root-relative.
func mapErr(op, rel string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ELOOP):
		return fmt.Errorf("%s %s: %w", op, rel, ErrSymlinkLoop)
	case strings.Contains(err.Error(), "escapes from parent"):
		return fmt.Errorf("%s %s: %w", op, rel, ErrSymlinkEscape)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s %s: %w", op, rel, fs.ErrNotExist)
	default:
		return fmt.Errorf("%s %s: %w", op, rel, sanitizeErr(err))
	}
}

// sanitizeErr drops absolute paths that *fs.PathError carries, keeping the cause.
func sanitizeErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// stat validates rel and stats it, following links inside the root.
func (c *confined) stat(rel string) (string, fs.FileInfo, error) {
	clean, err := ValidateRel(rel)
	if err != nil {
		return "", nil, err
	}
	fi, err := c.root.Stat(clean)
	if err != nil {
		return clean, nil, mapErr("stat", clean, err)
	}
	return clean, fi, nil
}

// readFile reads a regular file of at most max bytes.
func (c *confined) readFile(ctx context.Context, rel string, max int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, fi, err := c.stat(rel)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: %w", clean, ErrNotRegular)
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("read %s: %w", clean, ErrTooLarge)
	}
	f, err := c.root.Open(clean)
	if err != nil {
		return nil, mapErr("open", clean, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, mapErr("read", clean, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("read %s: %w", clean, ErrTooLarge)
	}
	return data, nil
}

// readDir lists a directory inside the root.
func (c *confined) readDir(ctx context.Context, rel string) ([]fs.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := ValidateRel(rel)
	if err != nil {
		return nil, err
	}
	d, err := c.root.Open(clean)
	if err != nil {
		return nil, mapErr("open", clean, err)
	}
	defer d.Close()
	ents, err := d.ReadDir(-1)
	if err != nil {
		return nil, mapErr("readdir", clean, err)
	}
	return ents, nil
}
