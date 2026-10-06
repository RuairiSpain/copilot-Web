package app

import (
	"context"
	"net/http"
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
	if in.ARM == nil || in.AzureYAML == nil {
		return in.ARM
	}
	connections := runtimeiq.ConnectionsFromAzureYAML(&sdk.Input{AzureYAML: in.AzureYAML})
	if len(connections) == 0 {
		return in.ARM
	}
	client := runtimeiq.HTTPClient{
		HTTP:       &http.Client{Timeout: 20 * time.Second},
		Credential: azure.NewDefaultCredential(nil),
	}
	snap := runtimeiq.Snapshot{
		Connections: connections,
		Bases:       map[string]runtimeiq.KnowledgeBase{},
		Sources:     map[string]runtimeiq.KnowledgeSource{},
		Indexes:     map[string]runtimeiq.Index{},
		Skillsets:   map[string]runtimeiq.Skillset{},
	}
	for _, conn := range connections {
		kb, err := client.GetKnowledgeBase(ctx, conn.SearchService, conn.KnowledgeBase)
		if err != nil {
			continue
		}
		kb.Location = conn.Location
		for _, sourceName := range kb.KnowledgeSources {
			source, err := client.GetKnowledgeSource(ctx, conn.SearchService, sourceName)
			if err != nil {
				continue
			}
			source.Location = conn.Location
			snap.Sources[strings.ToLower(source.Name)] = source
			if source.SearchIndexName != "" {
				if idx, err := client.GetIndex(ctx, conn.SearchService, source.SearchIndexName); err == nil {
					idx.Location = conn.Location
					snap.Indexes[strings.ToLower(idx.Name)] = idx
				}
			}
			if source.SkillsetName != "" {
				if ss, err := client.GetSkillset(ctx, conn.SearchService, source.SkillsetName); err == nil {
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
	if len(snap.Bases) == 0 && len(snap.Sources) == 0 && len(snap.Indexes) == 0 && len(snap.Skillsets) == 0 {
		return in.ARM
	}
	return iqSnapshotModel{ARMModel: in.ARM, snap: snap}
}
