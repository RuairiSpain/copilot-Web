package bicep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

// Version is a parsed Bicep CLI version.
type Version struct {
	Major, Minor, Patch int
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Compare returns -1, 0 or 1.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] < p[1] {
			return -1
		}
		if p[0] > p[1] {
			return 1
		}
	}
	return 0
}

// Contract is the parsed ContractVersion.
var Contract = Version{0, 48, 1}

var versionRE = regexp.MustCompile(`Bicep CLI version (\d+)\.(\d+)\.(\d+)`)

// ParseVersion parses `bicep --version` output.
func ParseVersion(out string) (Version, error) {
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return Version{}, errors.New("unrecognised version output")
	}
	var v Version
	for i, dst := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, err
		}
		*dst = n
	}
	return v, nil
}

// Tool is a discovered, version-checked Bicep CLI.
type Tool struct {
	Path    string
	Version Version
	// Newer is true when the CLI is newer than the tested contract version.
	Newer bool
	// Source is "BICEP_PATH", "option" or "PATH".
	Source string
}

// DiscoverOptions controls discovery.
type DiscoverOptions struct {
	// Path is an explicit CLI path; "" or "auto" means BICEP_PATH then PATH.
	Path     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
	Runner   Runner
}

// Discover locates the CLI (explicit path, BICEP_PATH, then `bicep` on PATH;
// `az bicep` is not used) and verifies it meets the compatibility contract.
// Failures are *ToolError values for which IsDependencyError is true.
func Discover(ctx context.Context, o DiscoverOptions) (Tool, error) {
	getenv, look, stat, runner := o.Getenv, o.LookPath, o.Stat, o.Runner
	if getenv == nil {
		getenv = os.Getenv
	}
	if look == nil {
		look = exec.LookPath
	}
	if stat == nil {
		stat = os.Stat
	}
	if runner == nil {
		runner = ExecRunner{Timeout: defaultTimeout}
	}

	var path, source string
	switch {
	case o.Path != "" && o.Path != "auto":
		path, source = o.Path, "option"
	case getenv("BICEP_PATH") != "" && getenv("BICEP_PATH") != "auto":
		path, source = getenv("BICEP_PATH"), "BICEP_PATH"
	default:
		p, err := look("bicep")
		if err != nil {
			return Tool{}, toolErr(KindMissing, "bicep not found via BICEP_PATH or PATH", err)
		}
		path, source = p, "PATH"
	}
	if source != "PATH" {
		fi, err := stat(path)
		if err != nil || fi.IsDir() {
			return Tool{}, toolErr(KindMissing, "configured bicep path does not exist", err)
		}
	}

	res, err := runner.Run(ctx, path, []string{"--version"}, "")
	if err != nil {
		var te *ToolError
		if errors.As(err, &te) {
			return Tool{}, err
		}
		if ctx.Err() != nil {
			return Tool{}, ctx.Err()
		}
		return Tool{}, toolErr(KindNotExecutable, "bicep CLI could not be started", err)
	}
	if res.ExitCode != 0 {
		return Tool{}, toolErr(KindNotExecutable, fmt.Sprintf("bicep --version exited %d", res.ExitCode), nil)
	}
	v, perr := ParseVersion(string(res.Stdout) + string(res.Stderr))
	if perr != nil {
		return Tool{}, toolErr(KindVersionUnknown, "cannot determine bicep version", perr)
	}
	if v.Compare(Contract) < 0 {
		return Tool{}, toolErr(KindVersionTooOld,
			fmt.Sprintf("bicep %s is older than the supported %s", v, ContractVersion), nil)
	}
	return Tool{Path: path, Version: v, Newer: v.Compare(Contract) > 0, Source: source}, nil
}
