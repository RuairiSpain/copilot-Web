package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	runtimeiq "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/iq"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type phase8ARM struct{ resources []sdk.ARMResource }

func (m *phase8ARM) Resources() []sdk.ARMResource { return m.resources }

type fakeIQClient struct {
	kbCalls        int
	lastAPIVersion string
	kbErr          error
}

func (f *fakeIQClient) GetKnowledgeBase(_ context.Context, _, _ string, apiVersion string) (runtimeiq.KnowledgeBase, error) {
	f.kbCalls++
	f.lastAPIVersion = apiVersion
	if f.kbErr != nil {
		return runtimeiq.KnowledgeBase{}, f.kbErr
	}
	return runtimeiq.KnowledgeBase{Name: "sample-kb"}, nil
}

func (*fakeIQClient) GetKnowledgeSource(context.Context, string, string, string) (runtimeiq.KnowledgeSource, error) {
	return runtimeiq.KnowledgeSource{}, nil
}

func (*fakeIQClient) GetIndex(context.Context, string, string, string) (runtimeiq.Index, error) {
	return runtimeiq.Index{}, nil
}

func (*fakeIQClient) GetSkillset(context.Context, string, string, string) (runtimeiq.Skillset, error) {
	return runtimeiq.Skillset{}, nil
}

func testAzureYAML(t *testing.T, body string) sdk.AzureYAMLView {
	t.Helper()
	doc, err := azureyaml.Parse([]byte(body), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestWithPhase8SnapshotSkipsLocalMode(t *testing.T) {
	arm := &phase8ARM{}
	yaml := testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp
    authType: ProjectManagedIdentity
`)
	got := withPhase8Snapshot(context.Background(), RunInput{Local: true, ARM: arm, AzureYAML: yaml})
	if got != arm {
		t.Fatalf("expected original ARM model, got %#v", got)
	}
}

func TestBuildPhase8SnapshotRecordsProbeFailures(t *testing.T) {
	client := &fakeIQClient{kbErr: errors.New("boom")}
	snap := buildPhase8Snapshot(context.Background(), RunInput{
		AzureYAML: testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp?api-version=2026-08-01-preview
    authType: ProjectManagedIdentity
`),
	}, client)
	if client.kbCalls != 1 {
		t.Fatalf("knowledge base calls = %d", client.kbCalls)
	}
	if len(snap.ProbeIssues) != 1 || snap.ProbeIssues[0] != "knowledge base metadata could not be read for connection iq" {
		t.Fatalf("probe issues = %#v", snap.ProbeIssues)
	}
	if len(snap.Connections) != 1 || snap.Connections[0].TargetKind != runtimeiq.ConnectionTargetKnowledgeBaseMCP {
		t.Fatalf("connections = %+v", snap.Connections)
	}
}

func TestBuildPhase8SnapshotIgnoresServiceRootConnectionForLiveReads(t *testing.T) {
	client := &fakeIQClient{}
	snap := buildPhase8Snapshot(context.Background(), RunInput{
		AzureYAML: testAzureYAML(t, `name: sample
services:
  search:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net
    authType: ProjectManagedIdentity
`),
	}, client)
	if client.kbCalls != 0 {
		t.Fatalf("knowledge base calls = %d", client.kbCalls)
	}
	if len(snap.Connections) != 1 || snap.Connections[0].TargetKind != runtimeiq.ConnectionTargetCognitiveSearchTarget {
		t.Fatalf("connections = %+v", snap.Connections)
	}
	if len(snap.ProbeIssues) != 0 {
		t.Fatalf("unexpected probe issues: %+v", snap.ProbeIssues)
	}
}

func TestBuildPhase8SnapshotPreservesConnectionAPIVersion(t *testing.T) {
	client := &fakeIQClient{}
	_ = buildPhase8Snapshot(context.Background(), RunInput{
		AzureYAML: testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp?api-version=2026-04-01
    authType: ProjectManagedIdentity
`),
	}, client)
	if client.lastAPIVersion != "2026-04-01" {
		t.Fatalf("api version = %q", client.lastAPIVersion)
	}
}

func TestBuildPhase8SnapshotRequiresPinnedAPIVersion(t *testing.T) {
	client := &fakeIQClient{}
	snap := buildPhase8Snapshot(context.Background(), RunInput{
		AzureYAML: testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp
    authType: ProjectManagedIdentity
`),
	}, client)
	if client.kbCalls != 0 {
		t.Fatalf("knowledge base calls = %d", client.kbCalls)
	}
	if len(snap.ProbeIssues) != 1 || !strings.Contains(snap.ProbeIssues[0], "does not pin api-version") {
		t.Fatalf("probe issues = %#v", snap.ProbeIssues)
	}
}
