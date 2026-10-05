package azdspike

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

const pinnedProjectsExtension = "1.0.0-beta.13"

type syntheticTransition string

const (
	transitionSynthesize      syntheticTransition = "synthesize-embedded-template"
	transitionExistingProject syntheticTransition = "use-embedded-existing-project-template"
	transitionOnDisk          syntheticTransition = "delegate-to-on-disk-infrastructure"
	transitionUnsupported     syntheticTransition = "unsupported-fail-safe"
)

// syntheticBoundary is a test oracle for the provider boundary recorded at the
// pinned Microsoft source revision. It deliberately does not pretend to be a
// schema validator or production implementation.
func syntheticBoundary(src []byte, projectRoot string) (syntheticTransition, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(src, &document); err != nil {
		return transitionUnsupported, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return transitionUnsupported, fmt.Errorf("invalid azure.yaml root")
	}
	root := document.Content[0]
	infra := child(root, "infra")
	if infra == nil || child(infra, "provider") == nil || child(infra, "provider").Value != "microsoft.foundry" {
		return transitionUnsupported, fmt.Errorf("unsupported provisioning provider")
	}
	required := child(root, "requiredVersions")
	extensions := child(required, "extensions")
	version := child(extensions, "azure.ai.projects")
	if version == nil || version.Value != pinnedProjectsExtension {
		return transitionUnsupported, fmt.Errorf("unsupported azure.ai.projects version")
	}

	infraPath, module := "infra", "main"
	if value := child(infra, "path"); value != nil {
		infraPath = value.Value
	}
	if value := child(infra, "module"); value != nil {
		module = value.Value
	}
	for _, extension := range []string{".bicep", ".bicepparam"} {
		if _, err := os.Stat(filepath.Join(projectRoot, infraPath, module+extension)); err == nil {
			return transitionOnDisk, nil
		} else if !os.IsNotExist(err) {
			return transitionUnsupported, fmt.Errorf("cannot inspect on-disk infrastructure: %w", err)
		}
	}

	services := child(root, "services")
	if services != nil {
		for i := 0; i+1 < len(services.Content); i += 2 {
			service := services.Content[i+1]
			host := child(service, "host")
			if host != nil && host.Value == "azure.ai.project" && child(service, "endpoint") != nil {
				return transitionExistingProject, nil
			}
		}
	}
	return transitionSynthesize, nil
}

func TestMicrosoftFoundrySyntheticBoundary(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("valid-azure-yaml-only", "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	projectRoot, err := filepath.Abs("valid-azure-yaml-only")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"*.bicep", "*.bicepparam", "infra/*.bicep", "infra/*.bicepparam"} {
		matches, err := filepath.Glob(filepath.Join(projectRoot, pattern))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("azure.yaml-only fixture unexpectedly has on-disk infrastructure: %v", matches)
		}
	}
	got, err := syntheticBoundary(src, projectRoot)
	if err != nil || got != transitionSynthesize {
		t.Fatalf("boundary = %q, %v; want %q without on-disk Bicep", got, err, transitionSynthesize)
	}
}

func TestSyntheticBoundaryPrecedenceAndFailSafe(t *testing.T) {
	base := `name: boundary
requiredVersions:
  extensions:
    azure.ai.projects: "1.0.0-beta.13"
infra:
  provider: microsoft.foundry
services:
  project:
    host: azure.ai.project
%s`
	root := t.TempDir()
	endpoint := fmt.Sprintf(base, "    endpoint: https://example.invalid/projects/fake\n")
	got, err := syntheticBoundary([]byte(endpoint), root)
	if err != nil || got != transitionExistingProject {
		t.Fatalf("endpoint boundary = %q, %v; want %q", got, err, transitionExistingProject)
	}

	if err := os.Mkdir(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "main.bicep"), []byte("// test-only precedence marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = syntheticBoundary([]byte(endpoint), root)
	if err != nil || got != transitionOnDisk {
		t.Fatalf("on-disk boundary = %q, %v; want %q to take precedence over endpoint", got, err, transitionOnDisk)
	}

	cases := []struct {
		name   string
		source string
	}{
		{
			"unknown provider",
			stringReplaceOnce(endpoint, "provider: microsoft.foundry", "provider: microsoft.future"),
		},
		{
			"unknown extension version",
			stringReplaceOnce(endpoint, `azure.ai.projects: "1.0.0-beta.13"`, `azure.ai.projects: "9.9.9"`),
		},
		{
			"missing extension version",
			stringReplaceOnce(endpoint, "extensions:\n    azure.ai.projects: \"1.0.0-beta.13\"", "extensions: {}"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := syntheticBoundary([]byte(tc.source), t.TempDir())
			if err == nil || got != transitionUnsupported {
				t.Fatalf("deliberately broken input returned %q, %v; want an explicit fail-safe error", got, err)
			}
		})
	}
}

func stringReplaceOnce(source, old, replacement string) string {
	for i := 0; i+len(old) <= len(source); i++ {
		if source[i:i+len(old)] == old {
			return source[:i] + replacement + source[i+len(old):]
		}
	}
	return source
}
