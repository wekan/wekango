package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestLoginStateJavaScriptDisabledTruthiness(t *testing.T) {
	db := testDB(t)
	hash := passwordHash(t)
	cases := []struct {
		id               string
		state            any
		missing, allowed bool
	}{
		{"missing", nil, true, true}, {"null", nil, false, true}, {"false", false, false, true}, {"empty-string", "", false, true}, {"zero", int32(0), false, true},
		{"true", true, false, false}, {"string-false", "false", false, false}, {"one", int32(1), false, false}, {"empty-array", bson.A{}, false, false}, {"empty-object", bson.M{}, false, false},
	}
	h := New(db, Options{WithAPI: true, LoginMaxFailures: 100})
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			extra := bson.M{}
			if !tc.missing {
				extra["loginDisabled"] = tc.state
			}
			loginUser(t, db, tc.id, hash, extra)
			// Existing Meteor tokens and new password logins must agree on truthiness.
			existing := request(h, "GET", "/api/user", "old-"+tc.id, "", "")
			body, _ := json.Marshal(map[string]string{"username": tc.id, "password": "test password"})
			result := request(h, "POST", "/users/login", "", string(body), "")
			expected := 401
			if tc.allowed {
				expected = 200
			}
			if existing.Code != expected || result.Code != expected {
				t.Fatalf("state=%#v old-token=%d %s password=%d %s", tc.state, existing.Code, existing.Body.String(), result.Code, result.Body.String())
			}
			var stored bson.M
			if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": tc.id}).Decode(&stored); err != nil {
				t.Fatal(err)
			}
			tokens := loginStateMap(loginStateMap(stored["services"])["resume"])["loginTokens"].(bson.A)
			want := 1
			if tc.allowed {
				want = 2
			}
			if len(tokens) != want {
				t.Fatalf("stored token count %d want%d", len(tokens), want)
			}
			if tc.missing {
				if _, ok := stored["loginDisabled"]; ok {
					t.Fatal("login invented disabled field")
				}
			} else {
				got, _ := bson.Marshal(bson.M{"value": stored["loginDisabled"]})
				want, _ := bson.Marshal(bson.M{"value": tc.state})
				if string(got) != string(want) {
					t.Fatalf("login changed disabled state %#v", stored["loginDisabled"])
				}
			}
		})
	}
}
func loginStateMap(v any) bson.M {
	switch x := v.(type) {
	case bson.M:
		return x
	case bson.D:
		r := bson.M{}
		for _, e := range x {
			r[e.Key] = e.Value
		}
		return r
	}
	return nil
}

func TestLoginStateAdminReenableRetainsSecurityMetadata(t *testing.T) {
	db := testDB(t)
	hash := passwordHash(t)
	loginUser(t, db, "reenabled", hash, bson.M{"loginDisabled": true, "profile": bson.M{"fullname": "Keep"}})
	stamp := bson.DateTime(1700000000000)
	block := bson.M{"at": stamp, "reason": "Old refusal", "ip": "192.0.2.4", "bleed": "BoardBleed", "source": "api"}
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "reenabled"}, bson.M{"$set": bson.M{"services.securityBlock": block, "services.email.verificationTokens": bson.A{"keep-verification"}}}); err != nil {
		t.Fatal(err)
	}
	h := New(db, Options{WithAPI: true})
	if w := request(h, "POST", "/users/login", "", `{"username":"reenabled","password":"test password"}`, ""); w.Code != 401 {
		t.Fatal("disabled account admitted", w.Code)
	}
	// Collection2 cleans enableLogin's empty strings into $unset operations.
	// Re-enabling also removes old sessions, while retaining security evidence.
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "reenabled"}, bson.M{"$unset": bson.M{"loginDisabled": "", "services.resume.loginTokens": ""}}); err != nil {
		t.Fatal(err)
	}
	w := request(h, "POST", "/users/login", "", `{"username":"reenabled","password":"test password"}`, "")
	if w.Code != 200 {
		t.Fatalf("reenabled metadata account rejected %d %s", w.Code, w.Body.String())
	}
	var result struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w := request(h, "GET", "/api/user", "old-reenabled", "", ""); w.Code != 401 {
		t.Fatalf("revoked session survived admin re-enable: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/user", result.Token, "", ""); w.Code != 200 {
		t.Fatalf("new session rejected after admin re-enable: %d %s", w.Code, w.Body.String())
	}

	var stored bson.M
	if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "reenabled"}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if _, exists := stored["loginDisabled"]; exists {
		t.Fatal("login recreated removed disable flag")
	}
	services := loginStateMap(stored["services"])
	if tokens := loginStateMap(services["resume"])["loginTokens"].(bson.A); len(tokens) != 1 || loginStateMap(tokens[0])["hashedToken"] != tokenHash(result.Token) {
		t.Fatalf("old tokens retained or new token missing: %#v", tokens)
	}
	actual := loginStateMap(services["securityBlock"])
	for key, want := range block {
		if actual[key] != want {
			t.Fatalf("block metadata %s changed: %#v", key, actual)
		}
	}
	if loginStateMap(services["password"])["bcrypt"] != hash || loginStateMap(stored["profile"])["fullname"] != "Keep" || loginStateMap(services["email"])["verificationTokens"].(bson.A)[0] != "keep-verification" {
		t.Fatalf("unrelated fields changed %#v", stored)
	}
}

func TestLoginStateSecurityMetadataDoesNotBypassUnsupportedAuthentication(t *testing.T) {
	db := testDB(t)
	hash := passwordHash(t)
	h := New(db, Options{WithAPI: true, LoginMaxFailures: 100})
	for _, tc := range []struct {
		id         string
		extra, set bson.M
	}{
		{"ldap", bson.M{"authenticationMethod": "ldap"}, bson.M{}},
		{"two-factor", bson.M{}, bson.M{"services.twoFactorAuthentication": bson.M{"secret": "keep-secret"}}},
		{"unknown-service", bson.M{}, bson.M{"services.unknownProvider": bson.M{"token": "keep-secret"}}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			tc.extra["loginDisabled"] = ""
			loginUser(t, db, tc.id, hash, tc.extra)
			tc.set["services.securityBlock"] = bson.M{"reason": "historical"}
			if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": tc.id}, bson.M{"$set": tc.set}); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"username": tc.id, "password": "test password"})
			w := request(h, "POST", "/users/login", "", string(body), "")
			if w.Code != 401 || !strings.Contains(w.Body.String(), "Incorrect username, email address or password.") {
				t.Fatalf("unsupported policy bypass %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLoginStateConcurrentDisablePreventsTokenIssuance(t *testing.T) {
	for _, state := range []any{true, "false", bson.A{}, bson.M{}} {
		t.Run(func() string { b, _ := json.Marshal(state); return string(b) }(), func(t *testing.T) {
			server, err := database.Start(context.Background(), database.Config{Directory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			admin, err := mongo.Connect(options.Client().ApplyURI(server.URI()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = admin.Disconnect(context.Background()) })
			db := admin.Database("login_state_race")
			loginUser(t, db, "racing", passwordHash(t), bson.M{"loginDisabled": false})
			var intercepted atomic.Bool
			var disableErr error
			monitor := &event.CommandMonitor{Started: func(ctx context.Context, ev *event.CommandStartedEvent) {
				if ev.CommandName != "update" || !intercepted.CompareAndSwap(false, true) {
					return
				}
				bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				_, disableErr = db.Collection("users").UpdateOne(bounded, bson.M{"_id": "racing"}, bson.M{"$set": bson.M{"loginDisabled": state}})
			}}
			client, err := mongo.Connect(options.Client().ApplyURI(server.URI()).SetMonitor(monitor))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
			h := New(client.Database(db.Name()), Options{WithAPI: true})
			w := request(h, "POST", "/users/login", "", `{"username":"racing","password":"test password"}`, "")
			if !intercepted.Load() || disableErr != nil {
				t.Fatalf("race setup did not disable before token write: %v %v", intercepted.Load(), disableErr)
			}
			if w.Code != 401 {
				t.Fatalf("concurrent truthy disable bypassed %d %s", w.Code, w.Body.String())
			}
			var stored bson.M
			if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "racing"}).Decode(&stored); err != nil {
				t.Fatal(err)
			}
			if tokens := loginStateMap(loginStateMap(stored["services"])["resume"])["loginTokens"].(bson.A); len(tokens) != 1 {
				t.Fatalf("token inserted after disable: %#v", tokens)
			}
		})
	}
}

func TestLoginStateRejectsMalformedRawTokenString(t *testing.T) {
	db := testDB(t)
	loginUser(t, db, "malformed-tokens", passwordHash(t), nil)
	if _, err := db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": "malformed-tokens"}, bson.M{"$set": bson.M{"services.resume.loginTokens": ""}}); err != nil {
		t.Fatal(err)
	}
	h := New(db, Options{WithAPI: true})
	w := request(h, "POST", "/users/login", "", `{"username":"malformed-tokens","password":"test password"}`, "")
	if w.Code != 401 {
		t.Fatalf("malformed raw token value admitted %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/user", "old-malformed-tokens", "", ""); w.Code != 401 {
		t.Fatalf("invalid old session admitted %d", w.Code)
	}
	var stored bson.M
	if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "malformed-tokens"}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if loginStateMap(loginStateMap(stored["services"])["resume"])["loginTokens"] != "" {
		t.Fatal("rejected login rewrote malformed session data")
	}
}

func TestLoginStateConcurrentSuccessfulLoginsPreserveBothTokens(t *testing.T) {
	db := testDB(t)
	loginUser(t, db, "concurrent", passwordHash(t), nil)
	h := New(db, Options{WithAPI: true})
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, address := range []string{"192.0.2.11:1234", "192.0.2.12:1234"} {
		go func(address string) {
			<-start
			r := httptest.NewRequest("POST", "/users/login", strings.NewReader(`{"username":"concurrent","password":"test password"}`))
			r.RemoteAddr = address
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			results <- w
		}(address)
	}
	close(start)
	tokens := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case w := <-results:
			if w.Code != 200 {
				t.Fatalf("concurrent login failed %d %s", w.Code, w.Body.String())
			}
			var result struct{ Token string }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Token == "" || tokens[result.Token] {
				t.Fatal("missing or reused session token")
			}
			tokens[result.Token] = true
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent logins did not finish")
		}
	}
	for token := range tokens {
		if w := request(h, "GET", "/api/user", token, "", ""); w.Code != 200 {
			var current bson.M
			if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "concurrent"}).Decode(&current); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("concurrent issued session lost %d %s; expected hash %s; stored sessions %#v", w.Code, w.Body.String(), tokenHash(token), loginStateMap(loginStateMap(current["services"])["resume"])["loginTokens"])
		}
	}
	var stored bson.M
	if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "concurrent"}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if values := loginStateMap(loginStateMap(stored["services"])["resume"])["loginTokens"].(bson.A); len(values) != 3 {
		t.Fatalf("atomic token append lost old/new session: %#v", values)
	}
}
