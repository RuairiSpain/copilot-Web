//go:build spike

package azdspike

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPinnedAzdHasNoFoundryNamespaceCollision is the mandatory ADR-003
// release monitor. It checks both first-party registries and the pinned core
// command source; a collision must cause a release-blocking test failure.
func TestPinnedAzdHasNoFoundryNamespaceCollision(t *testing.T) {
	clone := os.Getenv("AZURE_DEV_DIR")
	if clone == "" || !filepath.IsAbs(clone) {
		spikeDependencyUnavailable(t,
			"AZURE_DEV_DIR must name the pinned Azure/azure-dev checkout for the ADR-003 collision monitor")
	}
	assertPinnedAzureDevCheckout(t, clone)

	for _, name := range []string{"registry.json", "registry.dev.json"} {
		path := filepath.Join(clone, "cli", "azd", "extensions", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ADR-003 requires pinned registry %s: %v", path, err)
		}
		var registry any
		if err := json.Unmarshal(raw, &registry); err != nil {
			t.Fatalf("invalid pinned registry %s: %v", path, err)
		}
		var collisions []string
		collectFoundryNamespaces(registry, "$", &collisions)
		if len(collisions) != 0 {
			t.Fatalf("azd registry claims reserved Foundry Doctor namespace: %s",
				strings.Join(collisions, ", "))
		}
	}

	assertNoFoundryCoreCommand(t, filepath.Join(clone, "cli", "azd", "cmd"))
}

func collectFoundryNamespaces(value any, location string, collisions *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childLocation := location + "." + key
			if strings.EqualFold(key, "namespace") {
				if namespace, ok := child.(string); ok {
					normalized := strings.ToLower(strings.TrimSpace(namespace))
					if normalized == "foundry" || strings.HasPrefix(normalized, "foundry.") {
						*collisions = append(*collisions,
							fmt.Sprintf("%s=%q", childLocation, namespace))
					}
				}
			}
			collectFoundryNamespaces(child, childLocation, collisions)
		}
	case []any:
		for index, child := range typed {
			collectFoundryNamespaces(child,
				location+"["+strconv.Itoa(index)+"]", collisions)
		}
	}
}

func assertNoFoundryCoreCommand(t *testing.T, commandRoot string) {
	t.Helper()
	info, err := os.Stat(commandRoot)
	if err != nil || !info.IsDir() {
		t.Fatalf("ADR-003 core-command source is unavailable at %s", commandRoot)
	}
	var collisions []string
	err = filepath.WalkDir(commandRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(commandRoot, path)
		if err != nil {
			return err
		}
		// A top-level command is represented by a first-level command package.
		if entry.IsDir() && relative != "." &&
			!strings.Contains(relative, string(filepath.Separator)) &&
			strings.EqualFold(entry.Name(), "foundry") {
			collisions = append(collisions, relative+string(filepath.Separator))
			return filepath.SkipDir
		}
		// Root command registration is in the files directly under cmd.
		if entry.IsDir() || filepath.Dir(relative) != "." ||
			!strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, literal := range strings.FieldsFunc(string(raw), func(r rune) bool {
			return r != '_' && r != '-' && r != '.' &&
				(r < '0' || r > '9') && (r < 'A' || r > 'Z') &&
				(r < 'a' || r > 'z')
		}) {
			if strings.EqualFold(literal, "foundry") {
				collisions = append(collisions, relative)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect pinned azd core commands: %v", err)
	}
	if len(collisions) != 0 {
		t.Fatalf("pinned azd core command surface now contains %q: %s",
			"foundry", strings.Join(collisions, ", "))
	}
}

func TestFoundryNamespaceCollisionDetector(t *testing.T) {
	var collisions []string
	collectFoundryNamespaces(map[string]any{
		"extensions": []any{
			map[string]any{"namespace": "ai.agent"},
			map[string]any{"namespace": "foundry.tools"},
		},
	}, "$", &collisions)
	if len(collisions) != 1 || !strings.Contains(collisions[0], "foundry.tools") {
		t.Fatalf("collision detector did not reject deliberately conflicting input: %v",
			collisions)
	}
}
