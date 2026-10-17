package project

import (
	"context"
	"os"
	"path"
	"slices"
	"strings"
)

// ParameterFiles returns the deployment parameter files that sit next to the
// entry template (entry is the root-relative, slash-separated main.bicep): the
// Bicep parameters file "<entry minus .bicep>.bicepparam" and the ARM
// parameters file "<entry minus .bicep>.parameters.json", which azd reads from
// infra/main.bicepparam and infra/main.parameters.json. A path is returned
// only when it is a regular file (not a symlink); otherwise it is "". Only the
// two exact names are checked; the files are not read.
func ParameterFiles(r *os.Root, entry string) (bicepparam, parametersJSON string) {
	base := strings.TrimSuffix(entry, path.Ext(entry))
	regular := func(rel string) bool {
		fi, err := r.Lstat(rel)
		return err == nil && fi.Mode().IsRegular()
	}
	if p := base + ".bicepparam"; regular(p) {
		bicepparam = p
	}
	if p := base + ".parameters.json"; regular(p) {
		parametersJSON = p
	}
	return bicepparam, parametersJSON
}

// ListInfraFiles returns the *.bicep files under the infra directory, sorted
// and bounded by maxFiles (0 means DefaultMaxFiles), with the same traversal
// rules as Discover: symlinks are skipped and depth is limited. Warnings are
// sorted notes about what was skipped.
func ListInfraFiles(ctx context.Context, r *os.Root, infraPath string, maxFiles int) (files, warnings []string, err error) {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	p := &Project{InfraPath: infraPath}
	warn := map[string]bool{}
	if err := p.scanInfra(ctx, r, maxFiles, warn); err != nil {
		return nil, nil, err
	}
	for _, f := range p.InfraFiles {
		if strings.EqualFold(path.Ext(f), ".bicep") {
			files = append(files, f)
		}
	}
	for w := range warn {
		warnings = append(warnings, w)
	}
	slices.Sort(warnings)
	return files, warnings, nil
}
