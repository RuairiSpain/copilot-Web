package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Limits on untrusted input. A real config is a few KiB.
const (
	// MaxFileBytes is the largest config file that is read.
	MaxFileBytes = 256 << 10
	maxDepth     = 24
	maxNodes     = 20000
)

// Load reads and validates the config file at path. It refuses symlinks, non-regular files and files over
// MaxFileBytes. A missing file returns an error for which errors.Is(err, fs.ErrNotExist) is true; callers decide
// whether that is normal (see LoadOptional). Every other failure is exit 2.
func Load(ctx context.Context, path string) (*File, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	li, err := os.Lstat(path)
	if err != nil {
		return nil, &ioError{file: path, msg: "cannot stat", err: err}
	}
	if li.Mode()&fs.ModeSymlink != 0 {
		return nil, &ioError{file: path, msg: "symlinks are not allowed for the config file"}
	}
	if !li.Mode().IsRegular() {
		return nil, &ioError{file: path, msg: "not a regular file"}
	}
	if li.Size() > MaxFileBytes {
		return nil, &ioError{file: path, msg: fmt.Sprintf("file is larger than %d bytes", MaxFileBytes)}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, &ioError{file: path, msg: "cannot open", err: err}
	}
	defer f.Close()
	// Close the Lstat/Open race: the opened file must be the one that was checked.
	if oi, err := f.Stat(); err != nil || !os.SameFile(li, oi) {
		return nil, &ioError{file: path, msg: "file changed while opening"}
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, &ioError{file: path, msg: "cannot read", err: err}
	}
	return Parse(path, data)
}

// LoadOptional is Load, except that a missing file yields (nil, nil): absence of the config is normal.
func LoadOptional(ctx context.Context, path string) (*File, error) {
	f, err := Load(ctx, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// Parse decodes and validates config bytes. name is used in error messages only.
//
// The document must be a single YAML document without anchors, aliases, merge keys or explicit tags.
// Unknown keys and duplicate keys fail, naming the key path and line. All problems found are returned together
// as an *InvalidError.
func Parse(name string, data []byte) (*File, error) {
	if len(data) > MaxFileBytes {
		return nil, &ioError{file: name, msg: fmt.Sprintf("file is larger than %d bytes", MaxFileBytes)}
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, &Error{File: name, Msg: "file contains a NUL byte"}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &Error{File: name, Msg: "file is empty; version is required"}
		}
		return nil, yamlError(name, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, &Error{File: name, Line: extra.Line, Msg: "more than one YAML document in the config file"}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, &Error{File: name, Msg: "not a YAML document"}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, &Error{File: name, Line: root.Line, Msg: "top level must be a mapping of config keys"}
	}

	w := &walker{file: name, lines: map[string]int{"": root.Line}}
	w.walk(root, reflect.TypeFor[File](), "", 0)
	if len(w.problems) > 0 {
		return nil, newInvalid(w.problems)
	}

	var f File
	if err := root.Decode(&f); err != nil {
		return nil, yamlError(name, err)
	}
	f.name = name
	f.lines = w.lines
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

var (
	yamlLineRe    = regexp.MustCompile(`^line (\d+): (.*)$`)
	yamlTopLineRe = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)
)

// yamlError converts a yaml decode error into Error values with lines.
func yamlError(name string, err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		var ps []*Error
		for _, m := range te.Errors {
			e := &Error{File: name, Msg: m}
			if sm := yamlLineRe.FindStringSubmatch(m); sm != nil {
				e.Line, _ = strconv.Atoi(sm[1])
				e.Msg = sm[2]
			}
			ps = append(ps, e)
		}
		return newInvalid(ps)
	}
	msg := err.Error()
	e := &Error{File: name, Msg: strings.TrimPrefix(msg, "yaml: ")}
	if sm := yamlTopLineRe.FindStringSubmatch(msg); sm != nil {
		e.Line, _ = strconv.Atoi(sm[1])
		e.Msg = sm[2]
	}
	return e
}

// walker checks a node tree against the Go types: unknown keys, duplicate keys, anchors, aliases, tags,
// depth and size. It also records the line of every key path for later value validation.
type walker struct {
	file     string
	lines    map[string]int
	nodes    int
	problems []*Error
	stop     bool
}

func (w *walker) add(line int, path, format string, a ...any) {
	if len(w.problems) >= 50 {
		w.stop = true
		return
	}
	w.problems = append(w.problems, &Error{File: w.file, Line: line, Path: path, Msg: fmt.Sprintf(format, a...)})
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (w *walker) walk(n *yaml.Node, t reflect.Type, path string, depth int) {
	if w.stop {
		return
	}
	w.nodes++
	switch {
	case w.nodes > maxNodes:
		w.add(n.Line, path, "document has more than %d nodes", maxNodes)
		w.stop = true
		return
	case depth > maxDepth:
		w.add(n.Line, path, "nesting deeper than %d levels", maxDepth)
		w.stop = true
		return
	case n.Kind == yaml.AliasNode:
		w.add(n.Line, path, "aliases are not supported in the config file")
		return
	case n.Anchor != "":
		w.add(n.Line, path, "anchors are not supported in the config file")
		return
	case n.Style&yaml.TaggedStyle != 0:
		w.add(n.Line, path, "explicit YAML tags are not supported in the config file")
		return
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch n.Kind {
	case yaml.MappingNode:
		w.walkMapping(n, t, path, depth)
	case yaml.SequenceNode:
		var elem reflect.Type
		if t != nil && t.Kind() == reflect.Slice {
			elem = t.Elem()
		}
		for i, c := range n.Content {
			p := path + "[" + strconv.Itoa(i) + "]"
			w.lines[p] = c.Line
			w.walk(c, elem, p, depth+1)
		}
	}
}

func (w *walker) walkMapping(n *yaml.Node, t reflect.Type, path string, depth int) {
	var fields map[string]reflect.Type
	var elem reflect.Type
	switch {
	case t != nil && t.Kind() == reflect.Struct:
		fields = yamlFields(t)
	case t != nil && t.Kind() == reflect.Map:
		elem = t.Elem()
	}
	seen := make(map[string]int, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			w.add(k.Line, path, "mapping keys must be plain scalars")
			continue
		}
		if k.Tag == "!!merge" {
			w.add(k.Line, path, "merge keys (<<) are not supported in the config file")
			continue
		}
		if k.Anchor != "" || k.Style&yaml.TaggedStyle != 0 {
			w.add(k.Line, path, "anchors and explicit tags are not supported in the config file")
			continue
		}
		kp := join(path, k.Value)
		if first, dup := seen[k.Value]; dup {
			w.add(k.Line, kp, "duplicate key %q (first defined on line %d)", k.Value, first)
			continue
		}
		seen[k.Value] = k.Line
		w.lines[kp] = k.Line
		var ct reflect.Type
		switch {
		case fields != nil:
			ft, ok := fields[k.Value]
			if !ok {
				w.add(k.Line, kp, "unknown key %q; allowed keys here: %s", k.Value, strings.Join(sortedKeys(fields), ", "))
				// Still walk the value so anchors, aliases and size limits are enforced on it too.
				w.walk(v, nil, kp, depth+1)
				continue
			}
			ct = ft
		case elem != nil:
			ct = elem
		}
		w.walk(v, ct, kp, depth+1)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// yamlFields maps the yaml key of each exported field of struct type t to the field type.
func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(sf.Name)
		}
		out[name] = sf.Type
	}
	return out
}
