package migrations

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestFilesystemHistoricalLayouts(t *testing.T) {
	ctx, db := migrationDB(t)
	writable := t.TempDir()
	current := filepath.Join(writable, "files", "attachments")
	payload := []byte("historical attachment\x00bytes\xff")
	cases := []struct{ id, source, recorded, version string }{
		{"basename", filepath.Join(current, "old.bin"), `C:\missing\old.bin`, "original"},
		{"id", filepath.Join(current, "id"), "/missing/id.bin", "original"},
		{"named", filepath.Join(current, "named-original-document.bin"), "", "original"},
		{"import", filepath.Join(current, "import_document.bin"), "", "original"},
		{"uploads", filepath.Join(writable, "uploads", "attachments", "uploads"), "", "original"},
		{"uploads-base", filepath.Join(writable, "files", "uploads", "attachments", "former.bin"), "/missing/former.bin", "original"},
		{"cfs", filepath.Join(writable, "cfs-document.bin"), "", "original"},
		{"pre-files", filepath.Join(writable, "attachments", "pre-files"), "", "original"},
		{"prefix", filepath.Join(current, "prefix-original-renamed.bin"), "", "thumbnail"},
	}
	for _, tt := range cases {
		if err := os.MkdirAll(filepath.Dir(tt.source), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tt.source, payload, 0644); err != nil {
			t.Fatal(err)
		}
		_, err := db.Collection("attachments").InsertOne(ctx, bson.M{"_id": tt.id, "name": "document.bin", "path": "old-top-level", "versions": bson.M{tt.version: bson.M{"path": tt.recorded, "size": 99, "sha256": "preserve-metadata"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	opts := FilesystemOptions{WritablePath: writable}
	if needed, err := CheckFilesystemPaths(ctx, db, opts); err != nil || !needed {
		t.Fatalf("check %v %v", needed, err)
	}
	result, err := RunFilesystemPaths(ctx, db, opts)
	if err != nil || result.Fixed != int64(len(cases)) || result.Unresolved != 0 {
		t.Fatalf("run %+v %v", result, err)
	}
	for _, tt := range cases {
		var doc bson.M
		if err := db.Collection("attachments").FindOne(ctx, bson.M{"_id": tt.id}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		version := fsMap(fsMap(doc["versions"])[tt.version])
		final := fsString(version["path"])
		if filepath.Dir(final) != current {
			t.Errorf("%s outside current layout %s", tt.id, final)
		}
		if version["storage"] != "fs" || version["size"] != int64(len(payload)) || version["sha256"] != "preserve-metadata" {
			t.Errorf("%s metadata %#v", tt.id, version)
		}
		if tt.version == "original" && doc["path"] != final {
			t.Errorf("original top-level path %#v", doc)
		}
		if tt.version != "original" && doc["path"] != "old-top-level" {
			t.Errorf("thumbnail changed original %#v", doc)
		}
		for _, p := range []string{tt.source, final} {
			content, err := os.ReadFile(p)
			if err != nil || sha256.Sum256(content) != sha256.Sum256(payload) {
				t.Errorf("%s copied/moved incorrectly: %v", p, err)
			}
		}
	}
	if needed, err := CheckFilesystemPaths(ctx, db, opts); err != nil || needed {
		t.Fatalf("repeat check %v %v", needed, err)
	}
	if result, err := RunFilesystemPaths(ctx, db, opts); err != nil || result != (Result{}) {
		t.Fatalf("repeat %+v %v", result, err)
	}
}

func TestFilesystemPreservesExistingGridFSAndUnresolved(t *testing.T) {
	ctx, db := migrationDB(t)
	writable := filepath.Join(t.TempDir(), "files")
	existing := filepath.Join(t.TempDir(), "old-avatar")
	if err := os.WriteFile(existing, []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := db.Collection("avatars").InsertMany(ctx, []any{
		bson.M{"_id": "existing", "versions": bson.M{"original": bson.M{"storage": "fs", "path": existing, "size": 9}}},
		bson.M{"_id": "grid", "versions": bson.M{"original": bson.M{"meta": bson.M{"gridFsFileId": "grid-id"}}}},
		bson.M{"_id": "explicit-grid", "versions": bson.M{"original": bson.M{"storage": "gridfs"}}},
		bson.M{"_id": "s3", "versions": bson.M{"original": bson.M{"storage": "s3"}}},
		bson.M{"_id": "old-cfs", "copies": bson.M{"attachments": bson.M{"key": "old-key"}}},
		bson.M{"_id": "missing", "versions": bson.M{"original": bson.M{"path": "/missing/avatar"}}},
		bson.M{"_id": "malformed", "versions": bson.M{"original": "not-a-version"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := FilesystemOptions{WritablePath: writable}
	result, err := RunFilesystemPaths(ctx, db, opts)
	if err != nil || result != (Result{Unresolved: 1}) {
		t.Fatalf("initial %+v %v", result, err)
	}
	var doc bson.M
	if err := db.Collection("avatars").FindOne(ctx, bson.M{"_id": "existing"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	version := fsMap(fsMap(doc["versions"])["original"])
	if version["path"] != existing || version["size"] != int32(9) {
		t.Fatalf("existing file changed %#v", doc)
	}
	// Re-mounted storage retries: an existing files suffix must not be doubled.
	recovered := filepath.Join(writable, "avatars", "missing")
	if err := os.MkdirAll(filepath.Dir(recovered), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovered, []byte("recovered"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err = RunFilesystemPaths(ctx, db, opts)
	if err != nil || result != (Result{Fixed: 1}) {
		t.Fatalf("retry %+v %v", result, err)
	}
	if err := db.Collection("avatars").FindOne(ctx, bson.M{"_id": "missing"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["path"] != recovered {
		t.Fatalf("wrong files root %#v", doc)
	}
	if n, err := db.Collection("_wekan_migration").CountDocuments(ctx, bson.M{}); err != nil || n != 0 {
		t.Fatalf("step wrote global marker %d %v", n, err)
	}
}

func TestFilesystemCopyFailureRemainsUnresolved(t *testing.T) {
	ctx, db := migrationDB(t)
	writable := t.TempDir()
	if err := os.WriteFile(filepath.Join(writable, "old-document"), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(writable, "files"), []byte("blocks directory"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := db.Collection("attachments").InsertOne(ctx, bson.M{"_id": "old", "name": "document", "versions": bson.M{"original": bson.M{"storage": "fs", "path": "/unavailable"}}})
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	result, err := RunFilesystemPaths(ctx, db, FilesystemOptions{WritablePath: writable, Log: func(s string) { logs = append(logs, s) }})
	if err != nil || result != (Result{Unresolved: 1}) || len(logs) != 1 || !strings.Contains(logs[0], "copy failed") {
		t.Fatalf("copy failure %+v %v %v", result, err, logs)
	}
	var doc bson.M
	if err := db.Collection("attachments").FindOne(ctx, bson.M{"_id": "old"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if fsMap(fsMap(doc["versions"])["original"])["path"] != "/unavailable" {
		t.Fatal("failed copy repointed metadata")
	}
}

func TestFilesystemVersionStoragePredicate(t *testing.T) {
	existingDirectory := t.TempDir()
	for _, tt := range []struct {
		name    string
		version bson.M
		want    bool
	}{
		{"nil", nil, false},
		{"implicit filesystem", bson.M{}, true},
		{"existing directory follows existsSync", bson.M{"path": existingDirectory}, false},
		{"explicit filesystem with grid reference", bson.M{"storage": "fs", "meta": bson.M{"gridFsFileId": "grid"}}, true},
		{"implicit grid reference", bson.M{"meta": bson.M{"gridFsFileId": "grid"}}, false},
		{"empty grid reference", bson.M{"meta": bson.M{"gridFsFileId": ""}}, true},
		{"other storage", bson.M{"storage": "s3"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := filesystemVersionNeedsHeal(tt.version); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestFilesystemCanceledRunDoesNotMutate(t *testing.T) {
	_, db := migrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckFilesystemPaths(ctx, db, FilesystemOptions{WritablePath: t.TempDir()}); err == nil {
		t.Fatal("canceled check succeeded")
	}
	if _, err := RunFilesystemPaths(ctx, db, FilesystemOptions{WritablePath: t.TempDir()}); err == nil {
		t.Fatal("canceled run succeeded")
	}
}
