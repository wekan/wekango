package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// Code scanning alert #3: Meteor's prehash is computed in one place, through
// the crypto package's registry, and no other fast hash in this repository's
// own Go code touches a password - as caddy-wekan-plugin pins for its #9 and
// #163 and WeKan for its breach-check digest (#599).
func TestNoFastHashOfPasswords(t *testing.T) {
	want := sha256.Sum256([]byte("pässwörd"))
	if meteorPreHash("pässwörd") != hex.EncodeToString(want[:]) {
		t.Fatal("meteorPreHash is Meteor's SHA-256 hex digest, UTF-8 too")
	}
	if meteorPreHash("") != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("meteorPreHash of the empty password")
	}
	fast := regexp.MustCompile(`\b(?:md5|sha1|sha256|sha512|sha3)\.(?:Sum\w*|New\w*)\(|crypto\.(?:MD5|SHA1|SHA256|SHA512)\.New\(`)
	pass := regexp.MustCompile(`(?i)pass`)
	// The negative test: the scan sees the shape alert #3 reported.
	if !fast.MatchString(`digest := sha256.Sum256([]byte(password))`) || !pass.MatchString(`digest := sha256.Sum256([]byte(password))`) {
		t.Fatal("the scan misses the old shape")
	}
	root := filepath.Join("..", "..")
	var offenders []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			// internal/compat holds vendored third-party modules (FerretDB and
			// others), not WeKan's login code.
			if rel == filepath.Join("internal", "compat") || strings.HasPrefix(d.Name(), ".") && rel != "." || d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		isPrehash := rel == filepath.Join("internal", "api", "meteor_prehash.go")
		if !isPrehash && strings.Contains(text, "crypto.SHA256") {
			offenders = append(offenders, rel+": crypto.SHA256 outside meteor_prehash.go")
		}
		for i, line := range strings.Split(text, "\n") {
			if !isPrehash && fast.MatchString(line) && pass.MatchString(line) {
				offenders = append(offenders, fmt.Sprintf("%s:%d %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		if isPrehash && (strings.Contains(text, "sha256.Sum") || !strings.Contains(text, "crypto.SHA256.New()")) {
			offenders = append(offenders, rel+": the prehash is computed through the crypto package's registry only")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d Go files", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("a fast hash next to a password:\n%s", strings.Join(offenders, "\n"))
	}
}
