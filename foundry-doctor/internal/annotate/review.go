package annotate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	bicepcomments "github.com/ruairispain/copilot-web/foundry-doctor/internal/annotate/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/annotate/yaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/diff"
)

const (
	manifestName = "annotations-manifest.json"
	diffName     = "annotations.diff"
)

// ReviewRequest describes one review copy. InfraPath is the resolved,
// project-relative infra directory (azure.yaml infra.path or the configured
// override); it is copied whole.
type ReviewRequest struct {
	RootDir     string
	OutDir      string
	AzureYAML   string
	InfraPath   string
	Entries     []Entry
	Profile     string
	ToolVersion string
	WriteDiff   bool
}

type ReviewResult struct {
	Manifest Manifest
	// Skipped lists project files left out of the review copy, each with the
	// reason, for example a credential-like file or one over the size limit.
	Skipped []string
}

const (
	// maxFileBytes and maxTreeBytes bound what is copied. A review copy is for
	// reading Bicep, so an infra tree holding large binaries is copied without
	// them instead of being read into memory whole.
	maxFileBytes = 8 << 20
	maxTreeBytes = 256 << 20
)

// copyStats collects what the copy left out and how much it wrote.
type copyStats struct {
	skipped []string
	total   int64
}

// skipReason says why a file is not copied into the review tree, or "".
// The review tree sits outside the source's ignore rules, so credential-like
// files are never duplicated into it. A module that loads one fails the
// recompile of the copy, and the run stops with that error.
func skipReason(rel string, size int64) string {
	base := strings.ToLower(path.Base(rel))
	switch ext := path.Ext(base); {
	case strings.HasPrefix(base, ".env"),
		strings.Contains(base, ".tfstate"),
		base == "id_rsa" || base == "id_ed25519",
		ext == ".pem" || ext == ".pfx" || ext == ".p12" || ext == ".key" || ext == ".jks" || ext == ".keystore":
		return "credential-like file"
	}
	if size > maxFileBytes {
		return fmt.Sprintf("larger than %d MiB", maxFileBytes>>20)
	}
	return ""
}

// copiedFile is one input copied into the review tree. ReviewRel differs from
// Rel only for azd project files, which are renamed so azd cannot discover
// the copy as a project.
type copiedFile struct {
	Rel       string
	ReviewRel string
	Perm      fs.FileMode
}

// WriteReview writes a generated review tree rooted at OutDir. OutDir must be a
// repo-relative path ending in ".review". An existing OutDir is replaced only
// when it is empty or was written by this tool. On error the partial tree is
// removed.
func WriteReview(ctx context.Context, req ReviewRequest) (res ReviewResult, err error) {
	if err := ctx.Err(); err != nil {
		return ReviewResult{}, err
	}
	outRel, err := safeOutputPath(req.OutDir)
	if err != nil {
		return ReviewResult{}, err
	}
	inputs := []string{req.AzureYAML, req.InfraPath}
	if err := validateOutputPlacement(outRel, inputs...); err != nil {
		return ReviewResult{}, err
	}
	root, err := os.OpenRoot(req.RootDir)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	if err := ensureNoSymlinkComponents(root, outRel); err != nil {
		return ReviewResult{}, err
	}
	if err := checkIdentityOverlap(root, outRel, inputs); err != nil {
		return ReviewResult{}, err
	}
	if err := resetReviewDir(root, outRel); err != nil {
		return ReviewResult{}, err
	}
	defer func() {
		if err != nil {
			_ = root.RemoveAll(outRel)
		}
	}()

	stats := &copyStats{}
	files, err := copyInputs(ctx, root, outRel, req.AzureYAML, req.InfraPath, stats)
	if err != nil {
		return ReviewResult{}, err
	}
	copied := make(map[string]bool, len(files))
	for _, f := range files {
		copied[f.Rel] = true
	}
	yamlComments := map[string][]yaml.Comment{}
	bicepComments := map[string][]bicepcomments.Comment{}
	entries := make([]Entry, 0, len(req.Entries))
	for _, e := range req.Entries {
		if e.Inline && !copied[e.File] {
			e.Inline = false
			if e.InlineReason == "" {
				e.InlineReason = "file-not-copied"
			}
		}
		entries = append(entries, e)
		if !e.Inline {
			continue
		}
		switch strings.ToLower(path.Ext(e.File)) {
		case ".yaml", ".yml":
			yamlComments[e.File] = append(yamlComments[e.File], yaml.Comment{Line: e.Line, Text: e.ReviewComment()})
		case ".bicep":
			bicepComments[e.File] = append(bicepComments[e.File], bicepcomments.Comment{Line: e.Line, Text: e.ReviewComment()})
		}
	}

	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Tool:          toolName,
		ToolVersion:   req.ToolVersion,
		Profile:       req.Profile,
		ReviewDir:     outRel,
		Files:         []ManifestFile{},
		Annotations:   append([]Entry{}, entries...),
	}
	if req.WriteDiff {
		manifest.DiffPath = path.Join(outRel, diffName)
	}
	var diffParts [][]byte
	for _, f := range files {
		staged := path.Join(outRel, f.ReviewRel)
		before, err := root.ReadFile(staged)
		if err != nil {
			return ReviewResult{}, fmt.Errorf("read staged file %s: %w", f.Rel, err)
		}
		updated := before
		commentStyle := ""
		count := 0
		ext := strings.ToLower(path.Ext(f.Rel))
		switch {
		case (ext == ".yaml" || ext == ".yml") && (len(yamlComments[f.Rel]) > 0 || f.ReviewRel != f.Rel):
			// Every renamed azd project file carries the banner, with or
			// without annotations.
			updated, err = yaml.Apply(before, yamlComments[f.Rel])
			commentStyle, count = "yaml", len(yamlComments[f.Rel])
		case ext == ".bicep":
			// Every copied Bicep file carries the banner; the copy is then
			// recompiled and compared with the original.
			updated, err = bicepcomments.Apply(before, bicepComments[f.Rel])
			commentStyle, count = "bicep", len(bicepComments[f.Rel])
		}
		if err != nil {
			return ReviewResult{}, err
		}
		if commentStyle == "" {
			continue
		}
		if err := root.WriteFile(staged, updated, f.Perm); err != nil {
			return ReviewResult{}, fmt.Errorf("write annotated file %s: %w", f.Rel, err)
		}
		manifest.Files = append(manifest.Files, ManifestFile{
			Path:            f.Rel,
			ReviewPath:      staged,
			CommentStyle:    commentStyle,
			AnnotationCount: count,
		})
		if req.WriteDiff {
			diffParts = append(diffParts, diff.Unified(f.Rel, before, updated))
		}
	}
	manifest.Summary.Total = len(entries)
	for _, e := range entries {
		if e.Inline {
			manifest.Summary.Inline++
		} else {
			manifest.Summary.ManifestOnly++
		}
	}
	manifestData, err := manifest.JSON()
	if err != nil {
		return ReviewResult{}, fmt.Errorf("encode manifest: %w", err)
	}
	if err := writeNewFile(root, path.Join(outRel, manifestName), manifestData, manifestPerm); err != nil {
		return ReviewResult{}, fmt.Errorf("write manifest: %w", err)
	}
	if req.WriteDiff {
		data := joinDiffs(diffParts)
		if err := writeNewFile(root, path.Join(outRel, diffName), data, diffPerm); err != nil {
			return ReviewResult{}, fmt.Errorf("write diff: %w", err)
		}
	}
	// The tree holds copies of project files outside the source's ignore
	// rules, so it ignores itself in git.
	if err := writeNewFile(root, path.Join(outRel, ".gitignore"), []byte("*\n"), manifestPerm); err != nil {
		return ReviewResult{}, fmt.Errorf("write .gitignore: %w", err)
	}
	slices.Sort(stats.skipped)
	return ReviewResult{Manifest: manifest, Skipped: stats.skipped}, nil
}

func joinDiffs(parts [][]byte) []byte {
	var joined []byte
	for _, part := range parts {
		joined = append(joined, part...)
	}
	if len(joined) == 0 {
		return []byte{}
	}
	return joined
}

func safeOutputPath(out string) (string, error) {
	out = filepath.ToSlash(out)
	if out == "" {
		return "", fmt.Errorf("%w: --out is required", ErrUnsafeOutput)
	}
	if strings.HasPrefix(out, "/") || filepath.IsAbs(filepath.FromSlash(out)) || !filepath.IsLocal(filepath.FromSlash(out)) {
		return "", fmt.Errorf("%w: --out must stay inside the project directory", ErrUnsafeOutput)
	}
	out = path.Clean(out)
	if !strings.HasSuffix(path.Base(out), ".review") {
		return "", fmt.Errorf("%w: --out must end with .review", ErrUnsafeOutput)
	}
	return out, nil
}

// reviewName maps a copied path to its name in the review tree. azd finds a
// project by the file name azure.yaml, so azd project files are renamed to
// azure.review.yaml (azure.yml to azure.review.yml).
func reviewName(rel string) string {
	dir, base := path.Split(rel)
	switch strings.ToLower(base) {
	case "azure.yaml":
		return dir + "azure.review.yaml"
	case "azure.yml":
		return dir + "azure.review.yml"
	}
	return rel
}

func copyInputs(ctx context.Context, root *os.Root, outDir, azureYAML, infraPath string, stats *copyStats) ([]copiedFile, error) {
	var copied []copiedFile
	seen := map[string]bool{}
	if azureYAML != "" {
		azureYAML = path.Clean(filepath.ToSlash(azureYAML))
		f, err := copyFile(root, outDir, azureYAML)
		if err != nil {
			return nil, err
		}
		copied = append(copied, f)
		seen[azureYAML] = true
	}
	if infraPath != "" {
		infraPath = path.Clean(filepath.ToSlash(infraPath))
		if info, err := root.Lstat(infraPath); err == nil {
			if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				return nil, fmt.Errorf("infra path must not be a symlink or reparse point: %s", infraPath)
			}
			if info.IsDir() {
				files, err := copyTree(ctx, root, outDir, infraPath, seen, stats)
				if err != nil {
					return nil, err
				}
				copied = append(copied, files...)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("stat infra path %s: %w", infraPath, err)
		}
	}
	slices.SortFunc(copied, func(a, b copiedFile) int { return strings.Compare(a.Rel, b.Rel) })
	return copied, nil
}

func copyTree(ctx context.Context, root *os.Root, outDir, rel string, seen map[string]bool, stats *copyStats) ([]copiedFile, error) {
	var files []copiedFile
	err := fs.WalkDir(root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if isWithin(p, outDir) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return fmt.Errorf("infra review copy does not follow symlinks or reparse points: %s", p)
		}
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return root.MkdirAll(path.Join(outDir, p), info.Mode().Perm()|0o700)
		}
		if !d.Type().IsRegular() || seen[p] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if reason := skipReason(p, info.Size()); reason != "" {
			stats.skipped = append(stats.skipped, p+" ("+reason+")")
			return nil
		}
		if stats.total += info.Size(); stats.total > maxTreeBytes {
			return fmt.Errorf("infra review copy is larger than %d MiB; point infra.path at the Bicep sources only", maxTreeBytes>>20)
		}
		f, err := copyFile(root, outDir, p)
		if err != nil {
			return err
		}
		seen[p] = true
		files = append(files, f)
		return nil
	})
	return files, err
}

// isWithin reports whether p is dir or lies under it, case-folded on
// case-insensitive hosts.
func isWithin(p, dir string) bool {
	if foldCase() {
		p, dir = strings.ToLower(p), strings.ToLower(dir)
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// copyFile copies rel into the review tree. The copy is never more
// permissive than the source: group and other bits are kept as they are, and
// only owner read/write is added so the copy can be annotated.
func copyFile(root *os.Root, outDir, rel string) (copiedFile, error) {
	if strings.HasPrefix(rel, "../") || rel == ".." || strings.Contains(rel, "/../") {
		return copiedFile{}, fmt.Errorf("path escapes project: %s", rel)
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return copiedFile{}, fmt.Errorf("stat source file %s: %w", rel, err)
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return copiedFile{}, fmt.Errorf("source file must not be a symlink or reparse point: %s", rel)
	}
	if !info.Mode().IsRegular() {
		return copiedFile{}, fmt.Errorf("source file is not regular: %s", rel)
	}
	data, err := root.ReadFile(rel)
	if err != nil {
		return copiedFile{}, fmt.Errorf("read source file %s: %w", rel, err)
	}
	perm := info.Mode().Perm()&0o666 | 0o600
	reviewRel := reviewName(filepath.ToSlash(rel))
	target := path.Join(outDir, reviewRel)
	if err := root.MkdirAll(path.Dir(target), reviewDirPerm); err != nil {
		return copiedFile{}, fmt.Errorf("create review path %s: %w", rel, err)
	}
	if err := writeNewFile(root, target, data, perm); err != nil {
		return copiedFile{}, fmt.Errorf("write review file %s: %w", rel, err)
	}
	return copiedFile{Rel: rel, ReviewRel: reviewRel, Perm: perm}, nil
}

// MarshalSchema is used by tests and docs to emit the manifest schema.
func MarshalSchema(schema any) ([]byte, error) {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
