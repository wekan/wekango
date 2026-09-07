package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestUserBoardsSourcePolicy(t *testing.T) {
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("requires reference WeKan source")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	const script = `const fs=require('fs'),path=require('path');const root=process.argv[1];const cats=fs.readFileSync(path.join(root,'models/lib/securityCategories.js'),'utf8').replace(/export\s*\{[^}]*\};?/g,'');const categoryFor=new Function(cats+';return categoryFor')();const src=fs.readFileSync(path.join(root,'server/lib/blockOnSecurityEvent.js'),'utf8').replace(/^import .*$/gm,'').replace(/^export /gm,'');const policy=new Function('require','Meteor',src+';return {shouldBlockAccount}')(()=>({categoryFor}),{});console.log(JSON.stringify({category:categoryFor('authz.board-list'),blocked:policy.shouldBlockAccount({userId:'member',key:'authz.board-list',action:'blocked'}),high:policy.shouldBlockAccount({userId:'member',severity:'high',action:'blocked'})}));`
	raw, err := exec.Command(node, "-e", script, root).CombinedOutput()
	if err != nil {
		t.Fatalf("source policy: %v %s", err, raw)
	}
	var got map[string]any
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"category": map[string]any{"category": "authz", "bleed": "StaleBleed", "severity": "medium", "cwe": "CWE-863"}, "blocked": false, "high": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source policy changed; review logging and account consequences: %#v", got)
	}
}

func userBoardsFixture(t *testing.T) (*mongo.Database, http.Handler) {
	t.Helper()
	db := testDB(t)
	for _, id := range []string{"member", "admin", "other", "disabled"} {
		insert(t, db, "users", bson.M{"_id": id, "username": id + "-name", "isAdmin": id == "admin", "loginDisabled": id == "disabled", "services": bson.M{"resume": bson.M{"loginTokens": bson.A{tokenDoc(id, time.Now())}}}})
	}
	active := bson.A{bson.M{"userId": "member", "isActive": true}}
	for _, doc := range []bson.M{
		{"_id": "later", "title": "Later board", "type": "board", "archived": false, "sort": 10, "members": active, "secret": "not in projection"},
		{"_id": "earlier", "title": "Earlier board", "type": "board", "archived": false, "sort": -5, "members": active},
		{"_id": "revoked", "title": "Secret revoked title", "type": "board", "archived": false, "members": bson.A{bson.M{"userId": "member", "isActive": false}, bson.M{"userId": "other", "isActive": true}}},
		{"_id": "revoked-helper", "title": "^Secret helper^", "type": "template-board", "archived": false, "members": bson.A{bson.M{"userId": "member", "isActive": false}}},
		{"_id": "revoked-archived", "title": "Secret archived revoked", "type": "board", "archived": true, "members": bson.A{bson.M{"userId": "member", "isActive": false}}},
		{"_id": "archived", "title": "Archived active", "type": "board", "archived": true, "members": active},
		{"_id": "missing-archived", "title": "Legacy archive flag", "type": "board", "members": active},
		{"_id": "missing-active", "title": "Legacy membership", "type": "board", "archived": false, "members": bson.A{bson.M{"userId": "member"}, bson.M{"userId": "other", "isActive": true}}},
		{"_id": "string-active", "title": "String active flag", "type": "board", "archived": false, "members": bson.A{bson.M{"userId": "member", "isActive": "true"}}},
		{"_id": "template", "title": "Template", "type": "template-board", "archived": false, "members": active},
		{"_id": "list-helper", "title": "Internal list", "type": "list", "archived": false, "members": active},
		{"_id": "caret", "title": "  ^Subtasks^  ", "type": "board", "archived": false, "members": active},
		{"_id": "untyped", "title": "Untyped board", "archived": false, "members": active},
		{"_id": "public-other", "title": "Public other board", "type": "board", "permission": "public", "archived": false, "members": bson.A{bson.M{"userId": "other", "isActive": true}}},
	} {
		insert(t, db, "boards", doc)
	}
	return db, New(db, Options{WithAPI: true})
}

func TestUserBoardsSelfAdminAndStrictActiveMembership(t *testing.T) {
	_, handler := userBoardsFixture(t)
	want := []any{map[string]any{"_id": "earlier", "title": "Earlier board"}, map[string]any{"_id": "later", "title": "Later board"}}
	for _, token := range []string{"member", "admin"} {
		response := request(handler, http.MethodGet, "/api/users/member/boards", token, "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", token, response.Code, response.Body.String())
		}
		if got := boardReadDecode(t, response.Body.Bytes()); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s leaked inactive/helper/archived board, wrong sorting or projection: %#v", token, got)
		}
	}
	// Administrator privilege authorizes reading the target's own memberships;
	// it neither expands that list to all boards nor requires the target to exist.
	response := request(handler, http.MethodGet, "/api/users/nonexistent/boards", "admin", "", "")
	if response.Code != 200 || !reflect.DeepEqual(boardReadDecode(t, response.Body.Bytes()), []any{}) {
		t.Fatalf("missing target %d %s", response.Code, response.Body.String())
	}
}

func TestUserBoardsAuthenticationAndSelfRestriction(t *testing.T) {
	db, handler := userBoardsFixture(t)
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {"invalid", 401}, {"disabled", 401}, {"other", 403}} {
		response := request(handler, http.MethodGet, "/api/users/member/boards", tc.token, "", "")
		if response.Code != tc.code {
			t.Fatalf("%q status %d: %s", tc.token, response.Code, response.Body.String())
		}
		want := "Forbidden"
		if tc.code == 401 {
			want = "Unauthorized"
		}
		if got := boardReadDecode(t, response.Body.Bytes()); !reflect.DeepEqual(got, map[string]any{"error": want}) {
			t.Fatalf("public auth error %#v", got)
		}
		if strings.Contains(response.Body.String(), "board") {
			t.Fatalf("auth denial disclosed board detail %s", response.Body.String())
		}
	}
	if count, err := db.Collection("eventlog").CountDocuments(context.Background(), bson.M{"bleed": "StaleBleed"}); err != nil || count != 0 {
		t.Fatalf("denied caller reached revoked-membership probe %d %v", count, err)
	}
	response := request(New(db, Options{}), http.MethodGet, "/api/users/member/boards", "member", "", "")
	if response.Code != 403 {
		t.Fatalf("WITH_API disabled status %d", response.Code)
	}
}

func userBoardsProblem(t *testing.T, db *mongo.Database, wantCount float64) bson.M {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var row bson.M
		err := db.Collection("eventlog").FindOne(context.Background(), bson.M{"stream": "security", "bleed": "StaleBleed", "source": "GET /api/users/:userId/boards"}).Decode(&row)
		var count float64
		switch n := row["count"].(type) {
		case float64:
			count = n
		case int32:
			count = float64(n)
		case int64:
			count = float64(n)
		}
		if err == nil && count == wantCount {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("security fold count %v want %v, err=%v row=%#v", count, wantCount, err, row)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUserBoardsRevokedAttemptsFoldWithoutBlockingAccount(t *testing.T) {
	db, handler := userBoardsFixture(t)
	var firstAt any
	for count := 1; count <= 2; count++ {
		response := request(handler, http.MethodGet, "/api/users/member/boards", "member", "", "")
		if response.Code != 200 {
			t.Fatalf("repeat lookup blocked account: %d %s", response.Code, response.Body.String())
		}
		row := userBoardsProblem(t, db, float64(count))
		for key, want := range map[string]any{"category": "authz", "bleed": "StaleBleed", "severity": "medium", "action": "blocked", "cwe": "CWE-863", "userId": "member", "username": "member-name", "ipv4": "192.0.2.1", "detail": "withheld 2 board(s) whose membership is revoked"} {
			if row[key] != want {
				t.Errorf("%s=%#v want %#v", key, row[key], want)
			}
		}
		detail, _ := row["detail"].(string)
		for _, secret := range []string{"Secret revoked title", "Secret helper", "revoked-helper"} {
			if strings.Contains(detail, secret) {
				t.Fatalf("security detail leaked withheld board %q", detail)
			}
		}
		if count == 1 {
			firstAt = row["firstAt"]
		} else if !reflect.DeepEqual(firstAt, row["firstAt"]) {
			t.Fatal("repeated event replaced firstAt")
		}
		var user bson.M
		if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "member"}).Decode(&user); err != nil {
			t.Fatal(err)
		}
		if user["loginDisabled"] != false {
			t.Fatalf("medium StaleBleed disabled account %#v", user)
		}
	}
	if count, err := db.Collection("eventlog").CountDocuments(context.Background(), bson.M{"bleed": "StaleBleed"}); err != nil || count != 1 {
		t.Fatalf("repeated attempt split event rows %d %v", count, err)
	}
}

func TestUserBoardsSecurityLoggingFailureDoesNotBreakListing(t *testing.T) {
	db, handler := userBoardsFixture(t)
	// This matching legacy row cannot accept $inc. A report failure must neither
	// reveal revoked boards nor deny otherwise authorized listing access.
	insert(t, db, "eventlog", bson.M{"_id": "malformed-counter", "stream": "security", "category": "authz", "bleed": "StaleBleed", "severity": "medium", "action": "blocked", "source": "GET /api/users/:userId/boards", "cwe": "CWE-863", "count": "invalid"})
	response := request(handler, http.MethodGet, "/api/users/member/boards", "member", "", "")
	want := []any{map[string]any{"_id": "earlier", "title": "Earlier board"}, map[string]any{"_id": "later", "title": "Later board"}}
	if response.Code != 200 || !reflect.DeepEqual(boardReadDecode(t, response.Body.Bytes()), want) {
		t.Fatalf("logging failure altered guard %d %s", response.Code, response.Body.String())
	}
}

func TestUserBoardsHelperTitleWhitespace(t *testing.T) {
	for _, tc := range []struct {
		title   string
		visible bool
	}{
		{"^helper^", false}, {"\ufeff^helper^\ufeff", false}, {"\u0085^helper^", true},
		{"^line\rbreak^", true}, {"^line\u2028break^", true}, {"normal", true},
	} {
		got := boardReadVisibleSummaries([]bson.M{{"_id": "test", "type": "board", "title": tc.title}})
		if (len(got) == 1) != tc.visible {
			t.Fatalf("%q visibility %#v", tc.title, got)
		}
	}
}
