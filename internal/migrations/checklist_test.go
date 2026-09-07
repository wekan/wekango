package migrations

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func migrationDB(t *testing.T) (context.Context, *mongo.Database) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	server, err := database.Start(ctx, database.Config{Directory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := mongo.Connect(options.Client().ApplyURI(server.URI()).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return ctx, client.Database("wekan")
}

func TestChecklistOneTimeMarker(t *testing.T) {
	ctx, db := migrationDB(t)
	collection := db.Collection("checklists")
	_, err := collection.InsertMany(ctx, []any{
		bson.M{"_id": "old-default", "showChecklistAtMinicard": false},
		bson.M{"_id": "explicit-true", "showChecklistAtMinicard": true},
		bson.M{"_id": "absent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	count, err := RunChecklistMinicard(ctx, db)
	if err != nil || count != 1 {
		t.Fatalf("first run: %d, %v", count, err)
	}
	for _, id := range []string{"old-default", "absent"} {
		var doc bson.M
		if err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		if _, exists := doc["showChecklistAtMinicard"]; exists {
			t.Fatalf("%s override remains", id)
		}
	}
	var doc bson.M
	if err := collection.FindOne(ctx, bson.M{"_id": "explicit-true"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["showChecklistAtMinicard"] != true {
		t.Fatal("explicit true changed")
	}
	if err := db.Collection("_wekan_migration").FindOne(ctx, bson.M{"_id": checklistMarker}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["at"].(bson.DateTime); !ok {
		t.Fatalf("marker has no BSON date: %#v", doc)
	}
	if err := db.Collection("_wekan_migration").FindOne(ctx, bson.M{"_id": "schema-upgrade"}).Err(); err != mongo.ErrNoDocuments {
		t.Fatalf("full upgrade incorrectly marked: %v", err)
	}
	if _, err := collection.InsertOne(ctx, bson.M{"_id": "new-choice", "showChecklistAtMinicard": false}); err != nil {
		t.Fatal(err)
	}
	count, err = RunChecklistMinicard(ctx, db)
	if err != nil || count != 0 {
		t.Fatalf("repeat: %d, %v", count, err)
	}
	if err := collection.FindOne(ctx, bson.M{"_id": "new-choice"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["showChecklistAtMinicard"] != false {
		t.Fatal("repeat erased user choice")
	}
}

func TestChecklistNoChangesDoesNotWriteMarker(t *testing.T) {
	ctx, db := migrationDB(t)
	count, err := RunChecklistMinicard(ctx, db)
	if err != nil || count != 0 {
		t.Fatalf("empty database: %d, %v", count, err)
	}
	if err := db.Collection("_wekan_migration").FindOne(ctx, bson.M{"_id": checklistMarker}).Err(); err != mongo.ErrNoDocuments {
		t.Fatalf("unexpected marker: %v", err)
	}
}

func TestChecklistCancelledRunDoesNotMark(t *testing.T) {
	_, db := migrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunChecklistMinicard(ctx, db); err == nil {
		t.Fatal("canceled migration reported success")
	}
}
