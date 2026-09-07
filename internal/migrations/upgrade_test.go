package migrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestSchemaFullRegistryAndVersionGate(t *testing.T) {
	ctx, db := migrationDB(t)
	r := NewSchemaRunner(FilesystemOptions{WritablePath: t.TempDir()})
	names := []string{}
	for _, step := range r.steps {
		names = append(names, step.Name)
	}
	want := []string{"archived-flag-backfill", "swimlane-structure", "merge-per-swimlane-lists", "checklist-items-embedded", "customfields-boardIds", "board-allows-defaults", "attachment-kind-flags", "board-members-isactive", "board-permission-lowercase", "nonfinite-sort-repair", "checklist-minicard-unset", "fs-path-heal"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("registry %v", names)
	}
	if _, err := db.Collection("settings").InsertOne(ctx, bson.M{"_id": "setting", "productName": "  Custom Kanban  "}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("checklists").InsertOne(ctx, bson.M{"_id": "legacy", "showChecklistAtMinicard": false}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
	if err != nil || out.Gated {
		t.Fatalf("first run %#v %v", out, err)
	}
	s := r.Snapshot()
	if s.Running || s.FinishedAt == nil || s.CurrentStep != "" || s.Product != "Custom Kanban" || s.LastCheck == nil || s.LastCheck.Version != "9.01" {
		t.Fatalf("state %#v", s)
	}
	for name, step := range s.Steps {
		if step.Status == "error" {
			t.Fatalf("%s failed: %s", name, step.Error)
		}
	}
	var marker struct {
		LastCheck LastCheck `bson:"lastCheck"`
		Steps     bson.M    `bson:"steps"`
	}
	if err := db.Collection(MarkerCollection).FindOne(ctx, bson.M{"_id": MarkerID}).Decode(&marker); err != nil {
		t.Fatal(err)
	}
	if marker.LastCheck.Version != "9.01" || marker.LastCheck.At.IsZero() || marker.Steps[checklistMarker] == nil {
		t.Fatalf("marker %#v", marker)
	}
	if _, err := db.Collection("checklists").InsertOne(ctx, bson.M{"_id": "choice", "showChecklistAtMinicard": false}); err != nil {
		t.Fatal(err)
	}
	// A new runner represents a process restart; the gate is persisted in SQLite.
	restarted := NewSchemaRunner(FilesystemOptions{WritablePath: t.TempDir()})
	out, err = restarted.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
	if err != nil || !out.Gated || len(out.Skipped) != 12 || len(out.Ran) != 0 {
		t.Fatalf("gate %#v %v", out, err)
	}
	out, err = restarted.Run(ctx, db, UpgradeOptions{AppVersion: "9.01", Force: true})
	if err != nil || out.Gated {
		t.Fatalf("forced %#v %v", out, err)
	}
	var choice bson.M
	if err := db.Collection("checklists").FindOne(ctx, bson.M{"_id": "choice"}).Decode(&choice); err != nil {
		t.Fatal(err)
	}
	if choice["showChecklistAtMinicard"] != false {
		t.Fatalf("user choice erased: %#v", choice)
	}
	out, err = restarted.Run(ctx, db, UpgradeOptions{AppVersion: "9.02"})
	if err != nil || out.Gated || restarted.Snapshot().LastCheck.Version != "9.02" {
		t.Fatalf("release recheck %#v %v", out, err)
	}
	// Snapshots may be mutated by callers without corrupting the live dashboard.
	s = restarted.Snapshot()
	s.Steps[checklistMarker] = StepState{Status: "corrupt"}
	s.LastCheck.Version = "corrupt"
	if restarted.Snapshot().LastCheck.Version == "corrupt" || restarted.Snapshot().Steps[checklistMarker].Status == "corrupt" {
		t.Fatal("mutable snapshot")
	}
}

func TestSchemaUnresolvedFilesystemRetries(t *testing.T) {
	ctx, db := migrationDB(t)
	root := t.TempDir()
	if _, err := db.Collection("attachments").InsertOne(ctx, bson.M{"_id": "missing", "name": "file.txt", "versions": bson.M{"original": bson.M{"storage": "fs", "path": filepath.Join(root, "missing-volume", "file.txt")}}}); err != nil {
		t.Fatal(err)
	}
	r := NewSchemaRunner(FilesystemOptions{WritablePath: root})
	out, err := r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
	if err != nil || out.Gated || r.Snapshot().LastCheck != nil {
		t.Fatalf("unresolved stamped: %#v %v state=%#v", out, err, r.Snapshot())
	}
	if r.Snapshot().Steps["fs-path-heal"].Unresolved != 1 {
		t.Fatalf("missing count: %#v", out)
	}
	path := filepath.Join(root, "files", "attachments", "missing")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("restored"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
	if err != nil || out.Gated || r.Snapshot().LastCheck == nil || r.Snapshot().Steps["fs-path-heal"].Fixed != 1 {
		t.Fatalf("retry: %#v %v %#v", out, err, r.Snapshot())
	}
}

func TestSchemaFailureContinuesWithoutVersionStamp(t *testing.T) {
	ctx, db := migrationDB(t)
	called := false
	r := newRunner([]Step{
		{"failure", func(context.Context, *mongo.Database) (bool, error) { return false, errors.New("fixture failure") }, func(context.Context, *mongo.Database) (Result, error) {
			t.Fatal("run after failed check")
			return Result{}, nil
		}},
		{"later", func(context.Context, *mongo.Database) (bool, error) { called = true; return false, nil }, func(context.Context, *mongo.Database) (Result, error) { return Result{}, nil }},
	})
	out, err := r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
	if err != nil || !called || r.Snapshot().LastCheck != nil || r.Snapshot().Running || len(out.Skipped) != 1 || r.Snapshot().Steps["failure"].Error != "fixture failure" {
		t.Fatalf("failure behavior %#v %v", out, err)
	}
	if err := db.Collection(MarkerCollection).FindOne(ctx, bson.M{"_id": MarkerID}).Err(); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("failed version stamped: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Run(canceled, db, UpgradeOptions{AppVersion: "9.01"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestSchemaConcurrentReadersAndSerializedRerun(t *testing.T) {
	ctx, db := migrationDB(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	r := newRunner([]Step{{"controlled", func(context.Context, *mongo.Database) (bool, error) { return true, nil }, func(ctx context.Context, _ *mongo.Database) (Result, error) {
		close(entered)
		select {
		case <-release:
			return Result{Fixed: 1}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}}})
	done := make(chan error, 2)
	go func() { _, err := r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"}); done <- err }()
	<-entered
	for i := 0; i < 20; i++ {
		s := r.Snapshot()
		if !s.Running || s.CurrentStep != "controlled" || s.Steps["controlled"].Status != "running" {
			t.Fatalf("live state %#v", s)
		}
	}
	go func() {
		out, err := r.Run(ctx, db, UpgradeOptions{AppVersion: "9.01"})
		if err == nil && !out.Gated {
			err = errors.New("concurrent run bypassed persisted gate")
		}
		done <- err
	}()
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
