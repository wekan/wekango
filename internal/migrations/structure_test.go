package migrations

import (
	"context"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"reflect"
	"regexp"
	"testing"
)

func structuralSeed(t *testing.T, ctx context.Context, db *mongo.Database, coll string, docs ...bson.M) {
	t.Helper()
	values := make([]any, len(docs))
	for i, d := range docs {
		values[i] = d
	}
	if _, e := db.Collection(coll).InsertMany(ctx, values); e != nil {
		t.Fatal(e)
	}
}
func structuralDoc(t *testing.T, ctx context.Context, db *mongo.Database, coll, id string) bson.M {
	t.Helper()
	var d bson.M
	if e := db.Collection(coll).FindOne(ctx, bson.M{"_id": id}).Decode(&d); e != nil {
		t.Fatal(e)
	}
	return d
}
func structuralAssert(t *testing.T, ctx context.Context, db *mongo.Database, coll, id string, fields bson.M) {
	t.Helper()
	d := structuralDoc(t, ctx, db, coll, id)
	for k, v := range fields {
		if !reflect.DeepEqual(d[k], v) {
			t.Fatalf("%s/%s %s = %#v, want %#v", coll, id, k, d[k], v)
		}
	}
}

func TestSwimlaneStructureRepairsAndPreserves(t *testing.T) {
	ctx, db := migrationDB(t)
	structuralSeed(t, ctx, db, "boards", bson.M{"_id": "old"}, bson.M{"_id": "existing"}, bson.M{"_id": "foreign"}, bson.M{"_id": "no-list"})
	structuralSeed(t, ctx, db, "swimlanes", bson.M{"_id": "archived", "boardId": "existing", "archived": true, "sort": -10}, bson.M{"_id": "template", "boardId": "existing", "type": "template-swimlane", "sort": -9}, bson.M{"_id": "later", "boardId": "existing", "sort": 2}, bson.M{"_id": "first", "boardId": "existing", "sort": 1}, bson.M{"_id": "foreign-lane", "boardId": "foreign", "sort": 0})
	structuralSeed(t, ctx, db, "lists", bson.M{"_id": "shared", "boardId": "old", "title": "Todo"}, bson.M{"_id": "empty-shared", "boardId": "existing", "swimlaneId": "", "sort": 0}, bson.M{"_id": "archived-first", "boardId": "existing", "swimlaneId": "archived", "archived": true, "sort": -1})
	structuralSeed(t, ctx, db, "cards",
		bson.M{"_id": "missing", "boardId": "old", "listId": "shared", "archived": false},
		bson.M{"_id": "empty", "boardId": "old", "listId": "shared", "swimlaneId": "", "archived": true},
		bson.M{"_id": "dangling", "boardId": "old", "listId": "gone", "archived": false},
		bson.M{"_id": "deleted", "boardId": "existing", "listId": "empty-shared", "swimlaneId": "deleted", "archived": false},
		bson.M{"_id": "foreign", "boardId": "existing", "listId": "empty-shared", "swimlaneId": "foreign-lane", "archived": false},
		bson.M{"_id": "hidden", "boardId": "existing", "listId": "empty-shared", "swimlaneId": "archived", "archived": false},
		bson.M{"_id": "template-card", "boardId": "existing", "listId": "empty-shared", "swimlaneId": "template", "archived": false},
		bson.M{"_id": "archived-card", "boardId": "existing", "listId": "empty-shared", "swimlaneId": "deleted", "archived": true},
		bson.M{"_id": "first-list-rescue", "boardId": "existing", "listId": "gone", "swimlaneId": "first", "archived": false},
		bson.M{"_id": "create-list", "boardId": "no-list", "listId": "gone-a", "swimlaneId": "deleted", "archived": false},
		bson.M{"_id": "reuse-list", "boardId": "no-list", "listId": "gone-b", "swimlaneId": "deleted", "archived": false})
	if yes, e := CheckSwimlaneStructure(ctx, db); e != nil || !yes {
		t.Fatalf("check %v %v", yes, e)
	}
	r, e := RunSwimlaneStructure(ctx, db)
	if e != nil || r.Fixed != 11 || r.Unresolved != 0 {
		t.Fatalf("run %+v %v", r, e)
	}
	missing := structuralDoc(t, ctx, db, "cards", "missing")
	id := missing["swimlaneId"].(string)
	if !regexp.MustCompile(`^[23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz]{17}$`).MatchString(id) {
		t.Fatal(id)
	}
	lane := structuralDoc(t, ctx, db, "swimlanes", id)
	for _, k := range []string{"createdAt", "modifiedAt", "updatedAt"} {
		if _, ok := lane[k].(bson.DateTime); !ok {
			t.Fatalf("%s not date", k)
		}
		if lane[k] != lane["createdAt"] {
			t.Fatal("dates differ")
		}
	}
	structuralAssert(t, ctx, db, "cards", "empty", bson.M{"swimlaneId": id, "archived": true})
	structuralAssert(t, ctx, db, "cards", "dangling", bson.M{"listId": "shared", "swimlaneId": id})
	if _, ok := structuralDoc(t, ctx, db, "lists", "shared")["swimlaneId"]; ok {
		t.Fatal("shared list stamped")
	}
	structuralAssert(t, ctx, db, "lists", "empty-shared", bson.M{"swimlaneId": ""})
	for _, card := range []string{"deleted", "foreign"} {
		structuralAssert(t, ctx, db, "cards", card, bson.M{"swimlaneId": "first"})
	}
	structuralAssert(t, ctx, db, "cards", "hidden", bson.M{"swimlaneId": "archived"})
	structuralAssert(t, ctx, db, "cards", "template-card", bson.M{"swimlaneId": "template"})
	structuralAssert(t, ctx, db, "cards", "archived-card", bson.M{"swimlaneId": "deleted", "archived": true})
	structuralAssert(t, ctx, db, "cards", "first-list-rescue", bson.M{"listId": "archived-first", "swimlaneId": "archived"})
	created := structuralDoc(t, ctx, db, "cards", "create-list")
	reused := structuralDoc(t, ctx, db, "cards", "reuse-list")
	if created["listId"] != reused["listId"] {
		t.Fatal("multiple rescued lists")
	}
	rescue := structuralDoc(t, ctx, db, "lists", created["listId"].(string))
	if rescue["title"] != "Rescued Data" || rescue["createdAt"] != lane["createdAt"] {
		t.Fatalf("rescue %#v", rescue)
	}
	if yes, e := CheckSwimlaneStructure(ctx, db); e != nil || yes {
		t.Fatalf("repeat check %v %v", yes, e)
	}
	if r, e = RunSwimlaneStructure(ctx, db); e != nil || r.Fixed != 0 {
		t.Fatalf("repeat %+v %v", r, e)
	}
}

func TestSwimlaneStructureArchivedBoardCheckDoesNotInventLane(t *testing.T) {
	ctx, db := migrationDB(t)
	structuralSeed(t, ctx, db, "boards", bson.M{"_id": "b"})
	structuralSeed(t, ctx, db, "swimlanes", bson.M{"_id": "s", "boardId": "b", "archived": true})
	structuralSeed(t, ctx, db, "lists", bson.M{"_id": "l", "boardId": "b", "swimlaneId": ""})
	structuralSeed(t, ctx, db, "cards", bson.M{"_id": "c", "boardId": "b", "listId": "l", "swimlaneId": "s", "archived": false})
	if yes, e := CheckSwimlaneStructure(ctx, db); e != nil || yes {
		t.Fatalf("archived board not broken %v %v", yes, e)
	}
	structuralAssert(t, ctx, db, "cards", "c", bson.M{"swimlaneId": "s"})
}

func TestMergePerSwimlaneListsCanonicalHistoryAndMarkers(t *testing.T) {
	ctx, db := migrationDB(t)
	structuralSeed(t, ctx, db, "boards", bson.M{"_id": "b", "fixMissingListsCompleted": true, "fixMissingListsCompletedAt": bson.DateTime(123), "comprehensiveMigrationCompleted": true, "keep": "yes"})
	structuralSeed(t, ctx, db, "lists",
		bson.M{"_id": "stamped", "boardId": "b", "title": "Todo", "swimlaneId": "s1", "createdAt": bson.DateTime(1)},
		bson.M{"_id": "shared", "boardId": "b", "title": "Todo", "swimlaneId": "", "createdAt": bson.DateTime(900)},
		bson.M{"_id": "renamed", "boardId": "b", "title": "Personal todo", "swimlaneId": "s2"},
		bson.M{"_id": "archived", "boardId": "b", "title": "Todo", "swimlaneId": "s1", "archived": true},
		bson.M{"_id": "template", "boardId": "b", "title": "Todo", "swimlaneId": "s1", "type": "template-list"},
		bson.M{"_id": "newer", "boardId": "b", "title": "Done", "swimlaneId": "s1", "createdAt": bson.DateTime(2), "sort": -1},
		bson.M{"_id": "older", "boardId": "b", "title": "Done", "swimlaneId": "s2", "createdAt": bson.DateTime(1), "sort": 2})
	structuralSeed(t, ctx, db, "cards", bson.M{"_id": "c", "boardId": "b", "listId": "stamped", "swimlaneId": "s1", "sort": 7, "archived": true}, bson.M{"_id": "d", "boardId": "b", "listId": "newer", "swimlaneId": "s2"})
	structuralSeed(t, ctx, db, "activities", bson.M{"_id": "a", "listId": "stamped", "text": "keep"}, bson.M{"_id": "d", "listId": "newer"})
	if yes, e := CheckMergePerSwimlaneLists(ctx, db); e != nil || !yes {
		t.Fatalf("check %v %v", yes, e)
	}
	r, e := RunMergePerSwimlaneLists(ctx, db)
	if e != nil || r.Fixed != 5 {
		t.Fatalf("run %+v %v", r, e)
	}
	structuralAssert(t, ctx, db, "cards", "c", bson.M{"listId": "shared", "swimlaneId": "s1", "sort": int32(7), "archived": true})
	structuralAssert(t, ctx, db, "activities", "a", bson.M{"listId": "shared", "text": "keep"})
	structuralAssert(t, ctx, db, "cards", "d", bson.M{"listId": "older", "swimlaneId": "s2"})
	structuralAssert(t, ctx, db, "activities", "d", bson.M{"listId": "older"})
	for _, id := range []string{"shared", "renamed", "archived", "older"} {
		structuralAssert(t, ctx, db, "lists", id, bson.M{"swimlaneId": ""})
	}
	structuralAssert(t, ctx, db, "lists", "template", bson.M{"swimlaneId": "s1", "type": "template-list"})
	for _, id := range []string{"stamped", "newer"} {
		if e := db.Collection("lists").FindOne(ctx, bson.M{"_id": id}).Err(); e != mongo.ErrNoDocuments {
			t.Fatalf("duplicate still exists %s %v", id, e)
		}
	}
	board := structuralDoc(t, ctx, db, "boards", "b")
	if len(board) != 2 || board["keep"] != "yes" {
		t.Fatalf("markers %#v", board)
	}
	if yes, e := CheckMergePerSwimlaneLists(ctx, db); e != nil || yes {
		t.Fatalf("repeat check %v %v", yes, e)
	}
	if r, e = RunMergePerSwimlaneLists(ctx, db); e != nil || r.Fixed != 0 {
		t.Fatalf("repeat %+v %v", r, e)
	}
}

func TestMergePerSwimlaneListsSymptomsAndNegativePreservation(t *testing.T) {
	ctx, db := migrationDB(t)
	structuralSeed(t, ctx, db, "lists", bson.M{"_id": "healthy", "boardId": "healthy", "title": "Todo", "swimlaneId": "s1"}, bson.M{"_id": "renamed", "boardId": "healthy", "title": "Different", "swimlaneId": "s2"}, bson.M{"_id": "mismatch", "boardId": "old", "title": "Todo", "swimlaneId": "s1"}, bson.M{"_id": "archived", "boardId": "hidden", "title": "Todo", "swimlaneId": "s1", "archived": true})
	structuralSeed(t, ctx, db, "cards", bson.M{"_id": "healthy", "listId": "healthy", "swimlaneId": "s1"}, bson.M{"_id": "mismatch", "listId": "mismatch", "swimlaneId": "s2"}, bson.M{"_id": "archived-mismatch", "listId": "archived", "swimlaneId": "s2"})
	r, e := RunMergePerSwimlaneLists(ctx, db)
	if e != nil || r.Fixed != 1 {
		t.Fatalf("run %+v %v", r, e)
	}
	structuralAssert(t, ctx, db, "lists", "mismatch", bson.M{"swimlaneId": ""})
	structuralAssert(t, ctx, db, "cards", "mismatch", bson.M{"swimlaneId": "s2", "listId": "mismatch"})
	for _, id := range []string{"healthy", "archived"} {
		structuralAssert(t, ctx, db, "lists", id, bson.M{"swimlaneId": "s1"})
	}
	structuralAssert(t, ctx, db, "lists", "renamed", bson.M{"swimlaneId": "s2"})
	// The JS check includes archived-list mismatches but its run does not act on
	// those alone. Keep that asymmetry, including its zero-change repeat.
	if yes, e := CheckMergePerSwimlaneLists(ctx, db); e != nil || !yes {
		t.Fatalf("archived mismatch check %v %v", yes, e)
	}
	if r, e = RunMergePerSwimlaneLists(ctx, db); e != nil || r.Fixed != 0 {
		t.Fatalf("repeat %+v %v", r, e)
	}
}

func TestStructuralStepsCancellation(t *testing.T) {
	_, db := migrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, check := range []func(context.Context, *mongo.Database) (bool, error){CheckSwimlaneStructure, CheckMergePerSwimlaneLists} {
		if _, e := check(ctx, db); e == nil {
			t.Fatal("canceled check succeeded")
		}
	}
	for _, run := range []func(context.Context, *mongo.Database) (Result, error){RunSwimlaneStructure, RunMergePerSwimlaneLists} {
		if _, e := run(ctx, db); e == nil {
			t.Fatal("canceled run succeeded")
		}
	}
}
