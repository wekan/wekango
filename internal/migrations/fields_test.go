package migrations

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func fieldInsert(t *testing.T, ctx context.Context, db *mongo.Database, collection string, docs ...any) {
	t.Helper()
	if _, err := db.Collection(collection).InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
}
func fieldRead(t *testing.T, ctx context.Context, db *mongo.Database, collection string, id any) bson.M {
	t.Helper()
	var doc bson.M
	if err := db.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func fieldRun(t *testing.T, ctx context.Context, db *mongo.Database, check func(context.Context, *mongo.Database) (bool, error), run func(context.Context, *mongo.Database) (Result, error), want int64) {
	t.Helper()
	needed, err := check(ctx, db)
	if err != nil || !needed {
		t.Fatalf("check = %v, %v", needed, err)
	}
	result, err := run(ctx, db)
	if err != nil || result.Fixed != want || result.Unresolved != 0 {
		t.Fatalf("result = %+v, %v; want fixed %d", result, err, want)
	}
	needed, err = check(ctx, db)
	if err != nil || needed {
		t.Fatalf("repeat check = %v, %v", needed, err)
	}
	result, err = run(ctx, db)
	if err != nil || result.Fixed != 0 || result.Unresolved != 0 {
		t.Fatalf("repeat = %+v, %v", result, err)
	}
}

func TestFieldsArchivedBackfillPreservesStoredValues(t *testing.T) {
	ctx, db := migrationDB(t)
	for _, collection := range archivedCollections {
		fieldInsert(t, ctx, db, collection, bson.M{"_id": "missing"}, bson.M{"_id": "null", "archived": nil}, bson.M{"_id": "false", "archived": false}, bson.M{"_id": "true", "archived": true})
	}
	fieldInsert(t, ctx, db, "checklists", bson.M{"_id": "untouched"})
	fieldRun(t, ctx, db, CheckArchivedFlagBackfill, RunArchivedFlagBackfill, 4)
	for _, collection := range archivedCollections {
		for id, want := range map[string]any{"missing": false, "null": nil, "false": false, "true": true} {
			if got := fieldRead(t, ctx, db, collection, id)["archived"]; got != want {
				t.Fatalf("%s/%s: %v", collection, id, got)
			}
		}
	}
	if _, ok := fieldRead(t, ctx, db, "checklists", "untouched")["archived"]; ok {
		t.Fatal("changed unrelated collection")
	}
}
func TestFieldsBoardAllowsDefaultsCountFieldsAndPreserveChoices(t *testing.T) {
	ctx, db := migrationDB(t)
	complete := bson.M{"_id": "complete"}
	partial := bson.M{"_id": "partial", "allowsComments": false, "allowsAttachments": nil, "allowsDescriptionText": "custom"}
	for _, f := range BoardAllowsTrueDefaults {
		complete[f] = true
	}
	fieldInsert(t, ctx, db, "boards", bson.M{"_id": "old"}, partial, complete)
	fieldRun(t, ctx, db, CheckBoardAllowsDefaults, RunBoardAllowsDefaults, int64(2*len(BoardAllowsTrueDefaults)-3))
	old := fieldRead(t, ctx, db, "boards", "old")
	for _, f := range BoardAllowsTrueDefaults {
		if old[f] != true {
			t.Fatalf("missing default %s", f)
		}
	}
	if _, ok := old["allowsDescriptionTextOnMinicard"]; ok {
		t.Fatal("invented excluded default")
	}
	got := fieldRead(t, ctx, db, "boards", "partial")
	for _, f := range []string{"allowsComments", "allowsAttachments", "allowsDescriptionText"} {
		if got[f] != partial[f] {
			t.Fatalf("overwrote %s", f)
		}
	}
}
func TestFieldsBoardPermissionsExactHistoricalSpellings(t *testing.T) {
	ctx, db := migrationDB(t)
	values := []any{"PUBLIC", "Public", "PRIVATE", "Private", "public", "private", "pUbLiC", "", "TEAM", nil}
	for i, v := range values {
		fieldInsert(t, ctx, db, "boards", bson.M{"_id": int32(i), "permission": v})
	}
	fieldInsert(t, ctx, db, "boards", bson.M{"_id": "missing"})
	fieldRun(t, ctx, db, CheckBoardPermissionLowercase, RunBoardPermissionLowercase, 4)
	for i, v := range values {
		want := v
		if i < 4 {
			want = strings.ToLower(v.(string))
		}
		if got := fieldRead(t, ctx, db, "boards", int32(i))["permission"]; got != want {
			t.Fatalf("%v -> %v", v, got)
		}
	}
}
func TestFieldsCustomFieldsPreserveExistingBoardArrays(t *testing.T) {
	ctx, db := migrationDB(t)
	cases := []struct {
		id          string
		old, boards any
		want        bson.A
	}{
		{"scalar", "b", nil, bson.A{"b"}}, {"empty", "b", bson.A{}, bson.A{"b"}}, {"null", nil, nil, bson.A{"null"}}, {"number", int32(42), "invalid", bson.A{"42"}}, {"existing", "old", bson.A{"new", "other"}, bson.A{"new", "other"}},
	}
	for _, c := range cases {
		fieldInsert(t, ctx, db, "customFields", bson.M{"_id": c.id, "boardId": c.old, "boardIds": c.boards, "name": "Keep"})
	}
	fieldInsert(t, ctx, db, "customFields", bson.M{"_id": "untouched", "boardIds": bson.A{}})
	fieldRun(t, ctx, db, CheckCustomFieldsBoardIDs, RunCustomFieldsBoardIDs, int64(len(cases)))
	for _, c := range cases {
		got := fieldRead(t, ctx, db, "customFields", c.id)
		if _, ok := got["boardId"]; ok {
			t.Fatal("old boardId remains")
		}
		if !reflect.DeepEqual(fieldArray(got["boardIds"]), []any(c.want)) || got["name"] != "Keep" {
			t.Fatalf("%s: %#v", c.id, got)
		}
	}
}
func TestFieldsBoardMembersBackfillPreservesInactiveAndNull(t *testing.T) {
	ctx, db := migrationDB(t)
	members := bson.A{bson.M{"userId": "old", "isAdmin": true}, bson.M{"userId": "inactive", "isActive": false}, bson.M{"userId": "active", "isActive": true}, bson.M{"userId": "null", "isActive": nil}, nil}
	fieldInsert(t, ctx, db, "boards", bson.M{"_id": "old", "members": members}, bson.M{"_id": "empty", "members": bson.A{}}, bson.M{"_id": "missing"})
	fieldRun(t, ctx, db, CheckBoardMembersIsActive, RunBoardMembersIsActive, 1)
	got := fieldArray(fieldRead(t, ctx, db, "boards", "old")["members"])
	if len(got) != 5 || fieldMap(got[0])["isActive"] != true || fieldMap(got[0])["isAdmin"] != true || fieldMap(got[1])["isActive"] != false || fieldMap(got[2])["isActive"] != true || fieldMap(got[3])["isActive"] != nil || got[4] != nil {
		t.Fatalf("members: %#v", got)
	}
}
func TestFieldsChecklistExtractionResumesWithoutDuplicating(t *testing.T) {
	ctx, db := migrationDB(t)
	fieldInsert(t, ctx, db, "checklists", bson.M{"_id": "old", "cardId": "card", "items": bson.A{bson.M{"title": "Last", "sort": 10, "isFinished": true}, bson.M{"title": "First", "sort": -2}, bson.M{"title": "", "sort": 0}}}, bson.M{"_id": "empty", "items": bson.A{}}, bson.M{"_id": "missing"})
	fieldInsert(t, ctx, db, "checklistItems", bson.M{"_id": "already-extracted", "title": "First", "sort": 0, "isFinished": false, "checklistId": "old", "cardId": "card"}, bson.M{"_id": "unrelated", "title": "Last", "sort": 2, "isFinished": true, "checklistId": "elsewhere"})
	fieldRun(t, ctx, db, CheckChecklistItemsEmbedded, RunChecklistItemsEmbedded, 2)
	if _, exists := fieldRead(t, ctx, db, "checklists", "old")["items"]; exists {
		t.Fatal("embedded items remain")
	}
	if _, exists := fieldRead(t, ctx, db, "checklists", "empty")["items"]; !exists {
		t.Fatal("empty array changed")
	}
	cursor, err := db.Collection("checklistItems").Find(ctx, bson.M{"checklistId": "old"})
	if err != nil {
		t.Fatal(err)
	}
	defer cursor.Close(ctx)
	count := 0
	for cursor.Next(ctx) {
		var doc bson.M
		if err = cursor.Decode(&doc); err != nil {
			t.Fatal(err)
		}
		count++
		if doc["cardId"] != "card" {
			t.Fatalf("lost cardId: %#v", doc)
		}
		if doc["_id"] == "already-extracted" {
			continue
		}
		id := doc["_id"].(string)
		if len(id) != 17 || strings.ContainsAny(id, "01IOl") {
			t.Fatalf("invalid Meteor id %q", id)
		}
		if _, ok := doc["createdAt"].(bson.DateTime); !ok {
			t.Fatal("missing creation time")
		}
		if doc["createdAt"] != doc["modifiedAt"] {
			t.Fatal("timestamps diverged")
		}
		switch doc["title"] {
		case "Checklist":
			if fieldNumber(doc["sort"]) != 1 || doc["isFinished"] != false {
				t.Fatalf("default: %#v", doc)
			}
		case "Last":
			if fieldNumber(doc["sort"]) != 2 || doc["isFinished"] != true {
				t.Fatalf("last: %#v", doc)
			}
		default:
			t.Fatalf("unexpected item: %#v", doc)
		}
	}
	if err = cursor.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count: %d", count)
	}
}
func TestFieldsAttachmentKindFlagsUsesMetadataAndPreservesUnrelatedFields(t *testing.T) {
	ctx, db := migrationDB(t)
	fieldInsert(t, ctx, db, "attachments", bson.M{"_id": "photo", "name": "C:\\upload\\PHOTO.PNG?download=1", "isImage": false, "versions": bson.M{"original": bson.M{"path": "unchanged"}}}, bson.M{"_id": "mime", "name": "notes.txt", "type": "image/png", "extension": "txt"}, bson.M{"_id": "nested", "name": "unknown", "versions": bson.M{"original": bson.M{"type": "video/mp4", "extension": "MP4"}}}, bson.M{"_id": "unknown", "name": "README"}, bson.M{"_id": "ready", "name": "file.pdf", "type": "application/pdf", "extension": "pdf", "isPDF": true})
	needed, err := CheckAttachmentKindFlags(ctx, db)
	if err != nil || !needed {
		t.Fatalf("check: %v %v", needed, err)
	}
	r, err := RunAttachmentKindFlags(ctx, db)
	if err != nil || r.Fixed != 3 || r.Unresolved != 0 {
		t.Fatalf("run: %+v %v", r, err)
	}
	photo := fieldRead(t, ctx, db, "attachments", "photo")
	if photo["isImage"] != true || photo["type"] != "image/png" || photo["extension"] != "png" || fieldMap(fieldMap(photo["versions"])["original"])["path"] != "unchanged" {
		t.Fatalf("photo: %#v", photo)
	}
	mime := fieldRead(t, ctx, db, "attachments", "mime")
	if mime["isImage"] != true || mime["isText"] != nil || mime["extension"] != "txt" {
		t.Fatalf("mime override: %#v", mime)
	}
	nested := fieldRead(t, ctx, db, "attachments", "nested")
	if nested["isVideo"] != true || nested["extension"] != "mp4" {
		t.Fatalf("nested: %#v", nested)
	}
	if len(fieldRead(t, ctx, db, "attachments", "unknown")) != 2 {
		t.Fatal("unknown file guessed")
	}
	// Source's broad check can remain true even though running again changes no
	// data; the orchestrator stamps a clean version despite those unknown files.
	r, err = RunAttachmentKindFlags(ctx, db)
	if err != nil || r.Fixed != 0 || r.Unresolved != 0 {
		t.Fatalf("repeat: %+v %v", r, err)
	}
}
func TestFieldsNonfiniteFilterWorksOnEmbeddedDatabase(t *testing.T) {
	ctx, db := migrationDB(t)
	values := []any{0, -1, math.MaxFloat64, -math.MaxFloat64, "Infinity", nil}
	for _, collection := range sortedCollections {
		for i, value := range values {
			fieldInsert(t, ctx, db, collection, bson.M{"_id": int32(i), "sort": value})
		}
		fieldInsert(t, ctx, db, collection, bson.M{"_id": "missing"})
	}
	needed, err := CheckNonfiniteSortRepair(ctx, db)
	if err != nil || needed {
		t.Fatalf("finite check: %v %v", needed, err)
	}
	r, err := RunNonfiniteSortRepair(ctx, db)
	if err != nil || r.Fixed != 0 {
		t.Fatalf("finite repair: %+v %v", r, err)
	}
	for _, value := range []float64{math.Inf(1), math.Inf(-1)} {
		if _, err = db.Collection("cards").InsertOne(ctx, bson.M{"sort": value}); err == nil {
			t.Fatalf("FerretDB unexpectedly accepted nonfinite %v", value)
		}
	}
	fieldInsert(t, ctx, db, "cards", bson.M{"_id": "nan", "sort": math.NaN()})
	fieldRun(t, ctx, db, CheckNonfiniteSortRepair, RunNonfiniteSortRepair, 1)
	if got := fieldRead(t, ctx, db, "cards", "nan")["sort"]; fieldNumber(got) != 0 {
		t.Fatalf("NaN not repaired: %v", got)
	}
	for _, collection := range sortedCollections {
		for i, want := range values {
			got := fieldRead(t, ctx, db, collection, int32(i))["sort"]
			if want == nil {
				if got != nil {
					t.Fatal("null sort changed")
				}
			} else if s, ok := want.(string); ok {
				if got != s {
					t.Fatal("string sort changed")
				}
			} else if fieldNumber(got) != fieldNumber(want) {
				t.Fatalf("sort %v changed to %v", want, got)
			}
		}
	}
}

// Fixtures derived from models/lib/attachmentKind.js. These exercise its
// precedence rules rather than a platform MIME database (which changes by OS).
func TestFieldsAttachmentKindSourceParity(t *testing.T) {
	cases := []struct {
		name      string
		doc, want bson.M
	}{
		{"query and Windows path", bson.M{"name": `C:\images\Photo.JPEG?size=2#small`}, bson.M{"extension": "jpeg", "ext": "jpeg", "type": "image/jpeg", "isImage": true}},
		{"mime alias wins over name", bson.M{"name": "notes.txt", "mime": "IMAGE/PNG"}, bson.M{"extension": "txt", "ext": "txt", "type": "image/png", "isImage": true}},
		{"existing false flag repaired", bson.M{"name": "movie.mp4", "isVideo": false}, bson.M{"extension": "mp4", "ext": "mp4", "type": "video/mp4", "isVideo": true}},
		{"existing true flag respected", bson.M{"name": "movie.mp4", "isImage": true}, bson.M{"extension": "mp4", "ext": "mp4", "type": "video/mp4", "isVideo": true}},
		{"alternate extension not duplicated", bson.M{"ext": ".PDF"}, bson.M{"type": "application/pdf", "isPDF": true}},
		{"stated MIME not overwritten", bson.M{"type": "APPLICATION/JSON", "name": "data.json"}, bson.M{"extension": "json", "ext": "json", "isJSON": true}},
		{"nested metadata", bson.M{"meta": bson.M{"type": "audio/opus"}}, bson.M{"type": "audio/opus", "isAudio": true}},
		{"unknown extension still preserved", bson.M{"name": "file.xyz"}, bson.M{"extension": "xyz", "ext": "xyz"}},
		{"dotfile has no extension", bson.M{"name": ".png"}, bson.M{}},
		{"unknown binary untouched", bson.M{"name": "README"}, bson.M{}},
		{"office MIME but no invented office flag", bson.M{"name": "report.docx"}, bson.M{"extension": "docx", "ext": "docx", "type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fieldAttachmentKindFix(c.doc); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestFieldsCancellationDoesNotReportSuccessfulRepair(t *testing.T) {
	_, db := migrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, step := range []struct {
		name  string
		check func(context.Context, *mongo.Database) (bool, error)
		run   func(context.Context, *mongo.Database) (Result, error)
	}{
		{"archived", CheckArchivedFlagBackfill, RunArchivedFlagBackfill},
		{"customfields", CheckCustomFieldsBoardIDs, RunCustomFieldsBoardIDs},
		{"defaults", CheckBoardAllowsDefaults, RunBoardAllowsDefaults},
		{"permissions", CheckBoardPermissionLowercase, RunBoardPermissionLowercase},
		{"members", CheckBoardMembersIsActive, RunBoardMembersIsActive},
		{"checklists", CheckChecklistItemsEmbedded, RunChecklistItemsEmbedded},
		{"attachments", CheckAttachmentKindFlags, RunAttachmentKindFlags},
		{"nonfinite", CheckNonfiniteSortRepair, RunNonfiniteSortRepair},
	} {
		t.Run(step.name, func(t *testing.T) {
			if needed, err := step.check(ctx, db); err == nil || needed {
				t.Fatalf("canceled check: %v %v", needed, err)
			}
			if result, err := step.run(ctx, db); err == nil || result.Fixed != 0 {
				t.Fatalf("canceled run: %+v %v", result, err)
			}
		})
	}
}
