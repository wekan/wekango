package eventlog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func writerDB(t *testing.T) (context.Context, *mongo.Database) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
func writerRead(t *testing.T, ctx context.Context, db *mongo.Database, selector bson.M) bson.M {
	t.Helper()
	var row bson.M
	if err := db.Collection(Collection).FindOne(ctx, selector).Decode(&row); err != nil {
		t.Fatal(err)
	}
	return row
}
func writerNumber(value any) float64 {
	switch n := value.(type) {
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return -1
}

func TestWriterRetainsExistingRowAndCountsOneOccurrence(t *testing.T) {
	ctx, db := writerDB(t)
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	last := first.Add(time.Hour)
	_, err := db.Collection(Collection).InsertOne(ctx, bson.M{"_id": "existing-meteor-id", "stream": "security", "source": "guard", "count": 12, "firstAt": first, "at": first, "extra": "retain"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "user", "username": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	writer := NewWriter(db)
	event := bson.M{"stream": "security", "source": "guard", "userId": "user", "ip": "::ffff:203.0.113.7", "at": last, "count": 25, "detail": "latest"}
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	if _, exists := event["username"]; exists {
		t.Fatal("writer mutated caller event")
	}
	row := writerRead(t, ctx, db, bson.M{"_id": "existing-meteor-id"})
	if writerNumber(row["count"]) != 13 || row["extra"] != "retain" || row["firstAt"] != bson.NewDateTimeFromTime(first) || row["at"] != bson.NewDateTimeFromTime(last) {
		t.Fatalf("old row not retained %#v", row)
	}
	if row["username"] != "alice" || row["ip"] != "203.0.113.7" || row["ipv4"] != "203.0.113.7" || row["detail"] != "latest" {
		t.Fatalf("missing enrichment %#v", row)
	}
	if n, err := db.Collection(Collection).CountDocuments(ctx, bson.M{}); err != nil || n != 1 {
		t.Fatalf("same problem split rows %d %v", n, err)
	}
	actors := writerMap(row["actors"])
	if len(actors) != 2 {
		t.Fatalf("account and IP actors %#v", actors)
	}
	for _, actor := range actors {
		if writerNumber(writerMap(actor)["count"]) != 1 {
			t.Fatalf("doc.count changed fold weight %#v", actor)
		}
	}
	// An account rename updates the existing row; username is never identity.
	event["username"] = "renamed"
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	row = writerRead(t, ctx, db, bson.M{"_id": "existing-meteor-id"})
	if row["username"] != "renamed" || writerNumber(row["count"]) != 14 {
		t.Fatalf("rename fold %#v", row)
	}
}

func TestWriterConcurrentActorsAreBoundedAndRetained(t *testing.T) {
	ctx, db := writerDB(t)
	writer := NewWriter(db)
	const count = 80
	errors := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errors <- writer.Fold(ctx, bson.M{"stream": "security", "source": "rotating-address", "ip": fmt.Sprintf("203.0.113.%d", i+1)})
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.Collection(Collection).CountDocuments(ctx, bson.M{}); err != nil || n != 1 {
		t.Fatalf("concurrent initial upserts %d %v", n, err)
	}
	row := writerRead(t, ctx, db, bson.M{"source": "rotating-address"})
	actors := writerMap(row["actors"])
	if writerNumber(row["count"]) != count || len(actors) != 50 || writerNumber(row["actorsOverflow"]) != 30 {
		t.Fatalf("actor bound %#v", row)
	}
	id, ok := row["_id"].(string)
	if !ok || len(id) != 17 {
		t.Fatalf("insert id not Meteor format %#v", row["_id"])
	}
	var existingIP string
	for _, actor := range actors {
		existingIP = writerMap(actor)["value"].(string)
		break
	}
	// Reconstructing the writer must hydrate known actors before admitting more.
	writer = NewWriter(db)
	if err := writer.Fold(ctx, bson.M{"stream": "security", "source": "rotating-address", "ip": existingIP}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Fold(ctx, bson.M{"stream": "security", "source": "rotating-address", "ip": "198.51.100.1"}); err != nil {
		t.Fatal(err)
	}
	row = writerRead(t, ctx, db, bson.M{"source": "rotating-address"})
	actors = writerMap(row["actors"])
	if len(actors) != 50 || writerNumber(row["actorsOverflow"]) != 31 || writerNumber(row["count"]) != 82 {
		t.Fatalf("restart exceeded actor cap %#v", row)
	}
	found := false
	for _, actor := range actors {
		a := writerMap(actor)
		if a["value"] == existingIP {
			found = true
			if writerNumber(a["count"]) != 2 {
				t.Fatalf("known actor lost count %#v", a)
			}
		}
	}
	if !found {
		t.Fatal("known actor disappeared")
	}
}

func TestWriterUsernameTTLDeletedUsersAndCacheBounds(t *testing.T) {
	ctx, db := writerDB(t)
	_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "user", "username": "original"})
	if err != nil {
		t.Fatal(err)
	}
	writer := NewWriter(db)
	clock := time.Now()
	writer.now = func() time.Time { return clock }
	event := bson.M{"stream": "tests", "source": "username-cache", "userId": "user", "ip": "2001:db8::1"}
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": "user"}, bson.M{"$set": bson.M{"username": strings.Repeat("n", 120)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	row := writerRead(t, ctx, db, bson.M{"source": "username-cache"})
	if row["username"] != "original" {
		t.Fatal("username cache expired before TTL")
	}
	clock = clock.Add(time.Minute)
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	row = writerRead(t, ctx, db, bson.M{"source": "username-cache"})
	if row["username"] != strings.Repeat("n", 100) || row["ipv6"] != "2001:db8::1" {
		t.Fatalf("username refresh/truncation %#v", row)
	}
	event["source"] = "deleted-user"
	event["userId"] = "deleted"
	if err := writer.Fold(ctx, event); err != nil {
		t.Fatal(err)
	}
	row = writerRead(t, ctx, db, bson.M{"source": "deleted-user"})
	if row["userId"] != "deleted" || row["username"] != nil {
		t.Fatalf("deleted account handling %#v", row)
	}
	// Exercise an eviction at each exact capacity boundary. Seed only cache
	// state, then use the real database path for each miss/refill; replaying 500
	// distinct database identities would test collection-scan throughput instead.
	clear(writer.usernames)
	for i := 0; i < usernameCacheMax; i++ {
		writer.usernames[fmt.Sprintf("cached-%d", i)] = usernameEntry{username: "cached", at: clock}
	}
	name, err := writer.usernameFor(ctx, "user")
	if err != nil || name != strings.Repeat("n", 120) || len(writer.usernames) != 1 {
		t.Fatalf("username eviction %q %d %v", name, len(writer.usernames), err)
	}
	clear(writer.knownActors)
	for i := 0; i < maxCachedRows; i++ {
		writer.knownActors[fmt.Sprintf("cached-%d", i)] = map[string]bool{}
	}
	if err := writer.Fold(ctx, bson.M{"stream": "tests", "source": "cache-refill", "ip": "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	if len(writer.knownActors) != 1 {
		t.Fatalf("actor cache eviction retained %d entries", len(writer.knownActors))
	}
	row = writerRead(t, ctx, db, bson.M{"source": "cache-refill"})
	if len(writerMap(row["actors"])) != 1 || writerNumber(row["count"]) != 1 {
		t.Fatalf("cache refill lost event %#v", row)
	}

}

func TestWriterCanceledAndFailedWritesReturnErrors(t *testing.T) {
	ctx, db := writerDB(t)
	writer := NewWriter(db)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := writer.Fold(canceled, bson.M{"stream": "tests"}); err == nil {
		t.Fatal("canceled fold succeeded")
	}
	// Cancellation must also release a caller waiting behind another fold.
	writer.gate <- struct{}{}
	if err := writer.Fold(canceled, bson.M{}); err == nil {
		t.Fatal("canceled gate wait succeeded")
	}
	<-writer.gate
	if err := NewWriter(nil).Fold(ctx, bson.M{}); err == nil {
		t.Fatal("nil database panicked or succeeded")
	}
	_, err := db.Collection(Collection).InsertOne(ctx, bson.M{"_id": "bad-count", "stream": "tests", "count": "not numeric"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Fold(ctx, bson.M{"stream": "tests"}); err == nil {
		t.Fatal("invalid stored counter was silently replaced")
	}
	row := writerRead(t, ctx, db, bson.M{"_id": "bad-count"})
	if row["count"] != "not numeric" {
		t.Fatal("failed update changed existing row")
	}
	if len(writer.knownActors) != 0 {
		t.Fatal("failed write left potentially stale actor cache")
	}
}
