// Package api implements a deliberately bounded WeKan REST compatibility slice.
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wekan/wekango/internal/clientip"
	"github.com/wekan/wekango/internal/eventlog"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

type Options struct {
	Usage                 *eventlog.Reporter
	TrustedClientIPHeader bool
	WithAPI               bool
	LoginExpiration       time.Duration
	LoginMaxFailures      int
	LoginFailureWindow    time.Duration
	LoginLockout          time.Duration
	HTTPForwardedCount    int
}

type service struct {
	db       *mongo.Database
	options  Options
	mutex    sync.Mutex
	attempts map[string]*attempt
	dummy    []byte
}
type attempt struct {
	failures     int
	since, until time.Time
	busy         bool
}
type loginToken struct {
	Hash string    `bson:"hashedToken"`
	When time.Time `bson:"when"`
}
type user struct {
	ID       string `bson:"_id"`
	IsAdmin  bool   `bson:"isAdmin"`
	Disabled bool   `bson:"loginDisabled"`
	Method   string `bson:"authenticationMethod"`
	Services struct {
		Password struct {
			Bcrypt string `bson:"bcrypt"`
			Argon2 string `bson:"argon2"`
		} `bson:"password"`
		Resume struct {
			Tokens []loginToken `bson:"loginTokens"`
		} `bson:"resume"`
		Email     bson.M         `bson:"email"`
		TwoFactor bson.M         `bson:"twoFactorAuthentication"`
		Other     map[string]any `bson:",inline"`
	} `bson:"services"`
}

// New serves only the routes documented in this package; unsupported routes
// return 404/405. WITH_API gates /api routes, as in Meteor, not /users/login.
func New(db *mongo.Database, opts Options) http.Handler {
	if opts.LoginExpiration <= 0 {
		opts.LoginExpiration = 90 * 24 * time.Hour
	}
	if opts.LoginMaxFailures <= 0 {
		opts.LoginMaxFailures = 10
	}
	if opts.LoginFailureWindow <= 0 {
		opts.LoginFailureWindow = time.Minute
	}
	if opts.LoginLockout <= 0 {
		opts.LoginLockout = time.Minute
	}
	// Match Meteor's default bcrypt cost for missing-account timing equalization.
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-never-accepted"), 10)
	if err != nil {
		panic(err)
	}
	s := &service{db: db, options: opts, attempts: make(map[string]*attempt), dummy: dummy}
	mux := http.NewServeMux()
	s.registerBoardReads(mux)
	mux.HandleFunc("GET /api/boards/{boardID}", s.board)
	mux.HandleFunc("POST /users/login", s.login)
	mux.HandleFunc("POST /users/logout", s.logout)
	for _, path := range []string{"/users/login", "/users/logout"} {
		mux.HandleFunc("OPTIONS "+path, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if strings.HasPrefix(r.URL.Path, "/api") && !opts.WithAPI {
			http.Error(w, "WeKan API is disabled. Set WITH_API=true and restart WeKan.", 403)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		routed := r.WithContext(ctx)
		userID := ""
		count := opts.Usage != nil && eventlog.IsAPIRequest(r.URL.RequestURI())
		if count {
			userID = s.usageUserID(routed)
		}
		mux.ServeHTTP(w, routed)
		if count {
			s.recordUsage(routed, userID)
		}
	})
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, name, reason string) {
	reply(w, status, map[string]string{"error": name, "reason": reason})
}
func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(hash[:])
}
func (s *service) authenticate(r *http.Request) (*user, string, error) {
	token := ""
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = parts[1]
	}
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}
	if token == "" || len(token) > 4096 {
		return nil, "", errors.New("unauthorized")
	}
	hash := tokenHash(token)
	var u user
	if err := s.db.Collection("users").FindOne(r.Context(), bson.M{"services.resume.loginTokens.hashedToken": hash}).Decode(&u); err != nil {
		return nil, "", err
	}
	if u.Disabled || u.ID == "" {
		return nil, "", errors.New("unauthorized")
	}
	now := time.Now()
	for _, t := range u.Services.Resume.Tokens {
		if t.Hash == hash && !t.When.IsZero() && !t.When.After(now) && now.Before(t.When.Add(s.options.LoginExpiration)) {
			return &u, hash, nil
		}
	}
	return nil, "", errors.New("unauthorized")
}
func (s *service) board(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticate(r)
	if err != nil {
		failure(w, 401, "Unauthorized", "Unauthorized")
		return
	}
	var board bson.M
	err = s.db.Collection("boards").FindOne(r.Context(), bson.M{"_id": r.PathValue("boardID")}).Decode(&board)
	if errors.Is(err, mongo.ErrNoDocuments) {
		failure(w, 404, "NotFound", "Board not found")
		return
	}
	if err != nil {
		failure(w, 500, "InternalServerError", "Internal server error")
		return
	}
	allowed := u.IsAdmin
	// This intentionally mirrors checkBoardAccess, including its three role
	// exclusions. The write-capability rules are different and are not exposed.
	if members, ok := board["members"].(bson.A); ok {
		for _, raw := range members {
			m, ok := jsonDocument(raw).(bson.M)
			if ok && m["userId"] == u.ID && m["isActive"] == true && m["isNoComments"] != true && m["isCommentOnly"] != true && m["isWorker"] != true {
				allowed = true
			}
		}
	}
	if !allowed {
		failure(w, 403, "Forbidden", "Forbidden")
		return
	}
	reply(w, 200, jsonDocument(board))
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var values map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		values = map[string]any{}
		for k, v := range r.PostForm {
			if len(v) != 1 {
				return nil, errors.New("duplicate field")
			}
			values[k] = v[0]
		}
		return values, nil
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&values); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON")
	}
	if values == nil {
		return nil, errors.New("object required")
	}
	return values, nil
}
func (s *service) clientKey(r *http.Request) string {
	if s.options.TrustedClientIPHeader {
		if key := r.Header.Get(clientip.Header); key != "" {
			return key
		}
	}
	return clientip.Resolve(r.Header, r.RemoteAddr, int64(s.options.HTTPForwardedCount))
}

// begin/finish bound retained state and serialize password work per address.
func (s *service) begin(key string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	now := time.Now()
	for k, a := range s.attempts {
		if !a.busy && now.After(a.until) && now.Sub(a.since) > s.options.LoginFailureWindow {
			delete(s.attempts, k)
		}
	}
	a := s.attempts[key]
	if a == nil {
		if len(s.attempts) >= 10000 {
			return false
		}
		a = &attempt{since: now}
		s.attempts[key] = a
	}
	if !a.until.IsZero() && !now.Before(a.until) {
		a.failures = 0
		a.since = now
		a.until = time.Time{}
	}
	if a.busy || now.Before(a.until) {
		return false
	}
	a.busy = true
	return true
}
func (s *service) finish(key string, success bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	a := s.attempts[key]
	if a == nil {
		return
	}
	if success {
		delete(s.attempts, key)
		return
	}
	a.busy = false
	a.failures++
	if a.failures >= s.options.LoginMaxFailures {
		a.until = time.Now().Add(s.options.LoginLockout)
	}
}
func (s *service) login(w http.ResponseWriter, r *http.Request) {
	key := s.clientKey(r)
	if !s.begin(key) {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(s.options.LoginLockout.Seconds()))))
		failure(w, 429, "too-many-requests", "Too many failed login attempts. Try again later.")
		return
	}
	success := false
	defer func() { s.finish(key, success) }()
	reject := func() { failure(w, 401, "login-failed", "Incorrect username, email address or password.") }
	body, err := decodeBody(w, r)
	if err != nil {
		failure(w, 400, "bad-request", "Invalid request body")
		return
	}
	password, pok := body["password"].(string)
	username, uok := body["username"].(string)
	email, eok := body["email"].(string)
	if !pok || len(password) > 4096 || (!uok && !eok) || (uok && eok) || len(username) > 1024 || len(email) > 1024 {
		reject()
		return
	}
	for k := range body {
		if k != "username" && k != "email" && k != "password" && k != "code" {
			reject()
			return
		}
	}
	selector := bson.M{"username": username}
	if eok {
		selector = bson.M{"emails.address": email}
	}
	var u user
	err = s.db.Collection("users").FindOne(r.Context(), selector).Decode(&u)
	digest := sha256.Sum256([]byte(password))
	encoded := []byte(hex.EncodeToString(digest[:]))
	local := err == nil && !u.Disabled && (u.Method == "" || u.Method == "password") && u.Services.TwoFactor == nil && len(u.Services.Other) == 0 && u.Services.Password.Bcrypt != "" && u.Services.Password.Argon2 == ""
	hash := s.dummy
	if local {
		hash = []byte(u.Services.Password.Bcrypt)
	}
	match := bcrypt.CompareHashAndPassword(hash, encoded) == nil
	if !local || !match {
		reject()
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		failure(w, 500, "InternalServerError", "Internal server error")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC().Truncate(time.Millisecond)
	// Recheck account policy with the password hash atomically when issuing the
	// token, so a concurrent disable, LDAP switch or 2FA enable cannot be bypassed.
	selector = bson.M{"_id": u.ID, "loginDisabled": bson.M{"$ne": true}, "services.password.bcrypt": u.Services.Password.Bcrypt, "services.password.argon2": bson.M{"$exists": false}, "services.twoFactorAuthentication": bson.M{"$exists": false}, "authenticationMethod": u.Method}
	if u.Method == "" {
		delete(selector, "authenticationMethod")
		selector["$or"] = bson.A{bson.M{"authenticationMethod": ""}, bson.M{"authenticationMethod": bson.M{"$exists": false}}}
	}
	result, err := s.db.Collection("users").UpdateOne(r.Context(), selector, bson.M{"$push": bson.M{"services.resume.loginTokens": bson.M{"hashedToken": tokenHash(token), "when": now}}})
	if err != nil {
		failure(w, 500, "InternalServerError", "Internal server error")
		return
	}
	if result.MatchedCount != 1 {
		reject()
		return
	}
	success = true
	reply(w, 200, map[string]any{"id": u.ID, "token": token, "tokenExpires": now.Add(s.options.LoginExpiration).Format("2006-01-02T15:04:05.000Z")})
}
func (s *service) logout(w http.ResponseWriter, r *http.Request) {
	u, hash, err := s.authenticate(r)
	if err != nil {
		failure(w, 401, "unauthorized", "You must be logged in to log out. Send your token as an Authorization: Bearer header.")
		return
	}
	body, err := decodeBody(w, r)
	if err != nil {
		failure(w, 400, "bad-request", "Invalid request body")
		return
	}
	all := body["all"] == true || body["all"] == float64(1)
	if value, ok := body["all"].(string); ok {
		value = strings.ToLower(strings.TrimSpace(value))
		all = value == "true" || value == "1" || value == "yes"
	}
	modifier := bson.M{"$pull": bson.M{"services.resume.loginTokens": bson.M{"hashedToken": hash}}}
	message := "You've been logged out!"
	if all {
		modifier = bson.M{"$set": bson.M{"services.resume.loginTokens": bson.A{}}}
		message = "All login tokens have been invalidated."
	}
	if _, err := s.db.Collection("users").UpdateOne(r.Context(), bson.M{"_id": u.ID}, modifier); err != nil {
		failure(w, 500, "InternalServerError", "Internal server error")
		return
	}
	reply(w, 200, map[string]string{"message": message})
}

// Driver v2 decodes nested documents as bson.D. REST exposes ordinary JSON
// objects, never the Go driver's ordered Key/Value representation.
func jsonDocument(value any) any {
	switch v := value.(type) {
	case bson.DateTime:
		return v.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	case time.Time:
		return v.UTC().Format("2006-01-02T15:04:05.000Z")
	case bson.D:
		out := bson.M{}
		for _, entry := range v {
			out[entry.Key] = jsonDocument(entry.Value)
		}
		return out
	case bson.M:
		out := bson.M{}
		for key, item := range v {
			out[key] = jsonDocument(item)
		}
		return out
	case bson.A:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = jsonDocument(item)
		}
		return out
	default:
		return value
	}
}
