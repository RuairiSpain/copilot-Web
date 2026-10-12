// Package psrule is the optional PSRule for Azure adapter (PRD sections 6 and 9, ADR-001). PSRule is
// not a core dependency and PowerShell is not bundled: the adapter looks for pwsh on PATH, checks the
// PSRule.Rules.Azure module, and when the adapter is enabled runs a fixed command over the compiled
// ARM templates and parses the SARIF it writes. Native findings win over adapter findings
// (engine.DedupAdapter); PSRule here verifies overlap.
//
// Everything the tool prints or writes is untrusted: it is parsed strictly, bounded, sanitised
// and redacted before it becomes a finding.
package psrule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Name is the stable adapter name; it is also the overlap list name in the catalogue.
const Name = "psrule"

// InputPrefix is the AdapterRequest.Options key prefix for a compiled ARM template. The key is
// InputPrefix + the logical file name (for example "arm:infra/main.bicep", used as Location.File);
// the value is the compiled ARM JSON text. The adapter writes it to a temp directory; it never reads
// the project tree.
const InputPrefix = "arm:"

// Version floors. MinPowerShell is verified (PSRule for Azure docs/install.md: PowerShell 7.4 or newer).
// ProposedMinModule is only a proposal (docs/tool-compatibility.md section 2.4), so it is NOT enforced
// unless Config.MinModule is set; Detect otherwise reports the module version only.
const (
	MinPowerShell     = "7.4.0"
	ProposedMinModule = "1.47.0"
)

// Fixed PowerShell command text. No caller input is ever interpolated into either script; the temp
// directory is the working directory and the scripts use relative paths only.
const (
	detectScript = `$ErrorActionPreference = 'Stop'; ` +
		`$m = Get-Module -ListAvailable -Name PSRule.Rules.Azure | Sort-Object Version -Descending | Select-Object -First 1; ` +
		`[pscustomobject]@{ psVersion = $PSVersionTable.PSVersion.ToString(); module = $(if ($m) { $m.Version.ToString() } else { $null }) } | ConvertTo-Json -Compress`

	runScript = `$ErrorActionPreference = 'Stop'; ` +
		`Import-Module PSRule.Rules.Azure; ` +
		`Assert-PSRule -InputPath 'input' -Format File -Module PSRule.Rules.Azure -Option 'ps-rule.yaml' ` +
		`-OutputFormat Sarif -OutputPath 'out/results.sarif' -ErrorAction Continue; ` +
		`if (-not (Test-Path -LiteralPath 'out/results.sarif')) { exit 3 }; exit 0`

	// psRuleYAML turns on expansion of parameter files, which link each input template (see writeWorkspace).
	psRuleYAML = "configuration:\n  AZURE_PARAMETER_FILE_EXPANSION: true\n"
)

// Environment allow-list. HOME and PSModulePath stay so a user-scope module install is found;
// every credential, token and CI variable is dropped.
var envAllow = []string{
	"PATH", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "PROGRAMFILES", "PROGRAMDATA",
	"SYSTEMROOT", "WINDIR", "PSMODULEPATH", "LANG", "LC_ALL", "XDG_DATA_HOME", "XDG_CONFIG_HOME",
	"XDG_CACHE_HOME", "DOTNET_ROOT",
}

// ErrNoInput means Run was called without any compiled ARM template.
var ErrNoInput = errors.New("psrule: no compiled ARM template supplied")

// Mapper maps a PSRule rule name to the catalogue rules that list it under overlap.psrule.
// engine.OverlapIndex satisfies it.
type Mapper interface {
	Lookup(adapter, externalID string) []string
}

// Config configures the adapter. Zero values are safe defaults.
type Config struct {
	Mode          adapters.Mode // zero means auto
	Mapper        Mapper        // nil maps nothing: every finding is in the psrule/ namespace
	Runner        adapters.Runner
	LookPath      func(file string) (string, error)
	Environ       func() []string
	MinModule     string        // enforce a minimum PSRule.Rules.Azure version; empty means detection only
	DetectTimeout time.Duration // default 30s
	RunTimeout    time.Duration // default 5m
	MaxOutput     int64         // default adapters.DefaultMaxOutput
}

// Adapter implements sdk.Adapter.
type Adapter struct {
	cfg Config
}

var _ sdk.Adapter = (*Adapter)(nil)

// New returns the adapter with defaults filled in.
func New(cfg Config) *Adapter {
	if cfg.Mode == "" {
		cfg.Mode = adapters.ModeAuto
	}
	if cfg.Runner == nil {
		cfg.Runner = adapters.ExecRunner{}
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	if cfg.Environ == nil {
		cfg.Environ = os.Environ
	}
	if cfg.DetectTimeout <= 0 {
		cfg.DetectTimeout = 30 * time.Second
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 5 * time.Minute
	}
	if cfg.MaxOutput <= 0 {
		cfg.MaxOutput = adapters.DefaultMaxOutput
	}
	return &Adapter{cfg: cfg}
}

// Name implements sdk.Adapter.
func (a *Adapter) Name() string { return Name }

// Availability returns the adapter-unavailable data for st under the configured mode, or nil.
func (a *Adapter) Availability(st sdk.ToolStatus) *adapters.Unavailability {
	return adapters.CheckAvailability(Name, st, a.cfg.Mode)
}

func (a *Adapter) env(extra map[string]string) []string {
	set := map[string]string{
		"POWERSHELL_TELEMETRY_OPTOUT": "1", "POWERSHELL_UPDATECHECK": "Off",
		"DOTNET_CLI_TELEMETRY_OPTOUT": "1", "NO_COLOR": "1",
	}
	for k, v := range extra {
		set[k] = v
	}
	return adapters.RestrictedEnv(a.cfg.Environ(), envAllow, set)
}

// Detect implements sdk.Adapter. It never fails: every problem is a ToolStatus state.
func (a *Adapter) Detect(ctx context.Context) sdk.ToolStatus {
	st, _ := a.detect(ctx)
	return st
}

// detect also returns the absolute pwsh path for Run.
func (a *Adapter) detect(ctx context.Context) (sdk.ToolStatus, string) {
	st := sdk.ToolStatus{Name: Name, Required: a.cfg.Mode == adapters.ModeTrue}
	if a.cfg.Mode == adapters.ModeFalse {
		st.State, st.Detail = sdk.ToolDisabled, "disabled by advanced.psrule.enabled: false"
		return st, ""
	}
	path, err := a.cfg.LookPath("pwsh")
	if err != nil {
		st.State, st.Detail = sdk.ToolMissing, "pwsh not found on PATH"
		return st, ""
	}
	if !filepath.IsAbs(path) {
		st.State, st.Detail = sdk.ToolMissing, "pwsh resolved to a non-absolute path; refusing to run it"
		return st, ""
	}
	st.Path = path
	res, err := a.cfg.Runner.Run(ctx, adapters.Command{
		Path: path, Args: []string{"-NoProfile", "-NonInteractive", "-Command", detectScript},
		Dir: os.TempDir(), Env: a.env(nil), Timeout: a.cfg.DetectTimeout, MaxOutput: 64 << 10,
	})
	if err != nil {
		st.State, st.Detail = sdk.ToolFailed, "pwsh detection failed: "+errText(err)
		return st, ""
	}
	if res.ExitCode != 0 {
		st.State, st.Detail = sdk.ToolFailed, fmt.Sprintf("pwsh detection exited with code %d", res.ExitCode)
		return st, ""
	}
	var out struct {
		PSVersion string  `json:"psVersion"`
		Module    *string `json:"module"`
	}
	dec := json.NewDecoder(strings.NewReader(string(res.Stdout)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		st.State, st.Detail = sdk.ToolFailed, "pwsh detection output is not the expected JSON"
		return st, ""
	}
	psv, ok := adapters.ParseVersion(out.PSVersion)
	if !ok {
		st.State, st.Detail = sdk.ToolFailed, "pwsh reported an unparseable PowerShell version"
		return st, ""
	}
	minPS, _ := adapters.ParseVersion(MinPowerShell)
	if psv.Less(minPS) {
		st.State, st.Detail = sdk.ToolUnsupported, fmt.Sprintf("PowerShell %s is older than the minimum %s", safeVersion(out.PSVersion), MinPowerShell)
		return st, ""
	}
	if out.Module == nil || *out.Module == "" {
		st.State, st.Detail = sdk.ToolMissing, "PSRule.Rules.Azure module is not installed (Install-Module PSRule.Rules.Azure)"
		return st, ""
	}
	mv, ok := adapters.ParseVersion(*out.Module)
	if !ok {
		st.State, st.Detail = sdk.ToolFailed, "pwsh reported an unparseable PSRule.Rules.Azure version"
		return st, ""
	}
	st.Version = safeVersion(*out.Module)
	if a.cfg.MinModule != "" {
		if want, ok := adapters.ParseVersion(a.cfg.MinModule); ok && mv.Less(want) {
			st.State = sdk.ToolUnsupported
			st.Detail = fmt.Sprintf("PSRule.Rules.Azure %s is older than the minimum %s", st.Version, a.cfg.MinModule)
			return st, ""
		}
	}
	st.State = sdk.ToolAvailable
	st.Detail = fmt.Sprintf("PowerShell %s, PSRule.Rules.Azure %s", safeVersion(out.PSVersion), st.Version)
	return st, path
}

// Run implements sdk.Adapter. It runs only when the adapter is enabled and available.
func (a *Adapter) Run(ctx context.Context, req sdk.AdapterRequest) ([]sdk.Finding, error) {
	if a.cfg.Mode == adapters.ModeFalse {
		return nil, fmt.Errorf("psrule: %w", adapters.ErrDisabled)
	}
	inputs, err := collectInputs(req.Options)
	if err != nil {
		return nil, err
	}
	st, path := a.detect(ctx)
	if st.State != sdk.ToolAvailable {
		return nil, fmt.Errorf("psrule: %w (%s): %s", adapters.ErrUnavailable, st.State, st.Detail)
	}
	work, err := os.MkdirTemp("", "fd-psrule-")
	if err != nil {
		return nil, fmt.Errorf("psrule: create work directory: %w", err)
	}
	defer os.RemoveAll(work)
	if err := writeWorkspace(work, inputs); err != nil {
		return nil, fmt.Errorf("psrule: %w", err)
	}
	res, err := a.cfg.Runner.Run(ctx, adapters.Command{
		Path: path, Args: []string{"-NoProfile", "-NonInteractive", "-Command", runScript},
		Dir: work, Timeout: a.cfg.RunTimeout, MaxOutput: a.cfg.MaxOutput,
		Env: a.env(map[string]string{"TMPDIR": work, "TMP": work, "TEMP": work}),
	})
	if err != nil {
		return nil, fmt.Errorf("psrule: run: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("psrule: %w: pwsh exited with code %d", adapters.ErrProtocol, res.ExitCode)
	}
	data, err := adapters.ReadFileLimited(filepath.Join(work, "out", "results.sarif"), a.cfg.MaxOutput)
	if err != nil {
		if errors.Is(err, adapters.ErrOutputTooLarge) {
			return nil, fmt.Errorf("psrule: results file: %w", err)
		}
		return nil, fmt.Errorf("psrule: %w: results file unreadable", adapters.ErrProtocol)
	}
	return Parse(data, inputs, a.cfg.Mapper)
}

// Input is one compiled ARM template handed to PSRule. Name is the logical file name for locations.
type Input struct {
	Name     string
	Template []byte
}

const (
	maxInputs    = 256
	maxInputSize = 16 << 20
)

func collectInputs(opts map[string]string) ([]Input, error) {
	var names []string
	for k := range opts {
		if strings.HasPrefix(k, InputPrefix) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, ErrNoInput
	}
	if len(names) > maxInputs {
		return nil, fmt.Errorf("psrule: %d templates supplied, the limit is %d", len(names), maxInputs)
	}
	var out []Input
	for _, k := range names {
		name, body := strings.TrimPrefix(k, InputPrefix), opts[k]
		if !validLogicalName(name) {
			return nil, fmt.Errorf("psrule: invalid template name %q", clean(name))
		}
		if len(body) > maxInputSize {
			return nil, fmt.Errorf("psrule: template %q exceeds %d bytes", name, maxInputSize)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &obj); err != nil || obj == nil {
			return nil, fmt.Errorf("psrule: template %q is not a JSON object", name)
		}
		out = append(out, Input{Name: name, Template: []byte(body)})
	}
	return out, nil
}

// validLogicalName accepts a relative, forward-slash name with no traversal and no control characters.
func validLogicalName(n string) bool {
	if n == "" || len(n) > 260 || strings.HasPrefix(n, "/") || strings.Contains(n, `\`) || strings.Contains(n, ":") {
		return false
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == "" || seg == ".." || seg == "." {
			return false
		}
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// workFile returns the temp file stem for the i-th input (0-based): 0001, 0002, ...
func workFile(i int) string { return fmt.Sprintf("%04d", i+1) }

// writeWorkspace writes input/NNNN.json, input/NNNN.parameters.json (linking the template by relative
// metadata, which PSRule for Azure uses to expand a template), ps-rule.yaml and the out directory.
func writeWorkspace(work string, inputs []Input) error {
	if err := os.MkdirAll(filepath.Join(work, "input"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(work, "out"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "ps-rule.yaml"), []byte(psRuleYAML), 0o600); err != nil {
		return err
	}
	for i, in := range inputs {
		stem := workFile(i)
		params := fmt.Sprintf(`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#","contentVersion":"1.0.0.0","metadata":{"template":"./%s.json"},"parameters":{}}`, stem)
		if err := os.WriteFile(filepath.Join(work, "input", stem+".json"), in.Template, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(work, "input", stem+".parameters.json"), []byte(params), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func errText(err error) string {
	switch {
	case errors.Is(err, adapters.ErrTimeout):
		return "timed out"
	case errors.Is(err, adapters.ErrOutputTooLarge):
		return "output too large"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "could not execute"
}

// safeVersion returns v when it looks like a version string, otherwise a placeholder.
func safeVersion(v string) string {
	if len(v) == 0 || len(v) > 40 {
		return "unknown"
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+') {
			return "unknown"
		}
	}
	return v
}
