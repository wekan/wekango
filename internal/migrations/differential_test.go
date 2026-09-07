package migrations

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestSchemaSourceDifferential compares the complete runner to the actual WeKan
// JavaScript source, using separate databases on the same embedded FerretDB.
// The fixtures exercise all twelve steps, including BSON NaN repair and a
// missing filesystem attachment that must prevent a successful version stamp.
func TestSchemaSourceDifferential(t *testing.T) {
	source := os.Getenv("WEKAN_SOURCE_ROOT")
	if source == "" {
		t.Skip("source differential requires WEKAN_SOURCE_ROOT pointing to a WeKan checkout with node_modules/mongodb")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(absolute, "server/lib/schemaUpgradeSteps.js")); err != nil {
		t.Fatalf("WEKAN_SOURCE_ROOT: %v", err)
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	binary, err := exec.LookPath(node)
	if err != nil {
		t.Fatalf("source differential Node unavailable: %v", err)
	}
	for _, scenario := range []string{"clean", "unknown-kind", "unresolved-filesystem"} {
		t.Run(scenario, func(t *testing.T) {
			runSchemaSourceDifferential(t, binary, absolute, scenario)
		})
	}
}

func differentialNode(t *testing.T, ctx context.Context, binary, source string, stdin any, args ...string) json.RawMessage {
	t.Helper()
	command := exec.CommandContext(ctx, binary, append([]string{"testdata/schema-differential.cjs"}, args...)...)
	command.Env = append(os.Environ(), "WEKAN_SOURCE_ROOT="+source, "WEKAN_FORCE_SCHEMA_UPGRADE=false")
	if stdin != nil {
		data, err := json.Marshal(stdin)
		if err != nil {
			t.Fatal(err)
		}
		command.Stdin = bytes.NewReader(data)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		t.Fatalf("source helper %v: %v\n%s", args, err, stderr.String())
	}
	if !json.Valid(out) {
		t.Fatalf("helper output is not JSON: %s", out)
	}
	return out
}

func differentialEqual(t *testing.T, want, got json.RawMessage) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		af, _ := json.MarshalIndent(a, "", "  ")
		bf, _ := json.MarshalIndent(b, "", "  ")
		t.Fatalf("WeKan JavaScript and Go differ\nsource:\n%s\nGo:\n%s", af, bf)
	}
}

func runSchemaSourceDifferential(t *testing.T, binary, source, scenario string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	server, err := database.Start(ctx, database.Config{Directory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := mongo.Connect(options.Client().ApplyURI(server.URI()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	const goName = "go_migrations"
	const sourceName = "source_migrations"
	writable := t.TempDir()
	differentialNode(t, ctx, binary, source, nil, "seed", server.URI(), goName, sourceName, scenario, writable)
	want := differentialNode(t, ctx, binary, source, nil, "source", server.URI(), sourceName, writable)
	differentialAssertSource(t, want, scenario)
	runner := NewSchemaRunner(FilesystemOptions{WritablePath: writable})
	var reports []json.RawMessage
	for _, opts := range []UpgradeOptions{{AppVersion: "differential-v1"}, {AppVersion: "differential-v1"}, {AppVersion: "differential-v1", Force: true}, {AppVersion: "differential-v2"}} {
		result, err := runner.Run(ctx, client.Database(goName), opts)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, differentialNode(t, ctx, binary, source, map[string]any{"result": result, "state": runner.Snapshot()}, "dump", server.URI(), goName))
	}
	got, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	differentialEqual(t, want, got)
}

// Reject vacuous parity where both implementations merely fail or skip work.
func differentialAssertSource(t *testing.T, report json.RawMessage, scenario string) {
	t.Helper()
	var runs []struct {
		Result struct {
			Ran, Skipped []string
			Gated        bool
			Results      map[string]struct {
				Fixed, Unresolved int64
				Error             string
			}
		}
		State struct {
			Steps map[string]struct{ Status string }
		}
		Documents map[string][]map[string]any
	}
	if err := json.Unmarshal(report, &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 {
		t.Fatalf("source returned %d runs", len(runs))
	}
	for i, run := range runs {
		if len(run.Result.Ran)+len(run.Result.Skipped) != 12 || len(run.State.Steps) != 12 {
			t.Fatalf("source run %d did not execute/check twelve steps: %+v", i, run.Result)
		}
		for name, result := range run.Result.Results {
			if result.Error != "" {
				t.Fatalf("source %s failed: %s", name, result.Error)
			}
		}
	}
	expectedRuns := 11
	if scenario == "unresolved-filesystem" {
		expectedRuns = 12
	}
	if len(runs[0].Result.Ran) != expectedRuns {
		t.Fatalf("fixture should exercise %d writes: %+v", expectedRuns, runs[0].Result)
	}
	if runs[1].Result.Gated != (scenario != "unresolved-filesystem") {
		t.Fatalf("unexpected %s version gate: %+v", scenario, runs[1].Result)
	}
	if scenario == "unresolved-filesystem" && runs[0].Result.Results["fs-path-heal"].Unresolved != 1 {
		t.Fatal("missing file did not preserve unresolved work")
	}
	if runs[0].Result.Results["nonfinite-sort-repair"].Fixed != 1 {
		t.Fatal("BSON NaN was not repaired")
	}
	if scenario == "unknown-kind" {
		found := false
		for _, doc := range runs[3].Documents["attachments"] {
			if doc["_id"] == "unknown-kind" {
				found = true
				if len(doc) != 1 {
					t.Fatalf("unknown kind was guessed: %v", doc)
				}
			}
		}
		if !found {
			t.Fatal("unknown kind disappeared")
		}
	}
	for _, marker := range runs[3].Documents["_wekan_migration"] {
		if marker["_id"] != "schema-upgrade" {
			continue
		}
		last, ok := marker["lastCheck"].(map[string]any)
		if !ok {
			t.Fatal("schema lastCheck disappeared")
		}
		expected := "differential-v2"
		if scenario == "unresolved-filesystem" {
			expected = "old-version"
		}
		if last["version"] != expected {
			t.Fatalf("schema version = %v, want %s", last["version"], expected)
		}
		return
	}
	t.Fatal("schema marker disappeared")
}
