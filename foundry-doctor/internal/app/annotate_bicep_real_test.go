//go:build spike

package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike -run TestRealBicepReviewCopyRecompiles ./internal/app/
//
// It annotates a project whose entry and module both raise no-unused-params
// warnings, next to a ”' multi-line string, with the real CLI, and relies on
// validateReviewCopy to prove the bannered and annotated copy gives the same
// compile status, ARM output (generator metadata aside) and diagnostics.
// Without BICEP_PATH it skips; REQUIRE_SPIKE_DEPS=1 turns that into a failure.
func TestRealBicepReviewCopyRecompiles(t *testing.T) {
	exe := os.Getenv("BICEP_PATH")
	if exe == "" {
		if os.Getenv("REQUIRE_SPIKE_DEPS") == "1" {
			t.Fatal("BICEP_PATH is required when REQUIRE_SPIKE_DEPS=1")
		}
		t.Skip("BICEP_PATH not set")
	}
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "param unusedParam string = 'x'\n"+
		"var description = '''\n"+
		"line one\n"+
		"line two\n"+
		"'''\n"+
		"module st './modules/st.bicep' = {\n"+
		"  name: 'st'\n"+
		"}\n"+
		"output d string = description\n")
	writeFile(t, dir, "infra/modules/st.bicep", "param unusedInModule string = 'y'\noutput ok bool = true\n")
	svc, _ := newSvc(&fakeEngine{})
	svc.ARM = armLoader{disc: bicep.DiscoverOptions{Path: exe}}
	svc.Project = fakeProject{src: Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "real.review", Dir: dir}, io.Discard); err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, rel := range []string{"real.review/infra/main.bicep", "real.review/infra/modules/st.bicep"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "// Foundry Doctor: no-unused-params") {
			t.Errorf("%s has no inline diagnostic comment:\n%s", rel, data)
		}
	}
}
