package migrations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDashboardHTMLProgressAndJSON(t *testing.T) {
	runner := newRunner([]Step{})
	handler := runner.Handler()
	request := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		return response
	}
	response := request("/schema-upgrade-status")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Not started yet") || !strings.Contains(response.Body.String(), "WeKan Schema Upgrade") || strings.Contains(response.Body.String(), `http-equiv="refresh"`) {
		t.Fatalf("initial dashboard %d %s", response.Code, response.Body.String())
	}
	started := "2026-09-07T12:00:00.000Z"
	runner.change(func(state *State) {
		*state = State{Running: true, Product: "My board service", AppVersion: "11.99", StartedAt: &started, CurrentStep: "fs-path-heal", LastCheck: &LastCheck{Version: "11.98", At: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}, Steps: map[string]StepState{
			"first": {Status: "done", Fixed: 7}, "fs-path-heal": {Status: "running", Unresolved: 2}, "failed": {Status: "error", Error: "disk unavailable"},
		}}
	})
	response = request("/schema-upgrade-status")
	html := response.Body.String()
	for _, want := range []string{"My board service Schema Upgrade", "Checking / migrating… (fs-path-heal)", `http-equiv="refresh" content="3"`, "previous re-check: 11.98", "started: " + started, "<td>7</td>", `class="fail">2</td>`, "disk unavailable", "#f77", "#fb7", "#7f7"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("HTML headers %v", response.Header())
	}
	for _, url := range []string{"/schema-upgrade-status?json", "/schema-upgrade-status?json=false"} {
		response = request(url)
		if response.Code != 200 || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("JSON response %d %v", response.Code, response.Header())
		}
		var state struct {
			Running     bool
			Product     string
			CurrentStep string
			StartedAt   string
			LastCheck   struct{ At string }
			Steps       map[string]StepState
		}
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if !state.Running || state.Product != "My board service" || state.CurrentStep != "fs-path-heal" || state.StartedAt != started || state.LastCheck.At != "2026-09-06T12:00:00.000Z" || state.Steps["first"].Fixed != 7 {
			t.Fatalf("JSON progress %#v", state)
		}
	}
	runner.change(func(state *State) { state.Running = false; state.FinishedAt = &started })
	response = request("/schema-upgrade-status")
	if !strings.Contains(response.Body.String(), "Completed") || strings.Contains(response.Body.String(), `http-equiv="refresh"`) {
		t.Fatal("finished dashboard still refreshes or lacks completed status")
	}
	runner.change(func(state *State) { state.Gated = true })
	if html := request("/schema-upgrade-status").Body.String(); !strings.Contains(html, "Already re-checked for version 11.99 — nothing to do") {
		t.Fatal(html)
	}
}

func TestDashboardEscapesEveryStateString(t *testing.T) {
	runner := newRunner([]Step{})
	attack := `<script>alert("&owned")</script>`
	runner.change(func(s *State) {
		*s = State{Running: true, Product: attack, AppVersion: attack, CurrentStep: attack, StartedAt: &attack, FinishedAt: &attack, LastCheck: &LastCheck{Version: attack}, Steps: map[string]StepState{attack: {Status: attack, Error: attack}}}
	})
	response := httptest.NewRecorder()
	runner.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/schema-upgrade-status", nil))
	html := response.Body.String()
	if strings.Contains(html, "<script>") || strings.Contains(html, "</script>") || strings.Count(html, "&lt;script&gt;") < 9 {
		t.Fatalf("unescaped state: %s", html)
	}
	response = httptest.NewRecorder()
	runner.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/schema-upgrade-status?json", nil))
	var decoded State
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Product != attack || decoded.Steps[attack].Error != attack {
		t.Fatal("JSON changed state string")
	}
}

func TestDashboardReadOnlyMethods(t *testing.T) {
	handler := newRunner([]Step{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/schema-upgrade-status", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST response %d %v", response.Code, response.Header())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/schema-upgrade-status", nil))
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD response %d %s", response.Code, response.Body.String())
	}
}
