package azureyaml

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// seg is one step of a document path: a mapping key or a sequence index.
type seg struct {
	s   string
	idx bool
}

type path []seg

func (p path) key(k string) path { return p.with(seg{s: k}) }
func (p path) index(i int) path  { return p.with(seg{s: strconv.Itoa(i), idx: true}) }

func (p path) with(s seg) path {
	out := make(path, len(p)+1)
	copy(out, p)
	out[len(p)] = s
	return out
}

func plainKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// Dotted renders services.api.uses[0]; keys with other characters render as ["a.b"].
func (p path) Dotted() string {
	var sb strings.Builder
	for _, s := range p {
		switch {
		case s.idx:
			sb.WriteString("[" + s.s + "]")
		case plainKey(s.s):
			if sb.Len() > 0 {
				sb.WriteByte('.')
			}
			sb.WriteString(s.s)
		default:
			sb.WriteString("[" + strconv.Quote(s.s) + "]")
		}
	}
	return sb.String()
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// Pointer renders an RFC 6901 JSON pointer.
func (p path) Pointer() string {
	var sb strings.Builder
	for _, s := range p {
		sb.WriteByte('/')
		sb.WriteString(pointerEscaper.Replace(s.s))
	}
	return sb.String()
}

func posOf(n *yaml.Node) model.Pos { return model.Pos{Line: n.Line, Column: n.Column} }

// builder converts the yaml.Node tree to model.Node while enforcing limits and collecting YAML-level data.
// It never follows an alias, so work is linear in the size of the input.
type builder struct {
	lim     Limits
	nodes   int
	dups    []Duplicate
	interps []Interpolation
	issues  []Issue
	perCode map[IssueCode]int // YAML-level issues seen, to cap the output of a hostile file
	lastPos map[IssueCode]model.Pos
}

// maxYAMLIssuesPerCode caps anchor, alias, merge-key and complex-key issues so a hostile file cannot produce an
// unbounded report. One summary issue per code states how many were left out.
const maxYAMLIssuesPerCode = 100

func (b *builder) count(n *yaml.Node) error {
	b.nodes++
	if b.nodes > b.lim.MaxNodes {
		return &LimitError{Limit: "nodes", Max: b.lim.MaxNodes, Pos: posOf(n)}
	}
	return nil
}

func (b *builder) yamlIssue(code IssueCode, p path, pos model.Pos, msg string) {
	if b.perCode == nil {
		b.perCode, b.lastPos = map[IssueCode]int{}, map[IssueCode]model.Pos{}
	}
	b.perCode[code]++
	b.lastPos[code] = pos
	if b.perCode[code] > maxYAMLIssuesPerCode {
		return
	}
	b.issues = append(b.issues, Issue{Code: code, Level: LevelInfo, Path: p.Dotted(), Pointer: p.Pointer(), Pos: pos, Message: msg})
}

func (b *builder) convert(n *yaml.Node, key string, keyPos model.Pos, p path, depth int) (*model.Node, error) {
	if depth > b.lim.MaxDepth {
		return nil, &LimitError{Limit: "depth", Max: b.lim.MaxDepth, Pos: posOf(n)}
	}
	if err := b.count(n); err != nil {
		return nil, err
	}
	out := &model.Node{Key: key, KeyPos: keyPos, Pos: posOf(n)}
	if n.Anchor != "" && n.Kind != yaml.AliasNode {
		b.yamlIssue(CodeAnchor, p, posOf(n), "anchors are not expanded by the reader; azd may expand them, so the effective value can differ")
	}
	switch n.Kind {
	case yaml.AliasNode:
		b.yamlIssue(CodeAlias, p, posOf(n), "aliases are not expanded by the reader; the effective value is unknown")
		out.Kind = model.NodeScalar
	case yaml.MappingNode:
		out.Kind = model.NodeMapping
		if err := b.mapping(n, out, p, depth); err != nil {
			return nil, err
		}
	case yaml.SequenceNode:
		out.Kind = model.NodeSequence
		for i, c := range n.Content {
			child, err := b.convert(c, "", model.Pos{}, p.index(i), depth+1)
			if err != nil {
				return nil, err
			}
			out.Children = append(out.Children, child)
		}
	default:
		out.Kind = model.NodeScalar
		if len(n.Value) > b.lim.MaxScalarBytes {
			return nil, &LimitError{Limit: "scalar", Max: b.lim.MaxScalarBytes, Pos: posOf(n)}
		}
		out.Value = n.Value
		b.scan(n, p)
	}
	return out, nil
}

func (b *builder) mapping(n *yaml.Node, out *model.Node, p path, depth int) error {
	type occ struct {
		key string
		pos []model.Pos
	}
	var order []string
	seen := map[string]*occ{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if err := b.count(k); err != nil {
			return err
		}
		keyText := ""
		switch {
		case k.Kind == yaml.ScalarNode:
			if len(k.Value) > b.lim.MaxScalarBytes {
				return &LimitError{Limit: "scalar", Max: b.lim.MaxScalarBytes, Pos: posOf(k)}
			}
			keyText = k.Value
			if k.ShortTag() == "!!merge" {
				b.yamlIssue(CodeMergeKey, p, posOf(k), "merge keys (<<) are not expanded by the reader; azd may expand them, so the effective keys can differ")
			}
		case k.Kind == yaml.AliasNode:
			b.yamlIssue(CodeAlias, p, posOf(k), "an alias used as a mapping key is not expanded")
		default:
			b.yamlIssue(CodeNonScalarKey, p, posOf(k), "a mapping key is a collection; it is kept with an empty name")
		}
		cp := p.key(keyText)
		child, err := b.convert(v, keyText, posOf(k), cp, depth+1)
		if err != nil {
			return err
		}
		out.Children = append(out.Children, child)
		o := seen[keyText]
		if o == nil {
			o = &occ{key: keyText}
			seen[keyText] = o
			order = append(order, keyText)
		}
		o.pos = append(o.pos, posOf(k))
	}
	for _, k := range order {
		if o := seen[k]; len(o.pos) > 1 {
			b.dups = append(b.dups, Duplicate{Path: p.key(k).Dotted(), Key: k, Positions: o.pos})
		}
	}
	return nil
}

// finish returns the collected issues plus one summary per capped code, in code order.
func (b *builder) finish() []Issue {
	codes := make([]IssueCode, 0, len(b.perCode))
	for c, n := range b.perCode {
		if n > maxYAMLIssuesPerCode {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	for _, c := range codes {
		b.issues = append(b.issues, Issue{
			Code: c, Level: LevelInfo, Pos: b.lastPos[c],
			Message: fmt.Sprintf("%d more %s issues are not listed", b.perCode[c]-maxYAMLIssuesPerCode, c),
		})
	}
	return b.issues
}
