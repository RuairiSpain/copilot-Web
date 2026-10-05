// Package azureyaml reads an azure.yaml file into the model.AzureYAML view the rules consume (ADR-004, ADR-011).
//
// It parses with a yaml.Node tree, so every key and value keeps its line and column, duplicate mapping keys are
// reported as data instead of failing the parse, and aliases are never expanded (an alias bomb costs nothing).
// It never evaluates ${VAR} references: it only records which form each reference has and where it is.
//
// Structural checks (required fields, types, enums, patterns, forbidden properties) use the vendored azd schemas in
// schemas/vendor/azd. This is a small keyword subset, not a JSON-schema engine (ADR-011 decision 3).
//
// Parse returns an error only when no usable document exists (empty, unparseable, several documents, not a
// mapping, over a limit). Everything else a file can get wrong is returned as Issue values in the Result.
package azureyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Limits bound the work done on untrusted input. A zero field means the default.
type Limits struct {
	MaxBytes       int // raw file size
	MaxDepth       int // nesting of mappings and sequences
	MaxNodes       int // keys, values and collections together
	MaxScalarBytes int // length of one scalar
}

// DefaultLimits returns the limits used when Options.Limits is zero.
func DefaultLimits() Limits {
	return Limits{MaxBytes: 1 << 20, MaxDepth: 64, MaxNodes: 100_000, MaxScalarBytes: 64 << 10}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = d.MaxNodes
	}
	if l.MaxScalarBytes <= 0 {
		l.MaxScalarBytes = d.MaxScalarBytes
	}
	return l
}

// Options configure Parse.
type Options struct {
	File   string // relative path recorded in the result, normally "azure.yaml"
	Limits Limits
}

// Sentinel errors. Use errors.Is; the concrete errors below carry details.
var (
	ErrEmpty         = errors.New("azure.yaml is empty")
	ErrSyntax        = errors.New("azure.yaml is not valid YAML")
	ErrMultiDocument = errors.New("azure.yaml has more than one YAML document")
	ErrNotMapping    = errors.New("azure.yaml root is not a mapping")
	ErrLimit         = errors.New("azure.yaml exceeds a reader limit")
)

// SyntaxError is a YAML syntax error. Line is 0 when the parser did not report one.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("azure.yaml line %d: %s", e.Line, e.Msg)
	}
	return "azure.yaml: " + e.Msg
}

// Is makes errors.Is(err, ErrSyntax) true.
func (e *SyntaxError) Is(target error) bool { return target == ErrSyntax }

// Pos returns the error position.
func (e *SyntaxError) Pos() model.Pos { return model.Pos{Line: e.Line} }

// LimitError reports which limit was exceeded.
type LimitError struct {
	Limit string // "bytes", "depth", "nodes" or "scalar"
	Max   int
	Pos   model.Pos
}

func (e *LimitError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("azure.yaml exceeds the %s limit of %d at line %d", e.Limit, e.Max, e.Pos.Line)
	}
	return fmt.Sprintf("azure.yaml exceeds the %s limit of %d", e.Limit, e.Max)
}

// Is makes errors.Is(err, ErrLimit) true.
func (e *LimitError) Is(target error) bool { return target == ErrLimit }

var yamlLineRe = regexp.MustCompile(`line (\d+)`)

func syntaxError(err error) *SyntaxError {
	msg := err.Error()
	se := &SyntaxError{Msg: trimYAMLPrefix(msg)}
	if m := yamlLineRe.FindStringSubmatch(msg); m != nil {
		se.Line, _ = strconv.Atoi(m[1])
	}
	return se
}

func trimYAMLPrefix(s string) string {
	const p = "yaml: "
	if len(s) > len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse reads azure.yaml bytes. See the package comment for what is an error and what is data.
func Parse(src []byte, opt Options) (res *Result, err error) {
	lim := opt.Limits.withDefaults()
	if len(src) > lim.MaxBytes {
		return nil, &LimitError{Limit: "bytes", Max: lim.MaxBytes}
	}
	// A UTF-8 BOM is accepted and ignored. Columns count characters after it.
	src = bytes.TrimPrefix(src, utf8BOM)

	defer func() {
		// The YAML library panics on some malformed input in older versions; never let that escape.
		if r := recover(); r != nil {
			res, err = nil, &SyntaxError{Msg: "the YAML parser rejected the input"}
		}
	}()

	dec := yaml.NewDecoder(bytes.NewReader(src))
	var doc yaml.Node
	if derr := dec.Decode(&doc); derr != nil {
		if errors.Is(derr, io.EOF) {
			return nil, ErrEmpty
		}
		return nil, syntaxError(derr)
	}
	var extra yaml.Node
	switch xerr := dec.Decode(&extra); {
	case xerr == nil:
		return nil, ErrMultiDocument
	case !errors.Is(xerr, io.EOF):
		return nil, syntaxError(xerr)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, ErrEmpty
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w (line %d)", ErrNotMapping, top.Line)
	}

	b := &builder{lim: lim}
	root, cerr := b.convert(top, "", model.Pos{}, nil, 0)
	if cerr != nil {
		return nil, cerr
	}
	res = &Result{
		YAML:           &model.AzureYAML{File: opt.File, Root: root},
		Duplicates:     b.dups,
		Interpolations: b.interps,
		Issues:         b.finish(),
	}
	project(res)
	checkStructure(res, top)
	sortIssues(res.Issues)
	return res, nil
}
