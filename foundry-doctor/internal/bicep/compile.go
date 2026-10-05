package bicep

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

// Compiler compiles Bicep files offline with a discovered Tool.
type Compiler struct {
	Tool   Tool
	Runner Runner
	// Root is the project root used to relativise diagnostic paths.
	Root string
}

// Result is the outcome of a compilation. A Bicep compile error is a normal
// result (OK=false), not a Go error.
type Result struct {
	OK          bool            `json:"ok"`
	ARM         json.RawMessage `json:"-"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
	Version     string          `json:"version"`
	Truncated   bool            `json:"truncated,omitempty"`
}

func (c Compiler) runner() Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return ExecRunner{}
}

// Compile runs `bicep build <file> --stdout --no-restore --diagnostics-format sarif`.
// --no-restore keeps it offline; registry modules not in cache surface as diagnostics.
func (c Compiler) Compile(ctx context.Context, file string) (Result, error) {
	if c.Tool.Path == "" {
		return Result{}, toolErr(KindMissing, "no bicep tool configured", nil)
	}
	dir := filepath.Dir(file)
	args := []string{"build", filepath.Base(file), "--stdout", "--no-restore", "--diagnostics-format", "sarif"}
	res, err := c.runner().Run(ctx, c.Tool.Path, args, dir)
	if err != nil {
		var te *ToolError
		if errors.As(err, &te) || ctx.Err() != nil {
			return Result{}, err
		}
		return Result{}, toolErr(KindNotExecutable, "bicep CLI could not be started", err)
	}
	return c.interpret(res)
}

func (c Compiler) interpret(res RunResult) (Result, error) {
	out := Result{Version: c.Tool.Version.String(), Truncated: res.Truncated}
	if res.Truncated {
		return out, toolErr(KindAdapter, "bicep output exceeded the size limit", nil)
	}
	diags, err := parseStderr(res.Stderr, c.Root)
	if err != nil {
		return out, err
	}
	out.Diagnostics = diags
	switch {
	case res.ExitCode == 0:
		if !json.Valid(res.Stdout) || !bytes.Contains(res.Stdout, []byte(`"resources"`)) {
			return out, toolErr(KindAdapter, "bicep produced no valid ARM JSON", nil)
		}
		out.OK = true
		out.ARM = json.RawMessage(bytes.TrimSpace(res.Stdout))
	case HasErrors(diags):
		out.OK = false
	default:
		return out, toolErr(KindAdapter, "bicep failed without parsable diagnostics", nil)
	}
	return out, nil
}

// parseStderr extracts a SARIF document if present, else text diagnostics.
func parseStderr(stderr []byte, root string) ([]Diagnostic, error) {
	s := string(stderr)
	if i := strings.Index(s, "{"); i >= 0 && strings.Contains(s[i:], "\"runs\"") {
		j := strings.LastIndex(s, "}")
		if j > i {
			return ParseSARIF([]byte(s[i:j+1]), root)
		}
	}
	return ParseTextDiagnostics(s, root), nil
}
