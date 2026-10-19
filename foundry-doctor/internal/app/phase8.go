package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/gw"
	iqrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/iq"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
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

// The optional ARM model interfaces that rules assert on (template variables,
// expression-valued tags, default-derived parameters) are forwarded, because
// embedding sdk.ARMModel hides everything but its own methods.

// Variable forwards armmodel.Model.Variable.
func (m iqSnapshotModel) Variable(name string) (any, bool) {
	if v, ok := m.ARMModel.(interface{ Variable(string) (any, bool) }); ok {
		return v.Variable(name)
	}
	return nil, false
}

// TagsAreExpression forwards armmodel.Model.TagsAreExpression.
func (m iqSnapshotModel) TagsAreExpression(r sdk.ARMResource) bool {
	if v, ok := m.ARMModel.(interface{ TagsAreExpression(sdk.ARMResource) bool }); ok {
		return v.TagsAreExpression(r)
	}
	return false
}

// FromDefault forwards armmodel.Model.FromDefault.
func (m iqSnapshotModel) FromDefault(r sdk.ARMResource) []string {
	if v, ok := m.ARMModel.(interface {
		FromDefault(sdk.ARMResource) []string
	}); ok {
		return v.FromDefault(r)
	}
	return nil
}

// FromDefaultPaths forwards armmodel.Model.FromDefaultPaths.
func (m iqSnapshotModel) FromDefaultPaths(r sdk.ARMResource) []string {
	if v, ok := m.ARMModel.(interface {
		FromDefaultPaths(sdk.ARMResource) []string
	}); ok {
		return v.FromDefaultPaths(r)
	}
	return nil
}

// Bounds of the opt-in Foundry IQ metadata probe (ADR-028): the whole probe
// shares one time budget, each request has its own timeout, and the number of
// objects read is capped.
const (
	phase8ProbeBudget     = 60 * time.Second
	phase8RequestTimeout  = 20 * time.Second
	phase8MaxBases        = 8
	phase8MaxSourcesPerKB = 32
)

// withPhase8Snapshot attaches the Foundry IQ data-plane snapshot to the ARM
// model. The probe is opt-in (RunInput.IQMetadataProbe, doctor --iq-metadata)
// and never runs with --local. Without it the IQ rules see only azure.yaml and
// ARM metadata and skip every data-plane leg.
func withPhase8Snapshot(ctx context.Context, in RunInput) sdk.ARMModel {
	if in.Local || !in.IQMetadataProbe || in.ARM == nil || in.AzureYAML == nil {
		return in.ARM
	}
	ctx, cancel := context.WithTimeout(ctx, phase8ProbeBudget)
	defer cancel()
	client := runtimeiq.HTTPClient{
		HTTP:       &http.Client{Timeout: phase8RequestTimeout},
		Credential: azure.NewDefaultCredential(nil),
	}
	snap := buildPhase8Snapshot(ctx, in, client)
	if !snap.LiveRead && len(snap.ProbeIssues) == 0 {
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
	bases := 0
	for _, conn := range connections {
		if conn.TargetKind != runtimeiq.ConnectionTargetKnowledgeBaseMCP {
			continue
		}
		if bases >= phase8MaxBases {
			snap.ProbeIssues = append(snap.ProbeIssues, fmt.Sprintf("more than %d knowledge base MCP connections; the rest were not read", phase8MaxBases))
			break
		}
		bases++
		apiVersion := conn.APIVersion
		if apiVersion == "" {
			snap.ProbeIssues = append(snap.ProbeIssues, "knowledge base MCP connection "+conn.Name+" does not pin api-version")
			continue
		}
		if !runtimeiq.KnownAPIVersion(apiVersion) {
			snap.ProbeIssues = append(snap.ProbeIssues, sdk.SkipUnsupportedVersion+": knowledge base MCP connection "+conn.Name+" uses api-version "+apiVersion+" outside the verified set ("+runtimeiq.KnownAPIVersionList+")")
			continue
		}
		snap.LiveRead = true
		kb, err := client.GetKnowledgeBase(ctx, conn.SearchService, conn.KnowledgeBase, apiVersion)
		if err != nil {
			snap.ProbeIssues = append(snap.ProbeIssues, phase8ProbeFailure("knowledge base", "connection "+conn.Name, err))
			continue
		}
		if kb.Name == "" {
			kb.Name = conn.KnowledgeBase
		}
		kb.Location = conn.Location
		for i, sourceName := range kb.KnowledgeSources {
			if i >= phase8MaxSourcesPerKB {
				snap.ProbeIssues = append(snap.ProbeIssues, fmt.Sprintf("knowledge base %s references more than %d knowledge sources; the rest were not read", kb.Name, phase8MaxSourcesPerKB))
				break
			}
			if ctx.Err() != nil {
				snap.ProbeIssues = append(snap.ProbeIssues, "knowledge source metadata for "+conn.Name+" was not read: probe time budget exhausted")
				break
			}
			source, err := client.GetKnowledgeSource(ctx, conn.SearchService, sourceName, apiVersion)
			if err != nil {
				snap.ProbeIssues = append(snap.ProbeIssues, phase8ProbeFailure("knowledge source", conn.Name+"/"+sourceName, err))
				continue
			}
			if source.Name == "" {
				source.Name = sourceName
			}
			source.Location = conn.Location
			snap.Sources[strings.ToLower(source.Name)] = source
			if idxName := source.BackingIndex(); idxName != "" {
				if _, done := snap.Indexes[strings.ToLower(idxName)]; !done {
					idx, err := client.GetIndex(ctx, conn.SearchService, idxName, apiVersion)
					if err != nil {
						snap.ProbeIssues = append(snap.ProbeIssues, phase8ProbeFailure("index", conn.Name+"/"+idxName, err))
					} else {
						if idx.Name == "" {
							idx.Name = idxName
						}
						idx.Location = conn.Location
						snap.Indexes[strings.ToLower(idx.Name)] = idx
					}
				}
			}
			if ssName := source.SkillsetName; ssName != "" {
				if _, done := snap.Skillsets[strings.ToLower(ssName)]; !done {
					ss, err := client.GetSkillset(ctx, conn.SearchService, ssName, apiVersion)
					if err != nil {
						snap.ProbeIssues = append(snap.ProbeIssues, phase8ProbeFailure("skillset", conn.Name+"/"+ssName, err))
					} else {
						if ss.Name == "" {
							ss.Name = ssName
						}
						ss.Location = conn.Location
						snap.Skillsets[strings.ToLower(ss.Name)] = ss
					}
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

// phase8ProbeFailure turns a probe error into a secret-free reason that keeps
// permission, not-found and budget failures apart.
func phase8ProbeFailure(object, target string, err error) string {
	var unavailable *azure.UnavailableError
	switch {
	case errors.As(err, &unavailable):
		return object + " metadata for " + target + " is not readable (" + unavailable.Reason + "; needs Search Service Contributor on the search service (Reader is not confirmed to be enough), which must allow role-based access)"
	case rt.IsNotFound(err):
		return object + " " + target + " was not found"
	case errors.Is(err, context.DeadlineExceeded):
		return object + " metadata for " + target + " was not read: probe time budget exhausted"
	default:
		return object + " metadata could not be read for " + target
	}
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
