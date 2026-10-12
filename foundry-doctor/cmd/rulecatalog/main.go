// Command rulecatalog validates the rule catalogue and generates documents from it.
//
//	rulecatalog validate         [--dir rules/catalog] [--phase0]
//	rulecatalog generate-docs    [--dir rules/catalog] [--out docs/rule-catalog.md]     [--check]
//	rulecatalog generate-overlap [--dir rules/catalog] [--out docs/overlap-analysis.md] [--check]
//
// Exit codes: 0 ok; 1 the catalogue is invalid or a generated file is stale;
// 2 utility/usage error or an I/O failure, including cancellation and writer failures.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

const usage = "usage: rulecatalog validate|generate-docs|generate-overlap [flags]"

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	outWriter, errWriter := &trackingWriter{w: stdout}, &trackingWriter{w: stderr}
	stdout, stderr = outWriter, errWriter
	defer func() {
		if outWriter.err != nil || errWriter.err != nil {
			code = 2
		}
	}()
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cmd := args[0]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "validate", "generate-docs", "generate-overlap":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", cmd, usage)
		return 2
	}

	fl := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fl.SetOutput(stderr)
	dir := fl.String("dir", "rules/catalog", "catalogue directory")
	phase0 := fl.Bool("phase0", false, "validate only: fail rules still in the proposed state")
	defaultOut := map[string]string{"generate-docs": "docs/rule-catalog.md", "generate-overlap": "docs/overlap-analysis.md"}[cmd]
	out := fl.String("out", defaultOut, "generate-* only: output file")
	check := fl.Bool("check", false, "generate-* only: fail if the output file is not up to date, without writing")
	if err := fl.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fl.Args())
		return 2
	}
	set := map[string]bool{}
	fl.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if cmd == "validate" && (set["out"] || set["check"]) || cmd != "validate" && set["phase0"] {
		fmt.Fprintf(stderr, "flag not valid for %s\n", cmd)
		return 2
	}

	root, err := repositoryRoot(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	repo, err := os.OpenRoot(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer repo.Close()
	goMod, err := readRootFile(repo, "go.mod")
	if err != nil || !isFoundryDoctorModule(string(goMod)) {
		fmt.Fprintln(stderr, "foundry-doctor repository root changed while opening")
		return 2
	}
	catalogueDir, err := confinedName(root, *dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var outputPath string
	if cmd != "validate" {
		outputPath, err = confinedName(root, *out)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	rules, err := catalog.Load(ctx, rootFS{Root: repo}, filepath.ToSlash(catalogueDir))
	if err != nil {
		fmt.Fprintln(stderr, err)
		var inv *catalog.InvalidError
		if errors.As(err, &inv) {
			return 1
		}
		return 2
	}
	opt := catalog.Options{Phase0Gate: cmd == "validate" && *phase0}
	if err := catalog.Validate(rules, opt); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if cmd == "validate" {
		fmt.Fprintf(stdout, "catalogue valid: %d rules\n", len(rules))
		return 0
	}

	doc := catalog.Markdown(rules)
	if cmd == "generate-overlap" {
		doc = catalog.OverlapMatrix(rules)
	}
	if *check {
		cur, err := readRootFile(repo, outputPath)
		if err == nil {
			err = ctx.Err()
		}
		switch {
		case errors.Is(err, fs.ErrNotExist) || err == nil && string(cur) != doc:
			fmt.Fprintf(stderr, "%s is stale; run: go run ./cmd/rulecatalog %s\n", *out, cmd)
			return 1
		case err != nil:
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := writeRootFileAtomic(repo, outputPath, []byte(doc)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "wrote %s (%d rules)\n", *out, len(rules))
	return 0
}

type trackingWriter struct {
	w   io.Writer
	err error
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func repositoryRoot(pathHint string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	starts := []string{cwd}
	if filepath.IsAbs(pathHint) {
		starts = append(starts, pathHint)
	}
	for _, start := range starts {
		if root, err := findRepositoryRoot(start); err == nil {
			return root, nil
		}
	}
	return "", errors.New("foundry-doctor repository root not found")
}

func findRepositoryRoot(dir string) (string, error) {
	for {
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			dir = filepath.Dir(dir)
		}
		mod := filepath.Join(dir, "go.mod")
		if b, readErr := os.ReadFile(mod); readErr == nil {
			if isFoundryDoctorModule(string(b)) {
				return dir, nil
			}
		} else if !errors.Is(readErr, fs.ErrNotExist) {
			return "", readErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fs.ErrNotExist
		}
		dir = parent
	}
}

func isFoundryDoctorModule(goMod string) bool {
	const module = "github.com/ruairispain/copilot-web/foundry-doctor"
	for _, line := range strings.Split(goMod, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) == module
		}
	}
	return false
}

func confinedName(root, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("path must not be empty")
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside repository root", name)
	}
	return rel, nil
}

func readRootFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

type rootFS struct {
	*os.Root
}

func (r rootFS) Open(name string) (fs.File, error) {
	return r.Root.Open(name)
}

// writeRootFileAtomic keeps directory creation, temporary-file creation and
// rename relative to the same rooted directory handle. os.Root rejects any
// symlink or reparse-point swap that would escape that handle.
func writeRootFileAtomic(root *os.Root, name string, data []byte) error {
	return writeRootFileAtomicWithRename(root, name, data, func(oldName, newName string) error {
		return root.Rename(oldName, newName)
	})
}

func writeRootFileAtomicWithRename(root *os.Root, name string, data []byte, rename func(string, string) error) error {
	d := filepath.Dir(name)
	if err := mkdirAllRoot(root, d, 0o755); err != nil {
		return err
	}
	var tmp *os.File
	var tmpName string
	for range 100 {
		var nonce [12]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		tmpName = filepath.Join(d, ".rulecatalog-"+hex.EncodeToString(nonce[:]))
		var err error
		tmp, err = root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	if tmp == nil {
		return errors.New("could not create unique temporary output")
	}
	defer root.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := rename(tmpName, name); err != nil {
		return err
	}
	if dir, err := root.Open(d); err == nil {
		defer dir.Close()
		// Some platforms do not support syncing directories. A successful
		// atomic rename remains valid there.
		_ = dir.Sync()
	}
	return nil
}

func mkdirAllRoot(root *os.Root, name string, perm fs.FileMode) error {
	if name == "." || name == "" {
		return nil
	}
	current := ""
	for _, component := range strings.FieldsFunc(filepath.Clean(name), func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		current = filepath.Join(current, component)
		err := root.Mkdir(current, perm)
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, statErr := root.Stat(current)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			return &fs.PathError{Op: "mkdir", Path: current, Err: errors.New("not a directory")}
		}
	}
	return nil
}
