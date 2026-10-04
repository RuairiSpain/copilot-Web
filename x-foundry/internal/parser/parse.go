package parser

import (
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
)

// Parsed is the result of parsing: the typed configuration plus the author's mapping.
type Parsed struct {
	Source string
	Raw    map[string]any
	Config *config.XFoundry
}

// ParseXFoundry validates an x-foundry subtree (JSON Schema first, then typed decoding).
// Errors are *diag.Failure.
func ParseXFoundry(raw any, source string) (*Parsed, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, diag.Fail(diag.Err("XF101", Root, "'x-foundry' must be a mapping"))
	}
	if problems := CheckVersion(obj); len(problems) > 0 {
		return nil, diag.Fail(problems...)
	}
	problems, err := ValidateSchema(obj)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, diag.Fail(problems...)
	}
	cfg, err := config.Decode(obj)
	if err != nil {
		return nil, diag.Fail(diag.Err("XF110", Root, "%v", err))
	}
	return &Parsed{Source: source, Raw: obj, Config: cfg}, nil
}

// ParseMapping parses an already-loaded azure.yaml mapping.
func ParseMapping(document any, source string) (*Parsed, error) {
	doc, ok := document.(map[string]any)
	if !ok {
		return nil, diag.Fail(diag.Err("XF101", "", "azure.yaml must contain a mapping at the top level"))
	}
	section, ok := doc["x-foundry"]
	if !ok {
		return nil, diag.Fail(diag.Err("XF101", Root, "azure.yaml has no 'x-foundry' section"))
	}
	return ParseXFoundry(section, source)
}

// ParseText parses azure.yaml text.
func ParseText(text, source string) (*Parsed, error) {
	doc, err := LoadYAML(text)
	if err != nil {
		return nil, err
	}
	return ParseMapping(doc, source)
}

// ParseFile parses an azure.yaml file.
func ParseFile(path string) (*Parsed, error) {
	text, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseText(text, path)
}
