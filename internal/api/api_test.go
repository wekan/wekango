package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

func testDB(t *testing.T) *mongo.Database {
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
	return client.Database("wekan")
}
func insert(t *testing.T, db *mongo.Database, coll string, doc bson.M) {
	t.Helper()
	if _, err := db.Collection(coll).InsertOne(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
}
func request(handler http.Handler, method, path, token, body, media string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if media != "" {
		r.Header.Set("Content-Type", media)
	} else {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func tokenDoc(token string, when time.Time) bson.M {
	return bson.M{"hashedToken": tokenHash(token), "when": when}
}
func TestBoardAuthorizationExistingMeteorTokens(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	for _, id := range []string{"member", "outsider", "inactive", "worker", "comments", "nocomments", "admin", "disabled", "expired", "revoked"} {
		when := now
		if id == "expired" {
			when = now.Add(-91 * 24 * time.Hour)
		}
		tokens := bson.A{tokenDoc(id, when)}
		if id == "revoked" {
			tokens = bson.A{}
		}
		insert(t, db, "users", bson.M{"_id": id, "isAdmin": id == "admin", "loginDisabled": id == "disabled", "services": bson.M{"resume": bson.M{"loginTokens": tokens}}})
	}
	members := bson.A{}
	for _, id := range []string{"member", "inactive", "worker", "comments", "nocomments", "disabled", "expired", "revoked"} {
		members = append(members, bson.M{"userId": id, "isActive": id != "inactive", "isWorker": id == "worker", "isCommentOnly": id == "comments", "isNoComments": id == "nocomments"})
	}
	insert(t, db, "boards", bson.M{"_id": "board", "title": "Original board", "permission": "public", "members": members})
	h := New(db, Options{WithAPI: true})
	for _, tc := range []struct {
		token string
		code  int
	}{{"member", 200}, {"admin", 200}, {"outsider", 403}, {"inactive", 403}, {"worker", 403}, {"comments", 403}, {"nocomments", 403}, {"disabled", 401}, {"expired", 401}, {"revoked", 401}, {"", 401}, {"invalid", 401}} {
		t.Run(tc.token, func(t *testing.T) {
			w := request(h, "GET", "/api/boards/board", tc.token, "", "")
			if w.Code != tc.code {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if tc.code == 200 && !strings.Contains(w.Body.String(), "Original board") {
				t.Fatal(w.Body.String())
			}
		})
	}
	var decoded struct {
		Members []map[string]any `json:"members"`
	}
	if err := json.Unmarshal(request(h, "GET", "/api/boards/board", "member", "", "").Body.Bytes(), &decoded); err != nil || len(decoded.Members) == 0 || decoded.Members[0]["userId"] != "member" {
		t.Fatalf("nested document JSON: %#v %v", decoded, err)
	}
	if w := request(h, "GET", "/api/boards/missing", "member", "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/boards/board?access_token=member", "", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(h, "DELETE", "/api/boards/board", "member", "", ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(New(db, Options{}), "GET", "/api/boards/board", "member", "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	var b bson.M
	if err := db.Collection("boards").FindOne(context.Background(), bson.M{"_id": "board"}).Decode(&b); err != nil || b["title"] != "Original board" {
		t.Fatalf("board changed: %v %v", b, err)
	}
}
func passwordHash(t *testing.T) string {
	t.Helper()
	digest := sha256.Sum256([]byte("test password"))
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(digest[:])), 10)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}
func loginUser(t *testing.T, db *mongo.Database, id, hash string, extra bson.M) {
	t.Helper()
	doc := bson.M{"_id": id, "username": id, "emails": bson.A{bson.M{"address": id + "@example.com"}}, "services": bson.M{"password": bson.M{"bcrypt": hash}, "resume": bson.M{"loginTokens": bson.A{tokenDoc("old-"+id, time.Now())}}}}
	for k, v := range extra {
		doc[k] = v
	}
	insert(t, db, "users", doc)
}
func TestLoginLogoutExistingPasswordAndTokenStorage(t *testing.T) {
	db := testDB(t)
	loginUser(t, db, "local", passwordHash(t), nil)
	// Verification tokens are ordinary password-account metadata, not an SSO service.
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "local"}, bson.M{"$set": bson.M{"services.email.verificationTokens": bson.A{}}}); err != nil {
		t.Fatal(err)
	}

	h := New(db, Options{WithAPI: true})
	w := request(h, "POST", "/users/login", "", `{"username":"local","password":"test password"}`, "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result struct {
		ID, Token    string
		TokenExpires time.Time
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "local" || result.Token == "" || time.Until(result.TokenExpires) < 89*24*time.Hour {
		t.Fatalf("bad response %#v", result)
	}
	var u user
	if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "local"}).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if len(u.Services.Resume.Tokens) != 2 || u.Services.Resume.Tokens[1].Hash != tokenHash(result.Token) {
		t.Fatalf("not Meteor token storage %#v", u.Services.Resume.Tokens)
	}
	w = request(h, "POST", "/users/logout", result.Token, `{}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/users/logout", result.Token, `{}`, ""); w.Code != 401 {
		t.Fatal("revoked token accepted", w.Code)
	}
	w = request(h, "POST", "/users/login", "", "email=local%40example.com&password=test+password", "application/x-www-form-urlencoded")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", "/users/logout", result.Token, "all=yes", "application/x-www-form-urlencoded")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/users/logout", "old-local", `{}`, ""); w.Code != 401 {
		t.Fatal("all failed to revoke older token", w.Code)
	}
}
func TestLoginRejectsUnsupportedAccountsAndThrottles(t *testing.T) {
	db := testDB(t)
	hash := passwordHash(t)
	loginUser(t, db, "local", hash, nil)
	loginUser(t, db, "ldap", hash, bson.M{"authenticationMethod": "ldap"})
	loginUser(t, db, "oidc", hash, bson.M{"authenticationMethod": "oidc"})
	loginUser(t, db, "disabled", hash, bson.M{"loginDisabled": true})
	loginUser(t, db, "twofactor", hash, nil)
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "twofactor"}, bson.M{"$set": bson.M{"services.twoFactorAuthentication": bson.M{"secret": "secret"}}}); err != nil {
		t.Fatal(err)
	}
	loginUser(t, db, "argon2", hash, nil)
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "argon2"}, bson.M{"$set": bson.M{"services.password.argon2": "unsupported-algorithm"}}); err != nil {
		t.Fatal(err)
	}
	h := New(db, Options{LoginMaxFailures: 20})
	for _, id := range []string{"missing", "ldap", "oidc", "disabled", "twofactor", "argon2"} {
		body, _ := json.Marshal(map[string]string{"username": id, "password": "test password"})
		w := request(h, "POST", "/users/login", "", string(body), "")
		if w.Code != 401 || !strings.Contains(w.Body.String(), "Incorrect username, email address or password.") {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body.String())
		}
	}
	h = New(db, Options{LoginMaxFailures: 2, LoginLockout: time.Minute})
	for i := 0; i < 2; i++ {
		w := request(h, "POST", "/users/login", "", `{"username":"local","password":"wrong"}`, "")
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	w := request(h, "POST", "/users/login", "", `{"username":"local","password":"test password"}`, "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code)
	}
}
func TestLoginBodyLimitsAndOperatorInjection(t *testing.T) {
	db := testDB(t)
	h := New(db, Options{})
	for _, body := range []string{`{"username":{"$ne":null},"password":"x"}`, `{"username":"a","password":"b"} {}`, strings.Repeat("x", 17<<10)} {
		w := request(h, "POST", "/users/login", "", body, "")
		if w.Code != 400 && w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}

func TestTrustedProxyKeyAndThrottleReset(t *testing.T) {
	s := &service{options: Options{LoginMaxFailures: 2, LoginFailureWindow: time.Minute, LoginLockout: time.Minute}, attempts: map[string]*attempt{}}
	r := httptest.NewRequest("POST", "/users/login", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := s.clientKey(r); got != "192.0.2.1" {
		t.Fatal("trusted spoof header", got)
	}
	s.options.HTTPForwardedCount = 1
	if got := s.clientKey(r); got != "203.0.113.9" {
		t.Fatal(got)
	}
	if !s.begin("client") || s.begin("client") {
		t.Fatal("concurrent attempts allowed")
	}
	s.finish("client", false)
	if !s.begin("client") {
		t.Fatal("first failure locked client")
	}
	s.finish("client", true)
	if !s.begin("client") {
		t.Fatal("success failed to reset")
	}
	s.finish("client", false)
	if !s.begin("client") {
		t.Fatal("success did not clear failure count")
	}
	s.finish("client", false)
	if s.begin("client") {
		t.Fatal("failure limit not enforced")
	}
}

func TestMeteorDateJSON(t *testing.T) {
	for _, stamp := range []int64{0, 1, 1704067200000} {
		data, err := json.Marshal(jsonDocument(bson.M{"createdAt": bson.DateTime(stamp)}))
		if err != nil {
			t.Fatal(err)
		}
		expected := time.UnixMilli(stamp).UTC().Format("2006-01-02T15:04:05.000Z")
		if string(data) != `{"createdAt":"`+expected+`"}` {
			t.Fatal(string(data))
		}
	}
}

func TestIngressIdentityRequiresPrivateTransportOptIn(t *testing.T) {
	s := &service{options: Options{}}
	r := httptest.NewRequest("POST", "/users/login", nil)
	r.RemoteAddr = "192.0.2.1:80"
	r.Header.Set("X-Wekan-Client-IP", "client-via-proxy")
	if got := s.clientKey(r); got != "192.0.2.1" {
		t.Fatal("accepted external private header", got)
	}
	s.options.TrustedClientIPHeader = true
	if got := s.clientKey(r); got != "client-via-proxy" {
		t.Fatal(got)
	}
	r.Header.Del("X-Wekan-Client-IP")
	if got := s.clientKey(r); got != "192.0.2.1" {
		t.Fatal("missing ingress fallback", got)
	}
}
