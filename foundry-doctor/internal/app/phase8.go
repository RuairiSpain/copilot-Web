package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/gw"
	iqrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/iq"
	runtimeiq "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/iq"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func phase8Rules() []sdk.Rule {
	var out []sdk.Rule
	out = append(out, iqrules.Register()...)
	out = append(out, gw.Register()...)
	return out
}

type iqSnapshotModel struct {
	sdk.ARMModel
	snap runtimeiq.Snapshot
}

func (m iqSnapshotModel) IQSnapshot() runtimeiq.Snapshot { return m.snap }

func (m iqSnapshotModel) Outputs() []sdk.ARMOutput {
	if out, ok := m.ARMModel.(sdk.ARMOutputs); ok {
		return out.Outputs()
	}
	return nil
}

func withPhase8Snapshot(ctx context.Context, in RunInput) sdk.ARMModel {
	if in.Local || in.ARM == nil || in.AzureYAML == nil {
		return in.ARM
	}
	client := runtimeiq.HTTPClient{
		HTTP:       &http.Client{Timeout: 20 * time.Second},
		Credential: azure.NewDefaultCredential(nil),
	}
	snap := buildPhase8Snapshot(ctx, in, client)
	if len(snap.Bases) == 0 && len(snap.Sources) == 0 && len(snap.Indexes) == 0 && len(snap.Skillsets) == 0 && len(snap.ProbeIssues) == 0 {
		return in.ARM
	}
	return iqSnapshotModel{ARMModel: in.ARM, snap: snap}
}

func buildPhase8Snapshot(ctx context.Context, in RunInput, client runtimeiq.Client) runtimeiq.Snapshot {
	connections := runtimeiq.ConnectionsFromAzureYAML(&sdk.Input{AzureYAML: in.AzureYAML})
	snap := runtimeiq.Snapshot{
		Connections: connections,
		Bases:       map[string]runtimeiq.KnowledgeBase{},
		Sources:     map[string]runtimeiq.KnowledgeSource{},
		Indexes:     map[string]runtimeiq.Index{},
		Skillsets:   map[string]runtimeiq.Skillset{},
	}
	for _, conn := range connections {
		if conn.TargetKind != runtimeiq.ConnectionTargetKnowledgeBaseMCP {
			continue
		}
		apiVersion := conn.APIVersion
		if apiVersion == "" {
			snap.ProbeIssues = append(snap.ProbeIssues, "knowledge base MCP connection "+conn.Name+" does not pin api-version")
			continue
		}
		kb, err := client.GetKnowledgeBase(ctx, conn.SearchService, conn.KnowledgeBase, apiVersion)
		if err != nil {
			snap.ProbeIssues = append(snap.ProbeIssues, "knowledge base metadata could not be read for connection "+conn.Name)
			continue
		}
		kb.Location = conn.Location
		for _, sourceName := range kb.KnowledgeSources {
			source, err := client.GetKnowledgeSource(ctx, conn.SearchService, sourceName, apiVersion)
			if err != nil {
				snap.ProbeIssues = append(snap.ProbeIssues, "knowledge source metadata could not be read for "+conn.Name+"/"+sourceName)
				continue
			}
			source.Location = conn.Location
			snap.Sources[strings.ToLower(source.Name)] = source
			if source.SearchIndexName != "" {
				idx, err := client.GetIndex(ctx, conn.SearchService, source.SearchIndexName, apiVersion)
				if err != nil {
					snap.ProbeIssues = append(snap.ProbeIssues, "index metadata could not be read for "+conn.Name+"/"+source.SearchIndexName)
				} else {
					idx.Location = conn.Location
					snap.Indexes[strings.ToLower(idx.Name)] = idx
				}
			}
			if source.SkillsetName != "" {
				ss, err := client.GetSkillset(ctx, conn.SearchService, source.SkillsetName, apiVersion)
				if err != nil {
					snap.ProbeIssues = append(snap.ProbeIssues, "skillset metadata could not be read for "+conn.Name+"/"+source.SkillsetName)
				} else {
					ss.Location = conn.Location
					snap.Skillsets[strings.ToLower(ss.Name)] = ss
				}
			}
			switch strings.ToLower(source.Kind) {
			case "web":
				kb.UsesWebKnowledgeSource = true
			case "searchindex", "azureblob", "indexedonelake", "indexedsharepoint", "indexedsql":
				kb.UsesIndexedKnowledge = true
			}
		}
		snap.Bases[strings.ToLower(conn.SearchService+"/"+kb.Name)] = kb
	}
	slices.Sort(snap.ProbeIssues)
	snap.ProbeIssues = dedupeStrings(snap.ProbeIssues)
	return snap
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:1]
	for _, item := range in[1:] {
		if item != out[len(out)-1] {
			out = append(out, item)
		}
	}
	return out
}
