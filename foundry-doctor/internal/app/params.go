package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
)

// Bounds for the offline helpers that run next to the entry compile
// (ADR-018). All of them are read-only: they open files under the project
// root and run `bicep build` / `bicep build-params` offline; they never call
// Azure or azd.
const (
	// maxBicepparamBytes bounds the .bicepparam file scanned for
	// readEnvironmentVariable.
	maxBicepparamBytes = 256 << 10
	// MaxModuleFiles bounds the module files compiled alone to find their
	// templateHash; further candidates keep the entry file as location.
	MaxModuleFiles = 64
	// maxModuleFileBytes bounds one module file compiled alone.
	maxModuleFileBytes = 512 << 10
	// maxModuleScan bounds the .bicep files listed while looking for modules.
	maxModuleScan = 1000
	// moduleBudget bounds the total time spent compiling module files.
	moduleBudget = 3 * time.Minute
	// maxBicepEnv bounds the environment values handed to bicep build-params.
	maxBicepEnv = 256
)

var envNameOK = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// selectedEnv returns the readable values of the selected azd environment
// without the secret-looking keys, and whether the environment was readable.
// known is false when no environment is selected or its .env is unusable: the
// parameter file's ${VAR} substitutions then stay unresolved.
func selectedEnv(root *os.Root, src Source) (vals map[string]string, known bool) {
	if src.Environment == "" {
		return nil, false
	}
	all, _, st := readEnvFile(root, path.Join(project.AzureDir, src.Environment, ".env"))
	if st != env.StatusOK {
		return nil, false
	}
	vals = make(map[string]string, len(all))
	for k, v := range all {
		if !secretishKey(k) {
			vals[k] = v
		}
	}
	return vals, true
}

// paramResult is the outcome of looking for deployment parameters.
type paramResult struct {
	values   map[string]any
	provided bool
	notes    []string
}

// loadParameters reads the deployment parameters of the entry template:
// <entry>.bicepparam (compiled with bicep build-params) takes precedence over
// <entry>.parameters.json. A file that cannot be used yields a named note and
// no parameters; there is no fallback to the other file, because azd would use
// the first one and the doctor must not model a different deployment.
//
// comp is nil when no Bicep CLI is available (an ARM JSON entry).
func (l armLoader) loadParameters(ctx context.Context, root *os.Root, entry string, comp *bicep.Compiler, src Source) paramResult {
	bp, pj := project.ParameterFiles(root, entry)
	vals, known := selectedEnv(root, src)
	skip := func(file string, err error) paramResult {
		reason := bicep.ReasonOf(err)
		if reason == "" {
			reason = bicep.ParamUnreadable
		}
		return paramResult{notes: []string{fmt.Sprintf("parameter file %s not used (%s); parameter-driven conditions, names and regions stay unresolved", file, reason)}}
	}
	var set bicep.ParamSet
	var file string
	switch {
	case bp != "":
		file = bp
		if comp == nil {
			return skip(bp, &bicep.ParamsError{Reason: bicep.ParamBicepparamFailed, Detail: "no Bicep CLI"})
		}
		text, err := readRootFile(root, bp, maxBicepparamBytes)
		if err != nil {
			return skip(bp, &bicep.ParamsError{Reason: bicep.ParamUnreadable})
		}
		if strings.Contains(string(text), "readEnvironmentVariable") && !known {
			return skip(bp, &bicep.ParamsError{Reason: bicep.ParamNeedsEnv})
		}
		c := *comp
		if l.runner == nil {
			c.Runner = bicep.ExecRunner{ExtraEnv: bicepEnv(vals)}
		}
		if set, err = c.BuildParams(ctx, filepath.Join(src.Dir, filepath.FromSlash(bp))); err != nil {
			return skip(bp, err)
		}
	case pj != "":
		file = pj
		data, err := readRootFile(root, pj, bicep.MaxParameterFileBytes)
		if err != nil {
			reason := bicep.ParamUnreadable
			if strings.Contains(err.Error(), "exceeds") {
				reason = bicep.ParamTooLarge
			}
			return skip(pj, &bicep.ParamsError{Reason: reason})
		}
		if set, err = bicep.ParseParametersJSON(data, bicep.ExpandAzd(vals, known)); err != nil {
			return skip(pj, err)
		}
	default:
		return paramResult{}
	}
	res := paramResult{values: set.Values, provided: true}
	if len(set.Unresolved) > 0 {
		res.notes = append(res.notes, fmt.Sprintf("parameter file %s: %s reference azd variables that could not be resolved from the selected environment and stay unresolved",
			file, strings.Join(set.Unresolved, ", ")))
	}
	return res
}

// bicepEnv renders environment values for the Bicep CLI process
// (readEnvironmentVariable). Secret-looking keys were removed by selectedEnv.
func bicepEnv(vals map[string]string) []string {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if envNameOK.MatchString(k) && !strings.ContainsAny(vals[k], "\x00") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > maxBicepEnv {
		keys = keys[:maxBicepEnv]
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k + "=" + vals[k]
	}
	return out
}

// moduleResult maps template hashes to module files.
type moduleResult struct {
	hashes map[string]string
	notes  []string
}

// locateModules finds the source file of each module deployment in the
// compiled entry template without reading Bicep. A module file compiled alone
// has the templateHash of the template nested in its caller, so every .bicep
// file next to the entry (under its directory, bounded) is compiled alone with
// `bicep build --no-restore` and matched by hash. Compilation stops when every
// hash is matched or a bound (MaxModuleFiles, file size, time) is reached.
// Two files with the same hash are ambiguous and not mapped. Unmatched modules
// keep the entry file as location.
func (l armLoader) locateModules(ctx context.Context, root *os.Root, entry string, comp bicep.Compiler, src Source, arm []byte) moduleResult {
	want := map[string]bool{}
	for _, h := range bicep.NestedTemplateHashes(arm, 2000) {
		want[h] = true
	}
	if len(want) == 0 {
		return moduleResult{}
	}
	dir := path.Dir(entry)
	files, _, err := project.ListInfraFiles(ctx, root, dir, maxModuleScan)
	if err != nil {
		return moduleResult{notes: []string{"module file locations not resolved: infra directory could not be listed"}}
	}
	files = slices.DeleteFunc(files, func(f string) bool { return f == entry })
	var res moduleResult
	if len(files) > MaxModuleFiles {
		res.notes = append(res.notes, fmt.Sprintf("module file locations: only the first %d of %d candidate .bicep files were compiled; findings in other modules are located at the entry file", MaxModuleFiles, len(files)))
		files = files[:MaxModuleFiles]
	}
	ctx, cancel := context.WithTimeout(ctx, moduleBudget)
	defer cancel()
	found := map[string]string{}
	ambiguous := map[string]bool{}
	failed := 0
	for _, f := range files {
		if ctx.Err() != nil || len(found)+len(ambiguous) >= len(want) {
			break
		}
		fi, err := root.Stat(f)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxModuleFileBytes {
			failed++
			continue
		}
		r, err := comp.Compile(ctx, filepath.Join(src.Dir, filepath.FromSlash(f)))
		if err != nil || !r.OK {
			failed++
			continue
		}
		h := bicep.RootTemplateHash(r.ARM)
		switch {
		case h == "" || !want[h]:
		case ambiguous[h]:
		case found[h] != "":
			delete(found, h)
			ambiguous[h] = true
		default:
			found[h] = f
		}
	}
	if failed > 0 {
		res.notes = append(res.notes, fmt.Sprintf("module file locations: %d .bicep file(s) could not be compiled alone and were not matched", failed))
	}
	if len(ambiguous) > 0 {
		res.notes = append(res.notes, fmt.Sprintf("module file locations: %d module(s) match more than one identical file and are located at the entry file", len(ambiguous)))
	}
	res.hashes = found
	return res
}

// modelOptions assembles armmodel options from the parameter and module
// lookups.
func modelOptions(entry string, p paramResult, m moduleResult) armmodel.Options {
	return armmodel.Options{
		Entry: entry, ModuleHashes: m.hashes,
		Parameters: p.values, ParametersProvided: p.provided,
	}
}
