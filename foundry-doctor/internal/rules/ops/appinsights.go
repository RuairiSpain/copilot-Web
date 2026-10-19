package ops

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FND-OPS-002 reads three verified shapes from the compiled ARM template:
//   - Microsoft.CognitiveServices/accounts/projects (and accounts) connections
//     whose properties.category is AppInsights (ConnectionCategory enumeration
//     and ConnectionPropertiesV2.category in the 2026-09-01 cognitiveservices.json);
//   - Microsoft.Insights/components properties.WorkspaceResourceId (the
//     workspace-based component link, the same property FND-OPS-006 reads).
//
// The connection's target and metadata are never read: the component a
// connection points to cannot be established from verified sources, so the
// rule only checks that a connection of the category exists. IngestionMode and
// APPLICATIONINSIGHTS_CONNECTION_STRING on linked compute are UNVERIFIED as
// pass or fail signals and are not used.
const (
	typeProject      = "Microsoft.CognitiveServices/accounts/projects"
	typeProjectConn  = "Microsoft.CognitiveServices/accounts/projects/connections"
	typeAccountConn  = "Microsoft.CognitiveServices/accounts/connections"
	typeAppInsights  = "Microsoft.Insights/components"
	categoryAppInsig = "AppInsights"
)

func eval002(in *sdk.Input, _ Providers) sdk.Result {
	if in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": compiled ARM model unavailable"}}
	}
	o := &outcome{}
	v := newView(in)
	projects := v.of(o, typeProject)
	if len(projects) == 0 {
		if len(o.notes) > 0 {
			return o.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipNotApplicable + ": no Foundry project in the compiled template"}}
	}

	type scoped struct {
		res      sdk.ARMResource
		segments []string
	}
	var projConns, acctConns []scoped
	// A conditional or looped connection may be the AppInsights connection.
	anyUnknownConn := v.hasUncertain(typeProjectConn, typeAccountConn)
	for _, c := range v.of(o, typeProjectConn, typeAccountConn) {
		cat, _ := c.Properties["category"].(string)
		if isExpr(cat) {
			anyUnknownConn = true
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(cat), categoryAppInsig) {
			continue
		}
		s := scoped{res: c, segments: strings.Split(c.Name, "/")}
		if strings.EqualFold(c.Type, typeProjectConn) {
			projConns = append(projConns, s)
		} else {
			acctConns = append(acctConns, s)
		}
	}

	connectedPerProject := map[string]bool{}
	for _, p := range projects {
		segs := strings.Split(p.Name, "/")
		if isExpr(p.Name) || len(segs) != 2 {
			o.notes = append(o.notes, p.Name+": project name is unresolved, so connections cannot be correlated")
			continue
		}
		var atProject, atAccount int
		unknown := anyUnknownConn
		for _, c := range projConns {
			switch {
			case isExpr(c.res.Name) || len(c.segments) != 3:
				unknown = true
			case strings.EqualFold(c.segments[0], segs[0]) && strings.EqualFold(c.segments[1], segs[1]):
				atProject++
			}
		}
		for _, c := range acctConns {
			switch {
			case isExpr(c.res.Name) || len(c.segments) != 2:
				unknown = true
			case strings.EqualFold(c.segments[0], segs[0]):
				atAccount++
			}
		}
		switch {
		case atProject > 1:
			o.addKey(p, "multiple-appinsights-connections", "the project has more than one AppInsights category connection; only one connection per category is allowed")
			connectedPerProject[strings.ToLower(p.Name)] = true
		case atAccount > 1:
			o.addKey(p, "multiple-appinsights-connections", "the parent account has more than one AppInsights category connection; only one connection per category is allowed")
			connectedPerProject[strings.ToLower(p.Name)] = true
		case atProject+atAccount > 0:
			connectedPerProject[strings.ToLower(p.Name)] = true
		case unknown:
			o.notes = append(o.notes, p.Name+": an unresolved connection name or category may be an AppInsights connection")
		default:
			o.addKey(p, "no-appinsights-connection", "the project has no connection of category AppInsights at project or parent account scope, so server-side tracing is not enabled")
		}
	}

	// Workspace-based check, only for a template with a connected project: a
	// connection is the reason this component matters. The connection target is
	// not read, so every component in the template is checked.
	if len(connectedPerProject) > 0 {
		for _, c := range v.of(o, typeAppInsights) {
			ws, present := lookupFold(c.Properties, "WorkspaceResourceId")
			s, _ := ws.(string)
			switch {
			case present && isExpr(s):
				o.notes = append(o.notes, c.Name+": Application Insights WorkspaceResourceId is unresolved")
			case !present || strings.TrimSpace(s) == "":
				o.addKey(c, "component-not-workspace-based", "the Application Insights component has no WorkspaceResourceId, so it is not linked to a Log Analytics workspace")
			}
		}
	}
	return o.result()
}

func lookupFold(m map[string]any, key string) (any, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}
