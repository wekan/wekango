package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func userReadsFixture(t *testing.T) (*mongo.Database, http.Handler, string) {
	t.Helper()
	server, err := database.Start(context.Background(), database.Config{Directory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := mongo.Connect(options.Client().ApplyURI(server.URI()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	db := client.Database("user_reads")
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"self", "admin", "collision", "username-owner", "nameless", "disabled", "expired", "revoked"} {
		stamp := now
		if id == "expired" {
			stamp = now.Add(-91 * 24 * time.Hour)
		}
		tokens := bson.A{tokenDoc(id, stamp)}
		if id == "revoked" {
			tokens = bson.A{}
		}
		doc := bson.M{"_id": id, "username": id + "-name", "isAdmin": id == "admin", "loginDisabled": id == "disabled", "createdAt": bson.NewDateTimeFromTime(now), "sessionData": bson.M{"internal": "retained-by-self-only"}, "profile": bson.M{"fullname": "Full " + id, "ordered": bson.A{"z-last", "a-first", "middle"}, "nested": bson.A{bson.M{"at": bson.DateTime(0)}}}, "services": bson.M{"password": bson.M{"bcrypt": "secret-password-hash"}, "resume": bson.M{"loginTokens": tokens}, "oauth": bson.M{"accessToken": "secret-oauth"}}}
		if id == "username-owner" {
			doc["username"] = "collision"
		}
		if id == "nameless" {
			delete(doc, "username")
		}
		insert(t, db, "users", doc)
	}
	for _, legacy := range []struct {
		id   any
		name string
	}{{int32(42), "numeric-id"}, {true, "boolean-id"}} {
		insert(t, db, "users", bson.M{"_id": legacy.id, "username": legacy.name, "services": bson.M{"password": bson.M{"bcrypt": "legacy-secret"}}, "sessionData": bson.M{"secret": true}})
		insert(t, db, "boards", bson.M{"_id": legacy.name + "-board", "type": "board", "members": bson.A{bson.M{"userId": legacy.id, "isActive": false}}})
	}
	member := func(active bool) bson.A {
		return bson.A{bson.M{"userId": "self", "isActive": active, "isAdmin": false, "role": "member", "extra": bson.M{"legacy": true, "ordered": bson.A{"second", "first", "third"}}}}
	}
	for _, doc := range []bson.M{
		{"_id": "active", "type": "board", "archived": false, "members": member(true)},
		{"_id": "inactive", "type": "board", "archived": false, "members": member(false)},
		{"_id": "archived", "type": "board", "archived": true, "members": member(true)},
		{"_id": "duplicate", "type": "board", "members": bson.A{bson.M{"userId": "self", "isActive": false, "which": "first"}, bson.M{"userId": "self", "isActive": true, "which": "second"}}},
		{"_id": "template", "type": "template-board", "members": member(true)},
		{"_id": "list", "type": "list", "members": member(true)},
		{"_id": "untyped", "members": member(true)},
		{"_id": "other", "type": "board", "members": bson.A{bson.M{"userId": "collision", "isActive": true}}},
		{"_id": "public", "type": "board", "permission": "public", "members": bson.A{}},
	} {
		insert(t, db, "boards", doc)
	}
	return db, New(db, Options{WithAPI: true}), server.URI()
}

func userReadsJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("invalid JSON %s: %v", raw, err)
	}
	return value
}
func userReadsSnapshot(t *testing.T, db *mongo.Database) any {
	t.Helper()
	out := map[string]any{}
	for _, name := range []string{"users", "boards"} {
		cursor, err := db.Collection(name).Find(context.Background(), bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
		if err != nil {
			t.Fatal(err)
		}
		var rows []bson.M
		if err = cursor.All(context.Background(), &rows); err != nil {
			t.Fatal(err)
		}
		out[name] = jsonDocument(bson.A(func() []any {
			v := make([]any, len(rows))
			for i := range rows {
				v[i] = rows[i]
			}
			return v
		}()))
	}
	return out
}

// Only these two arrays originate from unsorted database queries. Preserve all
// nested arrays verbatim: profile values and membership metadata are ordered.
func userReadsSortRows(value any, key string) any {
	rows, ok := value.([]any)
	if !ok {
		return value
	}
	result := append([]any{}, rows...)
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := result[i].(map[string]any)
		right, _ := result[j].(map[string]any)
		a, _ := json.Marshal(left[key])
		b, _ := json.Marshal(right[key])
		return string(a) < string(b)
	})
	return result
}
func userReadsCanonical(value any) any {
	switch v := value.(type) {
	case []any:
		return userReadsSortRows(v, "_id") // GET /api/users projection
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = item
		}
		if boards, ok := result["boards"]; ok {
			result["boards"] = userReadsSortRows(boards, "boardId")
		}
		return result
	default:
		return value
	}
}

func TestUserReadsPermissionsRedactionMembershipAndPersistence(t *testing.T) {
	db, h, _ := userReadsFixture(t)
	before := userReadsSnapshot(t, db)
	self := request(h, "GET", "/api/user", "self", "", "")
	if self.Code != 200 {
		t.Fatalf("self: %d %s", self.Code, self.Body.String())
	}
	doc := userReadsJSON(t, self.Body.Bytes()).(map[string]any)
	if _, ok := doc["services"]; ok {
		t.Fatal("self leaked credentials")
	}
	if _, ok := doc["sessionData"]; !ok {
		t.Fatal("self compatibility dropped sessionData")
	}
	if got := doc["profile"].(map[string]any)["ordered"]; !reflect.DeepEqual(got, []any{"z-last", "a-first", "middle"}) {
		t.Fatalf("profile array order changed: %#v", got)
	}
	boards := doc["boards"].([]any)
	if len(boards) != 4 {
		t.Fatalf("board membership count: %#v", boards)
	}
	for _, value := range boards {
		m := value.(map[string]any)
		if extra, ok := m["extra"].(map[string]any); ok {
			if !reflect.DeepEqual(extra["ordered"], []any{"second", "first", "third"}) {
				t.Fatalf("membership array order changed: %#v", extra)
			}
		}
		if _, ok := m["userId"]; ok {
			t.Fatal("member userId was not removed")
		}
		if m["boardId"] == "duplicate" && m["which"] != "first" {
			t.Fatalf("duplicate membership order: %#v", m)
		}
	}
	admin := request(h, "GET", "/api/users/self", "admin", "", "")
	if admin.Code != 200 {
		t.Fatalf("admin: %d %s", admin.Code, admin.Body.String())
	}
	adoc := userReadsJSON(t, admin.Body.Bytes()).(map[string]any)
	for _, field := range []string{"services", "sessionData"} {
		if _, ok := adoc[field]; ok {
			t.Errorf("admin leaked %s", field)
		}
	}
	if !reflect.DeepEqual(userReadsSortRows(doc["boards"], "boardId"), userReadsSortRows(adoc["boards"], "boardId")) {
		t.Fatal("self/admin membership semantics differ")
	}
	for _, path := range []string{"/api/users", "/api/users/self", "/api/users/admin"} {
		w := request(h, "GET", path, "self", "", "")
		if w.Code != 403 {
			t.Fatalf("ordinary user crossed admin boundary %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	list := request(h, "GET", "/api/users", "admin", "", "")
	if list.Code != 200 {
		t.Fatal(list.Body.String())
	}
	for _, value := range userReadsJSON(t, list.Body.Bytes()).([]any) {
		m := value.(map[string]any)
		for k := range m {
			if k != "_id" && k != "username" {
				t.Errorf("list exposes %s", k)
			}
		}
		if m["_id"] == "nameless" {
			if _, ok := m["username"]; ok {
				t.Fatal("missing username field invented")
			}
		}
	}
	for _, tc := range []struct{ path, id string }{{"/api/users/collision", "collision"}, {"/api/users/self-name", "self"}, {"/api/users/nameless", "nameless"}} {
		w := request(h, "GET", tc.path, "admin", "", "")
		if w.Code != 200 {
			t.Fatalf("lookup %s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if userReadsJSON(t, w.Body.Bytes()).(map[string]any)["_id"] != tc.id {
			t.Fatalf("ID-before-username lookup %s: %s", tc.path, w.Body.String())
		}
	}
	unknown := request(h, "GET", "/api/users/not-found", "admin", "", "")
	if unknown.Code != 500 || strings.TrimSpace(unknown.Body.String()) != `{"error":"Internal server error"}` {
		t.Fatalf("missing user source-safe error %d %s", unknown.Code, unknown.Body.String())
	}
	if !reflect.DeepEqual(before, userReadsSnapshot(t, db)) {
		t.Fatal("read handler changed stored users/members/credentials")
	}
}

func TestUserReadsRejectDisabledExpiredRevokedAndMissingTokens(t *testing.T) {
	_, h, _ := userReadsFixture(t)
	for _, token := range []string{"disabled", "expired", "revoked", "", "bad-token"} {
		for _, path := range []string{"/api/user", "/api/users", "/api/users/self"} {
			w := request(h, "GET", path, token, "", "")
			if w.Code != 401 {
				t.Errorf("%s %s: %d %s", token, path, w.Code, w.Body.String())
			}
		}
	}
}

func TestUserReadsMalformedMembershipReturnsSafe500(t *testing.T) {
	for name, members := range map[string]any{"object": bson.M{"userId": "self"}, "null-before-member": bson.A{nil, bson.M{"userId": "self"}}} {
		t.Run(name, func(t *testing.T) {
			db, h, _ := userReadsFixture(t)
			insert(t, db, "boards", bson.M{"_id": "malformed", "type": "board", "members": members})
			for _, tc := range []struct{ path, token string }{{"/api/user", "self"}, {"/api/users/self", "admin"}} {
				w := request(h, "GET", tc.path, tc.token, "", "")
				if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"error":"Internal server error"}` {
					t.Fatalf("%s exposed malformed data %d %s", tc.path, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestUserReadsActualJavaScriptDifferential(t *testing.T) {
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("WEKAN_SOURCE_ROOT required for actual user route differential")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	if _, err := exec.LookPath(node); err != nil {
		t.Skip("Node binary unavailable for actual user route differential")
	}
	module := os.Getenv("WEKAN_MONGODB_MODULE")
	if module == "" {
		module = filepath.Join(root, ".tools/wekango/tests/node_modules/mongodb")
	}
	db, h, uri := userReadsFixture(t)
	cases := []map[string]string{{"path": "/api/user", "token": "self", "actor": "self"}, {"path": "/api/user", "token": "admin", "actor": "admin"}, {"path": "/api/users", "token": "admin", "actor": "admin"}, {"path": "/api/users/self", "token": "admin", "actor": "admin", "target": "self"}, {"path": "/api/users/collision", "token": "admin", "actor": "admin", "target": "collision"}, {"path": "/api/users/self-name", "token": "admin", "actor": "admin", "target": "self-name"}, {"path": "/api/users/nameless", "token": "admin", "actor": "admin", "target": "nameless"}, {"path": "/api/users/missing", "token": "admin", "actor": "admin", "target": "missing"}, {"path": "/api/users", "token": "self", "actor": "self"}, {"path": "/api/users/admin", "token": "self", "actor": "self", "target": "admin"}, {"path": "/api/user"}, {"path": "/api/users"}, {"path": "/api/users/self", "target": "self"}}
	for _, name := range []string{"numeric-id", "boolean-id"} {
		cases = append(cases, map[string]string{"path": "/api/users/" + name, "token": "admin", "actor": "admin", "target": name})
	}
	script := `const fs=require('fs'),path=require('path'),vm=require('vm');const root=process.argv[1],{MongoClient}=require(process.argv[2]),input=JSON.parse(fs.readFileSync(0,'utf8'));
(async()=>{const client=new MongoClient(input.uri);await client.connect();try{const db=client.db('user_reads'),routes={};
const ReactiveCache={async getUser(q){return (await db.collection('users').findOne(q))||undefined},async getBoards(q,o){return db.collection('boards').find(q,{projection:o.fields}).toArray()}};
class MeteorError extends Error{constructor(error,reason){super(reason);this.error=error;this.reason=reason}}
const box={ReactiveCache,Meteor:{Error:MeteorError,users:{find(q,o){return {fetchAsync:()=>db.collection('users').find(q,{projection:o.fields}).toArray()}}}},WebApp:{handlers:{get(p,fn){routes[p]=fn}}},sendJsonResult(res,value){res.code=value.code;res.body=value.data}};
vm.createContext(box);
const auth=fs.readFileSync(path.join(root,'server/authentication.js'),'utf8');let start=auth.indexOf('export const Authentication = {'),end=auth.indexOf('\nMeteor.startup',start);if(start<0||end<0)throw Error('auth source boundaries');vm.runInContext(auth.slice(start,end).replace('export const','const'),box);
vm.runInContext(fs.readFileSync(path.join(root,'server/lib/apiResponseHelpers.js'),'utf8').replace(/^export /gm,''),box);
const src=fs.readFileSync(path.join(root,'server/models/users.js'),'utf8');start=src.indexOf("WebApp.handlers.get('/api/user',");end=src.indexOf("WebApp.handlers.put('/api/users/:userId',",start);if(start<0||end<0)throw Error('user source boundaries');vm.runInContext(src.slice(start,end),box);
const results=[];for(const c of input.cases){const route=c.path==='/api/user'?'/api/user':c.path==='/api/users'?'/api/users':'/api/users/:userId';const res={};await routes[route]({userId:c.actor||undefined,params:{userId:c.target}},res);results.push(res)}process.stdout.write(JSON.stringify(results));}finally{await client.close()}})().catch(e=>{console.error(e);process.exit(1)});`
	run := func() {
		before := userReadsSnapshot(t, db)
		input, _ := json.Marshal(map[string]any{"uri": uri, "cases": cases})
		cmd := exec.Command(node, "-e", script, root, module)
		cmd.Stdin = strings.NewReader(string(input))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("actual JS routes: %v %s", err, out)
		}
		var expected []struct {
			Code int `json:"code"`
			Body any `json:"body"`
		}
		if err = json.Unmarshal(out, &expected); err != nil {
			t.Fatal(err)
		}
		for i, c := range cases {
			w := request(h, "GET", c["path"], c["token"], "", "")
			got := userReadsCanonical(userReadsJSON(t, w.Body.Bytes()))
			want := userReadsCanonical(expected[i].Body)
			if w.Code != expected[i].Code || !reflect.DeepEqual(got, want) {
				t.Errorf("%s as %s differs: Go%d %#v JS%d %#v", c["path"], c["actor"], w.Code, got, expected[i].Code, want)
			}
		}
		if !reflect.DeepEqual(before, userReadsSnapshot(t, db)) {
			t.Fatal("Go/JS read routes mutated database")
		}
	}
	run()
	insert(t, db, "boards", bson.M{"_id": "malformed", "type": "board", "members": bson.A{nil, bson.M{"userId": "self"}}})
	run()
	if _, err := db.Collection("boards").UpdateOne(context.Background(), bson.M{"_id": "malformed"}, bson.M{"$set": bson.M{"members": bson.A{bson.M{"userId": "self", "which": "before-null"}, nil}}}); err != nil {
		t.Fatal(err)
	}
	run()
}

func TestUserReadsTrailingSlashAndHEAD(t *testing.T) {
	_, handler, _ := userReadsFixture(t)
	// The installed Meteor WebApp createExpressApp uses express() and does not
	// enable strict routing; Express therefore accepts one optional trailing slash.
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, tc := range []struct{ path, token string }{{"/api/user", "self"}, {"/api/users", "admin"}, {"/api/users/self", "admin"}} {
		ordinary := request(handler, "GET", tc.path, tc.token, "", "")
		trailing := request(handler, "GET", tc.path+"/", tc.token, "", "")
		if ordinary.Code != 200 || trailing.Code != ordinary.Code || !reflect.DeepEqual(userReadsJSON(t, ordinary.Body.Bytes()), userReadsJSON(t, trailing.Body.Bytes())) {
			t.Fatalf("slash alias differs for %s: %d %s", tc.path, trailing.Code, trailing.Body.String())
		}
		for _, path := range []string{tc.path, tc.path + "/"} {
			req, err := http.NewRequest("HEAD", server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+tc.token)
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || len(body) != 0 {
				t.Fatalf("HEAD %s status=%d body=%q error=%v", path, res.StatusCode, body, err)
			}
		}
	}
}

func TestUserReadsStrictImportedMemberIDs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member bson.M
		id     any
		want   bool
	}{
		{"explicit-null", bson.M{"userId": nil}, nil, true},
		{"missing-is-undefined", bson.M{}, nil, false},
		{"bool", bson.M{"userId": true}, true, true},
		{"bool-not-number", bson.M{"userId": true}, int32(1), false},
		{"same-js-number", bson.M{"userId": int32(42)}, float64(42), true},
		{"number-not-string", bson.M{"userId": int32(42)}, "42", false},
		{"different-date-objects", bson.M{"userId": bson.DateTime(0)}, bson.DateTime(0), false},
		{"different-object-values", bson.M{"userId": bson.M{"x": 1}}, bson.M{"x": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := userReadStrictID(tc.member, tc.id); got != tc.want {
				t.Fatalf("JavaScript strict ID equality got %v want %v", got, tc.want)
			}
		})
	}
}
