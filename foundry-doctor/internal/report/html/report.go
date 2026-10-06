// Package html renders a static, script-free assessment HTML page.
package html

import (
	"html/template"
	"io"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
)

var page = template.Must(template.New("assessment").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Foundry Doctor WAF assessment</title>
  <style>
    :root { color-scheme: light dark; --bg: #ffffff; --fg: #1b1b1b; --muted: #4b5563; --border: #d1d5db; }
    body { font-family: Segoe UI, Arial, sans-serif; margin: 2rem; background: var(--bg); color: var(--fg); line-height: 1.5; }
    h1,h2,h3 { line-height: 1.2; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 1rem; }
    th, td { border: 1px solid var(--border); padding: .5rem; text-align: left; vertical-align: top; }
    .muted { color: var(--muted); }
    .state-PASS { color: #0b6a0b; } .state-FAIL { color: #9f1239; } .state-WARNING { color: #92400e; }
    .state-UNKNOWN, .state-QUESTION, .state-SKIPPED { color: #374151; }
  </style>
</head>
<body>
  <header>
    <h1>Foundry Doctor WAF assessment</h1>
    <p class="muted">Static HTML report. No script is embedded. This report does not claim complete WAF compliance.</p>
  </header>
  <main>
    <section aria-labelledby="summary">
      <h2 id="summary">Summary</h2>
      <p><strong>Profile:</strong> {{.Profile}}</p>
      <p><strong>Decision:</strong> {{.Decision}}</p>
    </section>
    <section aria-labelledby="controls">
      <h2 id="controls">Controls</h2>
      <table>
        <thead><tr><th>Control</th><th>Pillar</th><th>State</th><th>Recommendation</th></tr></thead>
        <tbody>
          {{range .Controls}}
          <tr>
            <td>{{.Title}}</td>
            <td>{{.Pillar}}</td>
            <td class="state-{{.State}}">{{.State}}</td>
            <td>{{.Recommendation}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </section>
  </main>
</body>
</html>`))

// Render writes the HTML page.
func Render(w io.Writer, a assess.Assessment) error { return page.Execute(w, a) }
