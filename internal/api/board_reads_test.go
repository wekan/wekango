package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func boardReadFixture(t *testing.T) (*mongo.Database, http.Handler) {
	t.Helper()
	db := testDB(t)
	members := bson.A{}
	for _, id := range []string{"member", "admin", "outsider", "inactive", "worker", "comments", "nocomments", "assigned-comments"} {
		insert(t, db, "users", bson.M{"_id": id, "isAdmin": id == "admin", "services": bson.M{"resume": bson.M{"loginTokens": bson.A{tokenDoc(id, time.Now())}}}})
		if id == "outsider" || id == "admin" {
			continue
		}
		members = append(members, bson.M{"userId": id, "isActive": id != "inactive", "isWorker": id == "worker", "isCommentOnly": id == "comments", "isNoComments": id == "nocomments", "isCommentAssignedOnly": id == "assigned-comments"})
	}
	insert(t, db, "boards", bson.M{"_id": "board", "title": "Visible", "permission": "public", "archived": true, "type": "board", "members": members, "sort": 1})
	s := &service{db: db, options: Options{WithAPI: true, LoginExpiration: 90 * 24 * time.Hour}}
	mux := http.NewServeMux()
	s.registerBoardReads(mux)
	return db, mux
}
func boardReadDecode(t *testing.T, wBody []byte) any {
	t.Helper()
	var result any
	if e := json.Unmarshal(wBody, &result); e != nil {
		t.Fatalf("decode %s: %v", wBody, e)
	}
	return result
}

func TestBoardReadAdministratorListings(t *testing.T) {
	db, h := boardReadFixture(t)
	for _, d := range []bson.M{
		{"_id": "hidden-title", "title": "  ^Subtasks^  ", "type": "board", "permission": "public", "sort": 0},
		{"_id": "template", "title": "Template", "type": "template", "permission": "public", "sort": 0},
		{"_id": "untyped", "title": "Untyped", "permission": "public", "sort": 0},
		{"_id": "second", "title": "Another", "type": "board", "permission": "public", "sort": -1},
		{"_id": "private", "title": "Private", "type": "board", "permission": "private"},
	} {
		insert(t, db, "boards", d)
	}
	for _, path := range []string{"/api/boards", "/api/boards_count"} {
		for _, tc := range []struct {
			token  string
			status int
		}{{"", 401}, {"member", 403}, {"outsider", 403}, {"admin", 200}} {
			w := request(h, "GET", path, tc.token, "", "")
			if w.Code != tc.status {
				t.Fatalf("%s %s: %d %s", path, tc.token, w.Code, w.Body.String())
			}
			if tc.status != 200 {
				want := "Forbidden"
				if tc.status == 401 {
					want = "Unauthorized"
				}
				if !reflect.DeepEqual(boardReadDecode(t, w.Body.Bytes()), map[string]any{"error": want}) {
					t.Fatal(w.Body.String())
				}
			}
		}
	}
	got := boardReadDecode(t, request(h, "GET", "/api/boards", "admin", "", "").Body.Bytes())
	want := []any{map[string]any{"_id": "second", "title": "Another"}, map[string]any{"_id": "board", "title": "Visible"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("public listing includes archived real boards, filters helper boards: %#v", got)
	}
	got = boardReadDecode(t, request(h, "GET", "/api/boards_count", "admin", "", "").Body.Bytes())
	if !reflect.DeepEqual(got, map[string]any{"private": float64(1), "public": float64(5)}) {
		t.Fatalf("counts intentionally include helper/archived/untyped boards: %#v", got)
	}
	// This route also requires security-event folding before its full port.
	if w := request(h, "GET", "/api/users/member/boards", "member", "", ""); w.Code != 404 {
		t.Fatal("pending user listing was exposed")
	}
}

func TestBoardReadRoleAndLegacyErrorSemantics(t *testing.T) {
	_, h := boardReadFixture(t)
	for _, path := range []string{"/api/boards/board/lists", "/api/boards/board/swimlanes"} {
		for _, tc := range []struct {
			token   string
			allowed bool
			code    int
		}{{"member", true, 0}, {"admin", true, 0}, {"assigned-comments", true, 0}, {"outsider", false, 403}, {"inactive", false, 403}, {"worker", false, 403}, {"comments", false, 403}, {"nocomments", false, 403}, {"", false, 401}, {"invalid", false, 401}} {
			w := request(h, "GET", path, tc.token, "", "")
			if w.Code != 200 {
				t.Fatalf("legacy status %d %s", w.Code, w.Body.String())
			}
			got := boardReadDecode(t, w.Body.Bytes())
			if tc.allowed {
				if !reflect.DeepEqual(got, []any{}) {
					t.Fatalf("%s got %#v", tc.token, got)
				}
			} else {
				name := "Forbidden"
				if tc.code == 401 {
					name = "Unauthorized"
				}
				want := map[string]any{"isClientSafe": true, "error": name, "reason": name, "message": name + " [" + name + "]", "errorType": "Meteor.Error", "statusCode": float64(tc.code)}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s %s got %#v", path, tc.token, got)
				}
			}
		}
	}
	w := request(h, "GET", "/api/boards/missing/lists", "admin", "", "")
	got := boardReadDecode(t, w.Body.Bytes()).(map[string]any)
	if w.Code != 200 || got["statusCode"] != float64(404) || got["reason"] != "Board not found" {
		t.Fatal(w.Body.String())
	}
}

func TestBoardReadListsAndSwimlanesProjectionAndBoundaries(t *testing.T) {
	db, h := boardReadFixture(t)
	at := bson.NewDateTimeFromTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, coll := range []string{"lists", "swimlanes"} {
		for _, d := range []bson.M{{"_id": "visible", "boardId": "board", "archived": false, "title": "Title", "modifiedAt": at, "custom": "retained"}, {"_id": "empty", "boardId": "board", "archived": false, "title": "Empty"}, {"_id": "hidden", "boardId": "board", "archived": true, "title": "Archived"}, {"_id": "missing-flag", "boardId": "board", "title": "Legacy"}, {"_id": "foreign", "boardId": "other", "archived": false, "title": "Foreign"}} {
			insert(t, db, coll, d)
		}
	}
	for _, d := range []bson.M{{"_id": "current", "boardId": "board", "listId": "visible", "modifiedAt": at}, {"_id": "archived", "boardId": "board", "listId": "visible", "archived": true, "modifiedAt": "2021-01-01T00:00:00Z", "dateLastActivity": int64(1640995200000)}, {"_id": "invalid", "boardId": "board", "listId": "visible", "modifiedAt": "bad date"}, {"_id": "foreign", "boardId": "other", "listId": "visible", "modifiedAt": int64(9999999999999)}} {
		insert(t, db, "cards", d)
	}
	got := boardReadDecode(t, request(h, "GET", "/api/boards/board/lists", "member", "", "").Body.Bytes()).([]any)
	if len(got) != 2 {
		t.Fatal(got)
	}
	first := got[0].(map[string]any)
	if len(first) != 4 || first["modifiedAt"] != "2020-01-01T00:00:00.000Z" || first["cardsModifiedAt"] != "2022-01-01T00:00:00.000Z" {
		t.Fatalf("dates/projection: %#v", got)
	}
	if got[1].(map[string]any)["cardsModifiedAt"] != nil {
		t.Fatal(got)
	}
	got = boardReadDecode(t, request(h, "GET", "/api/boards/board/swimlanes", "member", "", "").Body.Bytes()).([]any)
	if len(got) != 2 || len(got[0].(map[string]any)) != 2 {
		t.Fatalf("swimlane summary %#v", got)
	}
	for _, coll := range []string{"lists", "swimlanes"} {
		w := request(h, "GET", "/api/boards/board/"+coll+"/visible", "member", "", "")
		full := boardReadDecode(t, w.Body.Bytes()).(map[string]any)
		if full["custom"] != "retained" || full["modifiedAt"] != "2020-01-01T00:00:00.000Z" {
			t.Fatal(full)
		}
		for _, id := range []string{"hidden", "foreign", "missing", "missing-flag"} {
			w = request(h, "GET", "/api/boards/board/"+coll+"/"+id, "member", "", "")
			if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
				t.Fatalf("missing child %s/%s =%d %q", coll, id, w.Code, w.Body.String())
			}
		}
	}
}

func TestBoardReadCardsSortedProjectedAndArchivedLookup(t *testing.T) {
	db, h := boardReadFixture(t)
	for _, d := range []bson.M{
		{"_id": "later", "boardId": "board", "listId": "list", "swimlaneId": "lane", "archived": false, "sort": 5, "title": "Later", "description": "Text", "assignees": bson.A{"member"}, "dueAt": bson.DateTime(0), "privateField": "full document only"},
		{"_id": "earlier", "boardId": "board", "listId": "list", "swimlaneId": "lane", "archived": false, "sort": -1, "title": "Earlier"},
		{"_id": "archived", "boardId": "board", "listId": "list", "swimlaneId": "lane", "archived": true, "title": "Archived"},
		{"_id": "foreign", "boardId": "other", "listId": "list", "swimlaneId": "lane", "archived": false, "title": "Foreign"},
		{"_id": "legacy", "boardId": "board", "listId": "list", "swimlaneId": "lane", "title": "Missing archive flag"},
	} {
		insert(t, db, "cards", d)
	}
	for _, scope := range []struct{ path, include, exclude string }{{"lists/list", "swimlaneId", "listId"}, {"swimlanes/lane", "listId", "swimlaneId"}} {
		path := "/api/boards/board/" + scope.path + "/cards"
		w := request(h, "GET", path, "member", "", "")
		docs := boardReadDecode(t, w.Body.Bytes()).([]any)
		if len(docs) != 2 || docs[0].(map[string]any)["_id"] != "earlier" || docs[1].(map[string]any)["_id"] != "later" {
			t.Fatalf("sort/archive/board filters: %s", w.Body.String())
		}
		early, late := docs[0].(map[string]any), docs[1].(map[string]any)
		if _, ok := early["description"]; ok {
			t.Fatal("undefined property serialized")
		}
		if _, ok := late[scope.exclude]; ok {
			t.Fatal("wrong grouping projection")
		}
		if late[scope.include] == nil || late["privateField"] != nil || late["dueAt"] != "1970-01-01T00:00:00.000Z" {
			t.Fatal(late)
		}
		for _, tc := range []struct {
			token  string
			status int
		}{{"admin", 200}, {"assigned-comments", 200}, {"outsider", 403}, {"worker", 403}, {"comments", 403}, {"nocomments", 403}, {"inactive", 403}, {"", 401}} {
			w = request(h, "GET", path, tc.token, "", "")
			if w.Code != tc.status {
				t.Fatalf("%s %s=%d", path, tc.token, w.Code)
			}
			if tc.status != 200 {
				if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(w.Body.String(), "<pre>"+http.StatusText(tc.status)+"</pre>") || strings.Contains(w.Body.String(), "Later") {
					t.Fatal(w.Body.String())
				}
			}
		}
	}
	for _, id := range []string{"archived", "legacy", "later"} {
		w := request(h, "GET", "/api/cards/"+id, "member", "", "")
		if w.Code != 200 || boardReadDecode(t, w.Body.Bytes()).(map[string]any)["_id"] != id {
			t.Fatalf("direct card includes archived/no-flag: %s", w.Body.String())
		}
	}
	w := request(h, "GET", "/api/cards/later", "member", "", "")
	if boardReadDecode(t, w.Body.Bytes()).(map[string]any)["privateField"] != "full document only" {
		t.Fatal(w.Body.String())
	}
	for _, path := range []string{"/api/boards/board/lists/list/cards/archived", "/api/boards/board/lists/list/cards/foreign", "/api/boards/board/lists/wrong/cards/later"} {
		w := request(h, "GET", path, "member", "", "")
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatalf("scoped card %s=%d %s", path, w.Code, w.Body.String())
		}
	}
	w = request(h, "GET", "/api/cards/does-not-exist", "", "", "")
	if w.Code != 404 || !reflect.DeepEqual(boardReadDecode(t, w.Body.Bytes()), map[string]any{"error": "Card not found"}) {
		t.Fatalf("missing direct card before auth: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "GET", "/api/cards/archived", "outsider", "", ""); w.Code != 403 {
		t.Fatal(w.Body.String())
	}
	if w = request(h, "GET", "/api/boards/missing/lists/list/cards", "admin", "", ""); w.Code != 404 {
		t.Fatal(w.Body.String())
	}
}
