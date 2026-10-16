package bicep

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Bounds on parameter files (ADR-018).
const (
	// MaxParameterFileBytes bounds a parameter file and the output of
	// `bicep build-params`.
	MaxParameterFileBytes = 1 << 20
	// MaxParameters bounds the parameters read from one file.
	MaxParameters = 2000
	// maxParameterString bounds one expanded string value.
	maxParameterString = 4096
)

// Named reasons a parameter file is not used. They appear in warnings and in
// the skip text, never as a panic or a silent drop.
const (
	ParamMalformed        = "parameter-file-malformed"
	ParamTooLarge         = "parameter-file-too-large"
	ParamTooMany          = "parameter-file-too-many-parameters"
	ParamBicepparamFailed = "bicepparam-compile-failed"
	ParamNeedsEnv         = "bicepparam-needs-environment"
	ParamUnreadable       = "parameter-file-unreadable"
)

// ParamsError is the failure to use a parameter file. Reason is one of the
// Param* constants.
type ParamsError struct {
	Reason string
	Detail string
}

func (e *ParamsError) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// ReasonOf returns the named reason of a parameter-file error, or "" for any
// other error.
func ReasonOf(err error) string {
	var pe *ParamsError
	if errors.As(err, &pe) {
		return pe.Reason
	}
	return ""
}

// ParamSet is the deployment parameter values read from a parameter file.
type ParamSet struct {
	// Values maps a parameter name to a string, bool or json.Number. A name
	// mapped to nil is set but not resolvable (an unresolved ${VAR}, an
	// object, an array, null or a Key Vault reference).
	Values map[string]any
	// Unresolved lists, sorted, the names set to a string that holds a ${VAR}
	// substitution that could not be resolved from the environment.
	Unresolved []string
}

// ParseParametersJSON reads an ARM deployment parameter file
// ({"parameters": {"name": {"value": ...}}}). expand, when non-nil, resolves
// the azd substitutions in string values (see ExpandAzd); a string it cannot
// resolve makes the parameter unresolved. A nil expand reads strings
// literally (the output of `bicep build-params`). A malformed or oversize
// file returns a *ParamsError.
func ParseParametersJSON(data []byte, expand func(string) (string, bool)) (ParamSet, error) {
	if len(data) > MaxParameterFileBytes {
		return ParamSet{}, &ParamsError{Reason: ParamTooLarge, Detail: fmt.Sprintf("more than %d bytes", MaxParameterFileBytes)}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc struct {
		Parameters map[string]json.RawMessage `json:"parameters"`
	}
	if err := dec.Decode(&doc); err != nil {
		return ParamSet{}, &ParamsError{Reason: ParamMalformed, Detail: "not a JSON object"}
	}
	if dec.More() {
		return ParamSet{}, &ParamsError{Reason: ParamMalformed, Detail: "trailing data after the JSON document"}
	}
	if doc.Parameters == nil {
		return ParamSet{}, &ParamsError{Reason: ParamMalformed, Detail: "no parameters object"}
	}
	if len(doc.Parameters) > MaxParameters {
		return ParamSet{}, &ParamsError{Reason: ParamTooMany, Detail: fmt.Sprintf("more than %d parameters", MaxParameters)}
	}
	set := ParamSet{Values: make(map[string]any, len(doc.Parameters))}
	for name, raw := range doc.Parameters {
		var p struct {
			Value     json.RawMessage `json:"value"`
			Reference json.RawMessage `json:"reference"`
		}
		if json.Unmarshal(raw, &p) != nil || len(bytes.TrimSpace(p.Value)) == 0 || len(p.Reference) > 0 {
			set.Values[name] = nil
			continue
		}
		vd := json.NewDecoder(bytes.NewReader(p.Value))
		vd.UseNumber()
		var v any
		if vd.Decode(&v) != nil {
			set.Values[name] = nil
			continue
		}
		switch x := v.(type) {
		case bool, json.Number:
			set.Values[name] = x
		case string:
			if expand == nil {
				set.Values[name] = x
				continue
			}
			out, ok := expand(x)
			if !ok || len(out) > maxParameterString {
				set.Values[name] = nil
				set.Unresolved = append(set.Unresolved, name)
				continue
			}
			set.Values[name] = out
		default:
			set.Values[name] = nil
		}
	}
	sort.Strings(set.Unresolved)
	return set, nil
}

var azdVarRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-=])(.*))?$`)

// ExpandAzd returns a function that resolves azd parameter-file substitutions
// ${VAR}, ${VAR=default}, ${VAR:=default}, ${VAR-default} and ${VAR:-default}
// against env, the values of the selected azd environment. It is pure: it
// never reads the process environment and never runs azd.
//
// A variable that is not in env is unresolved, and so is every variable when
// known is false (no environment was selected or readable): azd could
// substitute a value the doctor cannot see, so a default is not a fact either.
// A plain ${VAR} whose value is empty is unresolved. The colon forms use the
// default when the value is empty or missing, the others only when it is
// missing. Anything else containing "${" (an unterminated or malformed
// reference) makes the string unresolved. A string without "${" is returned
// unchanged.
func ExpandAzd(env map[string]string, known bool) func(string) (string, bool) {
	return func(s string) (string, bool) {
		if !strings.Contains(s, "${") {
			return s, true
		}
		var b strings.Builder
		for {
			i := strings.Index(s, "${")
			if i < 0 {
				b.WriteString(s)
				return b.String(), true
			}
			b.WriteString(s[:i])
			rest := s[i+2:]
			j := strings.IndexByte(rest, '}')
			if j < 0 || !known {
				return "", false
			}
			m := azdVarRE.FindStringSubmatch(rest[:j])
			if m == nil {
				return "", false
			}
			val, set := env[m[1]]
			switch {
			case m[2] == "":
				if !set || val == "" {
					return "", false
				}
			case strings.HasPrefix(m[2], ":"):
				if !set || val == "" {
					val = m[3]
				}
			default:
				if !set {
					val = m[3]
				}
			}
			b.WriteString(val)
			s = rest[j+1:]
			if b.Len() > maxParameterString {
				return "", false
			}
		}
	}
}

// BuildParams compiles a .bicepparam file with `bicep build-params --stdout
// --no-restore` (offline) and returns the parameter values the Bicep CLI
// computed. The CLI reads readEnvironmentVariable() from the process
// environment of the runner (see ExecRunner.ExtraEnv). A failure returns a
// *ParamsError with reason ParamBicepparamFailed (or a limit reason).
func (c Compiler) BuildParams(ctx context.Context, file string) (ParamSet, error) {
	if c.Tool.Path == "" {
		return ParamSet{}, toolErr(KindMissing, "no bicep tool configured", nil)
	}
	args := []string{"build-params", filepath.Base(file), "--stdout", "--no-restore"}
	res, err := c.runner().Run(ctx, c.Tool.Path, args, filepath.Dir(file))
	if err != nil {
		if ctx.Err() != nil {
			return ParamSet{}, ctx.Err()
		}
		return ParamSet{}, &ParamsError{Reason: ParamBicepparamFailed, Detail: "bicep CLI could not be run"}
	}
	if res.Truncated {
		return ParamSet{}, &ParamsError{Reason: ParamTooLarge, Detail: "build-params output exceeded the size limit"}
	}
	if res.ExitCode != 0 {
		return ParamSet{}, &ParamsError{Reason: ParamBicepparamFailed, Detail: fmt.Sprintf("bicep build-params exited with code %d", res.ExitCode)}
	}
	var out struct {
		ParametersJSON string `json:"parametersJson"`
	}
	if json.Unmarshal(bytes.TrimSpace(res.Stdout), &out) != nil || out.ParametersJSON == "" {
		return ParamSet{}, &ParamsError{Reason: ParamBicepparamFailed, Detail: "build-params produced no parametersJson"}
	}
	return ParseParametersJSON([]byte(out.ParametersJSON), nil)
}
