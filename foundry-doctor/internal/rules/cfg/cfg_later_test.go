package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type cfgARM struct{ rs []sdk.ARMResource }

func (m cfgARM) Resources() []sdk.ARMResource { return m.rs }

func TestCFG008To010(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		yaml     string
		arm      sdk.ARMModel
		wantFind int
		wantSkip bool
		contains string
	}{
		{
			name:     "cfg008 empty allow list",
			id:       "FND-CFG-008",
			yaml:     "services:\n  a:\n    host: azure.ai.agent\n    tools:\n      - type: mcp\n        allowed_tools: []\n",
			wantFind: 1, contains: "empty allowed_tools",
		},
		{
			name:     "cfg008 omit allow list uncertain",
			id:       "FND-CFG-008",
			yaml:     "services:\n  a:\n    host: azure.ai.agent\n    tools:\n      - type: mcp\n",
			wantSkip: true,
		},
		{
			name:     "cfg009 every minute",
			id:       "FND-CFG-009",
			yaml:     "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      bad:\n        type: recurring\n        cron_expression: \"* * * * *\"\n        time_zone: UTC\n",
			wantFind: 1, contains: "five minutes",
		},
		{
			name:     "cfg009 missing timezone uncertain",
			id:       "FND-CFG-009",
			yaml:     "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      bad:\n        type: recurring\n        cron_expression: \"0 7 * * 1-5\"\n",
			wantSkip: true,
		},
		{
			name: "cfg009 valid schedule",
			id:   "FND-CFG-009",
			yaml: "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      ok:\n        type: recurring\n        cron_expression: \"0/5 * * * *\"\n        time_zone: UTC\n",
		},
		{
			name:     "cfg010 latest yaml model",
			id:       "FND-CFG-010",
			yaml:     "services:\n  p:\n    host: azure.ai.project\n    deployments:\n      - name: dep\n        model:\n          format: OpenAI\n          name: gpt-4.1\n          version: latest\n",
			wantFind: 1, contains: "unpinned",
		},
		{
			name:     "cfg010 arm missing upgrade option",
			id:       "FND-CFG-010",
			yaml:     "services: {}\n",
			arm:      cfgARM{[]sdk.ARMResource{{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/chat", Properties: map[string]any{"model": map[string]any{"version": "2026-03-17"}}}}},
			wantFind: 1, contains: "versionUpgradeOption is absent",
		},
		{
			name: "cfg010 pinned arm deployment",
			id:   "FND-CFG-010",
			yaml: "services: {}\n",
			arm:  cfgARM{[]sdk.ARMResource{{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/chat", Properties: map[string]any{"model": map[string]any{"version": "2026-03-17"}, "versionUpgradeOption": "NoAutoUpgrade"}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := find(Register(), tc.id)
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, tc.yaml), ARM: tc.arm})
			if (res.Skipped != nil) != tc.wantSkip {
				t.Fatalf("skipped=%v want %v", res.Skipped, tc.wantSkip)
			}
			if len(res.Findings) != tc.wantFind {
				t.Fatalf("findings=%d want %d: %+v", len(res.Findings), tc.wantFind, res.Findings)
			}
			if tc.contains != "" && tc.wantFind > 0 && !strings.Contains(res.Findings[0].Evidence, tc.contains) {
				t.Fatalf("evidence %q lacks %q", res.Findings[0].Evidence, tc.contains)
			}
		})
	}
}
