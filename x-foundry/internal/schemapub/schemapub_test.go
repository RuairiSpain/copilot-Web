package schemapub_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/schemapub"
)

const root = "../.."

func TestPublishedFilesAreCurrent(t *testing.T) {
	files, err := schemapub.Render(root)
	if err != nil {
		t.Fatal(err)
	}
	if stale := schemapub.Stale(root, files); len(stale) > 0 {
		t.Fatalf("stale files %v: run `go run ./tools/schemagen`", stale)
	}
	for _, want := range []string{"x-foundry.schema.json", "versions/1.0/x-foundry.schema.json", "azure-yaml-x-foundry.schema.json", "examples/enterprise.yaml"} {
		if want == "examples/enterprise.yaml" {
			continue // enterprise is deliberately not part of the published schema examples
		}
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s", want)
		}
	}
}

func TestWriteAndStaleDetection(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join(root, "schemas", "x-foundry.schema.json"))
	_ = os.WriteFile(filepath.Join(dir, "schemas", "x-foundry.schema.json"), src, 0o644)
	if err := os.MkdirAll(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range schemapub.PublishedExamples {
		b, _ := os.ReadFile(filepath.Join(root, "examples", name+".yaml"))
		_ = os.WriteFile(filepath.Join(dir, "examples", name+".yaml"), b, 0o644)
	}
	files, err := schemapub.Render(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(schemapub.Stale(dir, files)) == 0 {
		t.Fatal("nothing has been written yet")
	}
	if err := schemapub.Write(dir, files); err != nil {
		t.Fatal(err)
	}
	if stale := schemapub.Stale(dir, files); len(stale) != 0 {
		t.Fatalf("stale after write: %v", stale)
	}
	_ = os.WriteFile(filepath.Join(dir, "schemas", "azure-yaml-x-foundry.schema.json"), []byte("{}"), 0o644)
	if stale := schemapub.Stale(dir, files); len(stale) != 1 {
		t.Fatalf("stale = %v", stale)
	}
}

func TestRenderErrors(t *testing.T) {
	if _, err := schemapub.Render(t.TempDir()); err == nil {
		t.Fatal("a missing schema is an error")
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "schemas"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "schemas", "x-foundry.schema.json"), []byte(`{"$id":"urn:nope","$defs":{}}`), 0o644)
	if _, err := schemapub.Render(dir); err == nil {
		t.Fatal("an unexpected $id is an error")
	}
	_ = os.WriteFile(filepath.Join(dir, "schemas", "x-foundry.schema.json"), []byte(`not json`), 0o644)
	if _, err := schemapub.Render(dir); err == nil {
		t.Fatal("invalid JSON is an error")
	}
}
