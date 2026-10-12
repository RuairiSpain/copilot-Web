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
}

// WriteReview writes a generated review tree rooted at OutDir. OutDir must be a
// repo-relative path ending in ".review".
func WriteReview(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return ReviewResult{}, err
	}
	outRel, err := safeOutputPath(req.OutDir)
	if err != nil {
		return ReviewResult{}, err
	}
	if err := validateOutputPlacement(outRel, req.AzureYAML, req.InfraPath); err != nil {
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
	if err := root.RemoveAll(outRel); err != nil {
		return ReviewResult{}, fmt.Errorf("reset review directory: %w", err)
	}
	if err := root.MkdirAll(outRel, 0o755); err != nil {
		return ReviewResult{}, fmt.Errorf("create review directory: %w", err)
	}

	files, err := copyInputs(ctx, root, outRel, req.AzureYAML, req.InfraPath)
	if err != nil {
		return ReviewResult{}, err
	}
	copied := make(map[string]bool, len(files))
	for _, rel := range files {
		copied[rel] = true
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
		Tool:          "foundry-doctor",
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
	for _, rel := range files {
		var (
			updated []byte
			before  []byte
		)
		staged := path.Join(outRel, rel)
		before, err = root.ReadFile(staged)
		if err != nil {
			return ReviewResult{}, fmt.Errorf("read staged file %s: %w", rel, err)
		}
		updated = before
		commentStyle := ""
		if cs := yamlComments[rel]; len(cs) > 0 {
			updated, err = yaml.Apply(before, cs)
			commentStyle = "yaml"
		} else if cs := bicepComments[rel]; len(cs) > 0 {
			updated, err = bicepcomments.Apply(before, cs)
			commentStyle = "bicep"
		}
		if err != nil {
			return ReviewResult{}, err
		}
		if commentStyle != "" {
			if err := root.WriteFile(staged, updated, 0o644); err != nil {
				return ReviewResult{}, fmt.Errorf("write annotated file %s: %w", rel, err)
			}
			manifest.Files = append(manifest.Files, ManifestFile{
				Path:            rel,
				ReviewPath:      path.Join(outRel, rel),
				CommentStyle:    commentStyle,
				AnnotationCount: len(yamlComments[rel]) + len(bicepComments[rel]),
			})
			if req.WriteDiff {
				diffParts = append(diffParts, diff.Unified(rel, before, updated))
			}
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
	if err := root.WriteFile(path.Join(outRel, manifestName), manifestData, 0o644); err != nil {
		return ReviewResult{}, fmt.Errorf("write manifest: %w", err)
	}
	if req.WriteDiff {
		data := joinDiffs(diffParts)
		if err := root.WriteFile(path.Join(outRel, diffName), data, 0o644); err != nil {
			return ReviewResult{}, fmt.Errorf("write diff: %w", err)
		}
	}
	return ReviewResult{Manifest: manifest}, nil
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
		return "", fmt.Errorf("--out is required")
	}
	if strings.HasPrefix(out, "/") || filepath.IsAbs(filepath.FromSlash(out)) || !filepath.IsLocal(filepath.FromSlash(out)) {
		return "", fmt.Errorf("--out must stay inside the project directory")
	}
	out = path.Clean(out)
	if !strings.HasSuffix(path.Base(out), ".review") {
		return "", fmt.Errorf("--out must end with .review")
	}
	return out, nil
}

func validateOutputPlacement(outRel, azureYAML, infraPath string) error {
	for _, parent := range []string{azureYAML, infraPath} {
		parent = path.Clean(filepath.ToSlash(parent))
		if parent == "" {
			continue
		}
		if pathsOverlap(outRel, parent) {
			return fmt.Errorf("--out must not be nested under copied inputs")
		}
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	a = path.Clean(filepath.ToSlash(a))
	b = path.Clean(filepath.ToSlash(b))
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func copyInputs(ctx context.Context, root *os.Root, outDir, azureYAML, infraPath string) ([]string, error) {
	var copied []string
	if azureYAML != "" {
		if err := copyFile(root, outDir, azureYAML); err != nil {
			return nil, err
		}
		copied = append(copied, azureYAML)
	}
	if infraPath != "" {
		if info, err := root.Lstat(infraPath); err == nil {
			if info.Mode()&fs.ModeSymlink != 0 {
				return nil, fmt.Errorf("infra path must not be a symlink: %s", infraPath)
			}
			files, err := copyTree(ctx, root, outDir, infraPath)
			if err != nil {
				return nil, err
			}
			copied = append(copied, files...)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("stat infra path %s: %w", infraPath, err)
		}
	}
	slices.Sort(copied)
	return copied, nil
}

func copyTree(ctx context.Context, root *os.Root, outDir, rel string) ([]string, error) {
	var files []string
	err := fs.WalkDir(root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == outDir || strings.HasPrefix(p, outDir+"/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("infra review copy does not follow symlinks: %s", p)
		}
		if d.IsDir() {
			return root.MkdirAll(path.Join(outDir, p), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := copyFile(root, outDir, p); err != nil {
			return err
		}
		files = append(files, p)
		return nil
	})
	return files, err
}

func ensureNoSymlinkComponents(root *os.Root, rel string) error {
	current := ""
	for _, component := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("stat review path %s: %w", current, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("review output must not traverse symlinks: %s", current)
		}
		if !info.IsDir() && current != rel {
			return fmt.Errorf("review output parent is not a directory: %s", current)
		}
	}
	return nil
}

func copyFile(root *os.Root, outDir, rel string) error {
	if strings.HasPrefix(rel, "../") || rel == ".." || strings.Contains(rel, "/../") {
		return fmt.Errorf("path escapes project: %s", rel)
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return fmt.Errorf("stat source file %s: %w", rel, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("source file must not be a symlink: %s", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source file is not regular: %s", rel)
	}
	data, err := root.ReadFile(rel)
	if err != nil {
		return fmt.Errorf("read source file %s: %w", rel, err)
	}
	target := path.Join(outDir, filepath.ToSlash(rel))
	if err := root.MkdirAll(path.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create review path %s: %w", rel, err)
	}
	if err := root.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("write review file %s: %w", rel, err)
	}
	return nil
}

// MarshalSchema is used by tests and docs to emit the manifest schema.
func MarshalSchema(schema any) ([]byte, error) {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
