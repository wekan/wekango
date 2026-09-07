package migrations

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
)

// Handler serves the same public, read-only progress dashboard and ?json state
// as server/startupSchemaUpgrade.js. It reads a snapshot, so a slow HTTP client
// cannot hold the migration lock or observe a partially updated state.
func (r *Runner) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		state := r.Snapshot()
		w.Header().Set("Cache-Control", "no-store")
		var body []byte
		var err error
		if req.URL.Query().Has("json") {
			w.Header().Set("Content-Type", "application/json")
			body, err = json.MarshalIndent(state, "", "  ")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			var buffer bytes.Buffer
			rows := make([]dashboardRow, 0, len(state.Steps))
			seen := map[string]bool{}
			add := func(name string) {
				step, ok := state.Steps[name]
				if !ok || seen[name] {
					return
				}
				seen[name] = true
				color := "#aaa"
				switch step.Status {
				case "error":
					color = "#f77"
				case "running", "checking":
					color = "#fb7"
				case "done":
					color = "#7f7"
				}
				rows = append(rows, dashboardRow{Name: name, StepState: step, Color: color})
			}
			for _, step := range r.steps {
				add(step.Name)
			}
			// Snapshot-only/custom runner states retain stable rows without depending on
			// Go map iteration; the normal registry always supplies migration order.
			rest := make([]string, 0, len(state.Steps))
			for name := range state.Steps {
				if !seen[name] {
					rest = append(rest, name)
				}
			}
			sort.Strings(rest)
			for _, name := range rest {
				add(name)
			}
			if state.Product == "" {
				state.Product = "WeKan"
			}
			displayVersion := state.AppVersion
			if displayVersion == "" {
				displayVersion = "?"
			}
			err = upgradeDashboard.Execute(&buffer, struct {
				State
				Rows           []dashboardRow
				DisplayVersion string
			}{state, rows, displayVersion})
			body = buffer.Bytes()
		}
		if err != nil {
			http.Error(w, "could not render upgrade status", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if req.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	})
}

type dashboardRow struct {
	Name string
	StepState
	Color string
}

var upgradeDashboard = template.Must(template.New("schema-upgrade").Parse(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">{{if .Running}}<meta http-equiv="refresh" content="3">{{end}}
<title>{{.Product}} Schema Upgrade</title>
<style>body{font-family:monospace;background:#111;color:#ddd;padding:1em 2em}h1{color:#7bf}
table{border-collapse:collapse}td,th{padding:4px 10px;border-bottom:1px solid #333;text-align:left}
th{color:#aaa;font-size:.85em}.fail{color:#f77}</style></head><body>
<h1>{{.Product}} Schema Upgrade</h1>
<p>Status: <strong>{{if .Running}}Checking / migrating… ({{.CurrentStep}}){{else if .Gated}}Already re-checked for version {{.AppVersion}} — nothing to do{{else if .FinishedAt}}Completed{{else}}Not started yet{{end}}</strong></p>
<p style="color:#aaa">WeKan version: {{.DisplayVersion}}
{{if .LastCheck}} &nbsp;·&nbsp; previous re-check: {{.LastCheck.Version}} at {{.LastCheck.At}}{{end}}
{{if .StartedAt}} &nbsp;·&nbsp; started: {{.StartedAt}}{{end}}
{{if .FinishedAt}} &nbsp;·&nbsp; finished: {{.FinishedAt}}{{end}}</p>
<table><tr><th>Step</th><th>Status</th><th>Fixed</th><th>Unresolved</th><th>Error</th></tr>{{range .Rows}}<tr><td>{{.Name}}</td><td style="color:{{.Color}}">{{.Status}}</td><td>{{.Fixed}}</td><td class="{{if .Unresolved}}fail{{end}}">{{.Unresolved}}</td><td>{{.Error}}</td></tr>{{end}}</table>
<p style="color:#aaa">A re-check is mandatory only after a new {{.Product}} release; while the version
is unchanged, startup costs a single database read. Raw state: <a href="?json" style="color:#7bf">JSON</a></p>
</body></html>`))
