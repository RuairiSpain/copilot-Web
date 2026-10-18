// Package html renders static, script-free assessment HTML pages: a one-page
// owner summary and a developer detail page.
package html

import (
	"fmt"
	"html/template"
	"io"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const layout = `{{define "head"}}<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root { color-scheme: light dark; --bg: #ffffff; --fg: #1b1b1b; --muted: #4b5563; --border: #d1d5db; --pass: #0b6a0b; --fail: #9f1239; --warn: #92400e; --other: #374151; --link: #0645ad; }
    @media (prefers-color-scheme: dark) {
      :root { --bg: #111827; --fg: #f3f4f6; --muted: #d1d5db; --border: #4b5563; --pass: #86efac; --fail: #fda4af; --warn: #fcd34d; --other: #e5e7eb; --link: #93c5fd; }
    }
    body { font-family: Segoe UI, Arial, sans-serif; margin: 2rem; background: var(--bg); color: var(--fg); line-height: 1.5; }
    a { color: var(--link); }
    h1,h2,h3 { line-height: 1.2; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 1rem; }
    caption { text-align: left; font-weight: 600; padding-bottom: .25rem; }
    th, td { border: 1px solid var(--border); padding: .5rem; text-align: left; vertical-align: top; }
    pre { overflow-x: auto; border: 1px solid var(--border); padding: .5rem; }
    .muted { color: var(--muted); }
    .state-PASS { color: var(--pass); } .state-FAIL { color: var(--fail); } .state-WARNING { color: var(--warn); }
    .state-UNKNOWN, .state-QUESTION, .state-SKIPPED, .state-INFO, .state-ATTESTED, .state-NOT_APPLICABLE { color: var(--other); }
  </style>
</head>
<body>
  <header>
    <h1>{{.Title}}</h1>
    <p class="muted">Static HTML report. No script is embedded. This report does not claim complete WAF compliance and is not a certification.</p>
  </header>
  <main>
    <section aria-labelledby="summary">
      <h2 id="summary">Summary</h2>
      <p><strong>Profile:</strong> {{.Profile}}</p>
      <p><strong>Decision:</strong> {{.Decision}}</p>
      {{if .ScopeNote}}<p><strong>Assessed scope:</strong> {{.ScopeNote}}</p>{{end}}
      <table>
        <caption>Controls by state</caption>
        <thead><tr><th scope="col">State</th><th scope="col">Count</th></tr></thead>
        <tbody>
          {{range .Counts}}<tr><th scope="row" class="state-{{.State}}">{{.State}}</th><td>{{.Count}}</td></tr>
          {{end}}
        </tbody>
      </table>
    </section>
{{end}}
{{define "prov"}}{{if .ProjectOpinion}}<strong>Project opinion</strong>, not Microsoft guidance{{if .SourceURL}}; context source: <a href="{{.SourceURL}}">{{.SourceURL}}</a>{{end}}{{else if .SourceURL}}Source: <a href="{{.SourceURL}}">{{.SourceURL}}</a>{{else}}No source recorded; treat as project opinion{{end}}{{if and .SourceURL .LastVerified}} (verified {{.LastVerified}}){{end}}{{end}}
{{define "cost"}}{{.CostBasis}} (<a href="{{.CostSourceURL}}">{{.CostSourceURL}}</a>{{if .CostVerified}}, verified {{.CostVerified}}{{end}}){{end}}
{{define "foot"}}
    <section aria-labelledby="limitations">
      <h2 id="limitations">Limitations</h2>
      <ul>
        {{range .Limitations}}<li>{{.}}</li>
        {{end}}
      </ul>
    </section>
  </main>
</body>
</html>
{{end}}`

const ownerPage = `{{template "head" .}}
    <section aria-labelledby="top-actions">
      <h2 id="top-actions">Top five actions</h2>
      {{if .TopActions}}<ol>
        {{range .TopActions}}<li><strong>{{.Title}}</strong> (<span class="state-{{.State}}">{{.State}}</span>, {{pillar .Pillar}}, {{if .Mandatory}}mandatory{{else}}optional{{end}}): {{.Recommendation}} {{template "prov" .}}.</li>
        {{end}}
      </ol>{{else}}<p>No immediate actions in the assessed scope.</p>{{end}}
    </section>
    <section aria-labelledby="costly">
      <h2 id="costly">Costly recommendations (optional)</h2>
      {{if .Costly}}<p>These optional controls add Azure cost. They are kept out of the top actions.</p>
      <ul>
        {{range .Costly}}<li><strong>{{.Title}}</strong> (<span class="state-{{.State}}">{{.State}}</span>, {{pillar .Pillar}}): {{.Recommendation}} Cost basis: {{template "cost" .}}. {{template "prov" .}}.</li>
        {{end}}
      </ul>{{else}}<p>None outstanding.</p>{{end}}
    </section>
    {{if .Attested}}<section aria-labelledby="attested">
      <h2 id="attested">Team-attested answers</h2>
      <p>Recorded by the team in Foundry Doctor configuration, including settings delegated to Azure Policy. Foundry Doctor did not verify them. A mandatory control that rests on an attestation keeps the decision at "Proceed with conditions".</p>
      <ul>
        {{range .Attested}}<li>{{.Title}} (<span class="state-{{.State}}">{{.State}}</span>, {{if .Mandatory}}mandatory{{else}}optional{{end}}): {{if .QuestionAnswer}}{{.QuestionAnswer}}{{else}}{{.StateReason}}{{end}}</li>
        {{end}}
      </ul>
    </section>{{end}}
    <section aria-labelledby="unassessed">
      <h2 id="unassessed">Unassessed scope</h2>
      {{if .Unassessed}}<ul>
        {{range .Unassessed}}<li>{{.Title}} (<span class="state-{{.State}}">{{.State}}</span>, {{pillar .Pillar}}){{if .StateReason}}: {{.StateReason}}{{end}}</li>
        {{end}}
      </ul>{{else}}<p>None.</p>{{end}}
    </section>
{{template "foot" .}}`

const developerPage = `{{template "head" .}}
    {{range .Groups}}<section aria-labelledby="pillar-{{.Key}}">
      <h2 id="pillar-{{.Key}}">{{.Name}}</h2>
      {{range .Controls}}<article aria-labelledby="control-{{.RuleID}}">
        <h3 id="control-{{.RuleID}}">{{.Title}} — <span class="state-{{.State}}">{{.State}}</span></h3>
        <dl>
          <dt>Rule</dt><dd>{{.RuleID}}{{if .RuleVersion}} (version {{.RuleVersion}}){{end}}</dd>
          <dt>Mandatory</dt><dd>{{.Mandatory}}</dd>
          {{if .Basis}}<dt>Evidence basis</dt><dd>{{.Basis}}</dd>{{end}}
          {{if .StateReason}}<dt>Why this state</dt><dd>{{.StateReason}}</dd>{{end}}
          <dt>Costly recommendation</dt><dd>{{if .Costly}}{{template "cost" .}}{{else}}false{{end}}</dd>
          <dt>Recommendation basis</dt><dd>{{template "prov" .}}</dd>
          {{if .QuestionPrompt}}<dt>Review question</dt><dd>{{.QuestionPrompt}}</dd>{{end}}
          {{if .QuestionAnswer}}<dt>Recorded answer (team attestation, not verified)</dt><dd>{{.QuestionAnswer}}</dd>{{end}}
          {{if .WARAReferences}}<dt>WARA references (recommendations, not platform facts)</dt><dd><ul>{{range .WARAReferences}}<li><a href="{{.SourceURL}}">{{.ID}}</a> {{.Label}} (pgVerified: {{.PGVerified}})</li>{{end}}</ul></dd>{{end}}
          {{if .ExternalIDs}}<dt>Overlapping external checks (not ingested)</dt><dd>{{range $i, $e := .ExternalIDs}}{{if $i}}, {{end}}{{$e}}{{end}}</dd>{{end}}
          {{if .Recommendation}}<dt>Recommendation</dt><dd>{{.Recommendation}}</dd>{{end}}
          {{if .Fix}}<dt>Fix</dt><dd><pre><code>{{.Fix}}</code></pre></dd>{{end}}
          <dt>Evidence</dt><dd>{{if or .Findings .Skipped}}<ul>
            {{range .Findings}}<li>{{if .Severity}}{{.Severity}}{{else}}finding{{end}}{{if .Suppressed}}, suppressed ({{.Suppressed.Reason}}; owner {{.Suppressed.Owner}}{{if .Suppressed.Expires}}; expires {{.Suppressed.Expires}}{{end}}){{else if .Baselined}}, baselined{{end}}: {{.Evidence}} at <code>{{loc .Location}}</code></li>
            {{end}}{{range .Skipped}}<li>skipped: {{.Reason}}</li>
            {{end}}</ul>{{else}}No finding or skip record was present.{{end}}</dd>
        </dl>
      </article>
      {{end}}
    </section>
    {{end}}
{{template "foot" .}}`

var (
	funcs = template.FuncMap{"pillar": assess.PillarName, "loc": loc}
	owner = template.Must(template.Must(template.New("owner").Funcs(funcs).Parse(layout)).Parse(ownerPage))
	dev   = template.Must(template.Must(template.New("developer").Funcs(funcs).Parse(layout)).Parse(developerPage))
)

type stateCount struct {
	State assess.State
	Count int
}

type group struct {
	Key      string
	Name     string
	Controls []assess.Control
}

type view struct {
	assess.Assessment
	Title    string
	Counts   []stateCount
	Groups   []group
	Attested []assess.Control
}

func newView(a assess.Assessment, title string) view {
	v := view{Assessment: a, Title: title}
	for _, s := range assess.States {
		v.Counts = append(v.Counts, stateCount{State: s, Count: a.Summary.States[s]})
	}
	for _, p := range assess.Pillars {
		g := group{Key: p, Name: assess.PillarName(p)}
		for _, c := range a.Controls {
			if c.Pillar == p {
				g.Controls = append(g.Controls, c)
			}
		}
		if len(g.Controls) != 0 {
			v.Groups = append(v.Groups, g)
		}
	}
	for _, c := range a.Controls {
		if c.Basis == assess.BasisAttested {
			v.Attested = append(v.Attested, c)
		}
	}
	return v
}

// RenderOwner writes the one-page owner summary.
func RenderOwner(w io.Writer, a assess.Assessment) error {
	return owner.Execute(w, newView(a, "Foundry Doctor WAF owner summary"))
}

// RenderDeveloper writes the developer detail page.
func RenderDeveloper(w io.Writer, a assess.Assessment) error {
	return dev.Execute(w, newView(a, "Foundry Doctor WAF developer detail"))
}

func loc(l sdk.Location) string {
	switch {
	case l.File == "":
		return "unknown"
	case l.Line == 0:
		return l.File
	case l.Column == 0:
		return fmt.Sprintf("%s:%d", l.File, l.Line)
	}
	return fmt.Sprintf("%s:%d:%d", l.File, l.Line, l.Column)
}
