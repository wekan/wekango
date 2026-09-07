package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

func TestMeteorPasswordRequiresBcrypt(t *testing.T) {
	password := strings.Repeat("a", 100) + "suffix"
	digest := sha256.Sum256([]byte(password))
	encoded := []byte(hex.EncodeToString(digest[:]))
	first, err := bcrypt.GenerateFromPassword(encoded, bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	second, err := bcrypt.GenerateFromPassword(encoded, bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("bcrypt salt reused")
	}
	for _, hash := range [][]byte{first, second} {
		if !compareMeteorPassword(hash, password) {
			t.Fatal("existing Meteor verifier rejected")
		}
		if compareMeteorPassword(hash, password+"changed-after-72-bytes") {
			t.Fatal("password suffix ignored")
		}
		if compareMeteorPassword(hash, string(encoded)) {
			t.Fatal("prehash accepted as password")
		}
	}
	for _, hash := range [][]byte{encoded, digest[:], []byte(password), nil, []byte("$2a$10$malformed")} {
		if compareMeteorPassword(hash, password) {
			t.Fatal("non-bcrypt verifier accepted")
		}
	}
}

func TestLoginSelectorsEncodeOnlyLiteralBSONStrings(t *testing.T) {
	for _, value := range []string{`{"$ne":null}`, `{"$where":"return true"}`, `' OR 1=1 --`, "$regex", "雪@example.com"} {
		for _, tc := range []struct {
			key      string
			selector any
		}{{"username", usernameLookup{value}}, {"emails.address", emailLookup{value}}} {
			data, err := bson.Marshal(tc.selector)
			if err != nil {
				t.Fatal(err)
			}
			elements, err := bson.Raw(data).Elements()
			if err != nil || len(elements) != 1 {
				t.Fatalf("unexpected selector %v %v", elements, err)
			}
			if elements[0].Key() != tc.key || elements[0].Value().Type != bson.TypeString || elements[0].Value().StringValue() != value {
				t.Fatalf("selector interpreted as query: %v", elements)
			}
		}
	}
}

func TestLoginSelectorsRejectOperators(t *testing.T) {
	db := testDB(t)
	loginUser(t, db, "victim", passwordHash(t), nil)
	h := New(db, Options{LoginMaxFailures: 100})
	for _, body := range []string{
		`{"username":{"$ne":null},"password":"test password"}`,
		`{"email":{"$regex":".*"},"password":"test password"}`,
		`{"username":["victim"],"password":"test password"}`,
		`{"username":"victim","password":{"$ne":null}}`,
		`{"username":"victim","password":"test password","$where":"return true"}`,
		`{"username":"{\"$ne\":null}","password":"test password"}`,
	} {
		if response := request(h, "POST", "/users/login", "", body, ""); response.Code != 401 {
			t.Fatalf("operator input accepted: %d %s", response.Code, response.Body.String())
		}
	}
	for _, form := range []string{"username%5B%24ne%5D=x&password=test+password", "email%5B%24regex%5D=.*&password=test+password"} {
		if response := request(h, "POST", "/users/login", "", form, "application/x-www-form-urlencoded"); response.Code != 401 {
			t.Fatalf("form operator accepted: %d", response.Code)
		}
	}
	var victim user
	if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": "victim"}).Decode(&victim); err != nil {
		t.Fatal(err)
	}
	if len(victim.Services.Resume.Tokens) != 1 {
		t.Fatal("attack issued a session")
	}
	// Metacharacters remain valid literal data; never reject real names merely
	// for resembling query syntax, or reinterpret them as selectors.
	literal := `{"$ne":null}`
	loginUser(t, db, literal, passwordHash(t), nil)
	for _, media := range []string{"application/json", "application/x-www-form-urlencoded"} {
		body, _ := json.Marshal(map[string]string{"username": literal, "password": "test password"})
		if media != "application/json" {
			body = []byte(url.Values{"username": {literal}, "password": {"test password"}}.Encode())
		}
		response := request(h, "POST", "/users/login", "", string(body), media)
		if response.Code != 200 {
			t.Fatalf("literal name rejected: %d %s", response.Code, response.Body.String())
		}
	}
}
