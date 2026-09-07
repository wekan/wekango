package eventlog

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const Collection = "eventlog"
const maxCachedRows = 500
const usernameCacheMax = 1000
const usernameCacheTTL = time.Minute

type usernameEntry struct {
	username string
	at       time.Time
}

// Writer folds occurrences into the existing WeKan eventlog collection. Share
// one writer for an installation: its context-aware gate serializes actor-cap
// decisions, cache updates and upserts, so concurrent callers cannot overflow
// the cap or race the initial upsert. Independent external writers retain the
// same cross-process limitations as the JavaScript implementation.
type Writer struct {
	db          *mongo.Database
	gate        chan struct{}
	knownActors map[string]map[string]bool
	usernames   map[string]usernameEntry
	now         func() time.Time
}

func NewWriter(db *mongo.Database) *Writer {
	return &Writer{db: db, gate: make(chan struct{}, 1), knownActors: map[string]map[string]bool{}, usernames: map[string]usernameEntry{}, now: time.Now}
}

// Fold ports server/lib/eventLogFold.js. The caller supplies request/DDP
// identity and location fields; database username lookup and IP-family
// enrichment happen here. Errors are returned for the caller's background
// logger to handle, never panicked or written into the event being recorded.
// Like the current JavaScript writer, one call increments once even when doc
// contains count; summary batching is a separate producer concern.
func (w *Writer) Fold(ctx context.Context, doc bson.M) error {
	if w == nil || w.db == nil {
		return errors.New("eventlog writer has no database")
	}
	select {
	case w.gate <- struct{}{}:
		defer func() { <-w.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	evt := make(bson.M, len(doc))
	for key, value := range doc {
		if key != "at" {
			evt[key] = value
		}
	}
	when := w.now()
	switch at := doc["at"].(type) {
	case time.Time:
		when = at
	case bson.DateTime:
		when = at.Time()
	}
	if writerTruthy(evt["userId"]) && !writerTruthy(evt["username"]) {
		if username, err := w.usernameFor(ctx, evt["userId"]); err == nil && username != "" {
			chars := utf16.Encode([]rune(username))
			if len(chars) > 100 {
				chars = chars[:100]
			}
			evt["username"] = string(utf16.Decode(chars))
		}
	}
	if writerTruthy(evt["ip"]) && !writerTruthy(evt["ipv4"]) && !writerTruthy(evt["ipv6"]) {
		ipv4, ipv6 := ClassifyAddress(evt["ip"])
		if ipv4 != "" {
			evt["ipv4"] = ipv4
		}
		if ipv6 != "" {
			evt["ipv6"] = ipv6
		}
	}
	identity := SummaryIdentity(evt)
	encoded, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("eventlog identity: %w", err)
	}
	key := string(encoded)
	known, exists := w.knownActors[key]
	if !exists {
		var row bson.M
		err = w.db.Collection(Collection).FindOne(ctx, identity, options.FindOne().SetProjection(bson.M{"actors": 1})).Decode(&row)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("eventlog actors: %w", err)
		}
		known = map[string]bool{}
		for actor := range writerMap(row["actors"]) {
			known[actor] = true
		}
		if len(w.knownActors) >= maxCachedRows {
			clear(w.knownActors)
		}
		w.knownActors[key] = known
	}
	summary := SummaryUpdate(evt, when, 1)
	actors := ActorUpdate(evt, when, 1, known)
	set, inc := writerMap(summary["$set"]), writerMap(summary["$inc"])
	for k, v := range writerMap(actors["$set"]) {
		set[k] = v
	}
	for k, v := range writerMap(actors["$inc"]) {
		inc[k] = v
	}
	inserted := writerMap(summary["$setOnInsert"])
	id, err := eventID()
	if err != nil {
		return err
	}
	inserted["_id"] = id
	_, err = w.db.Collection(Collection).UpdateOne(ctx, identity, bson.M{"$set": set, "$setOnInsert": inserted, "$inc": inc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		// A timeout may happen after the server applied the write. Reload the
		// authoritative actor set on retry rather than admitting an extra actor.
		delete(w.knownActors, key)
		return fmt.Errorf("eventlog fold: %w", err)
	}
	for field := range writerMap(actors["$inc"]) {
		if strings.HasPrefix(field, "actors.") && strings.HasSuffix(field, ".count") {
			actor := strings.TrimSuffix(strings.TrimPrefix(field, "actors."), ".count")
			if len(actor) == 16 {
				known[actor] = true
			}
		}
	}
	return nil
}

func (w *Writer) usernameFor(ctx context.Context, userID any) (string, error) {
	key := fmt.Sprint(userID)
	now := w.now()
	if entry, ok := w.usernames[key]; ok && now.Sub(entry.at) < usernameCacheTTL {
		return entry.username, nil
	}
	var user struct {
		Username string `bson:"username"`
	}
	err := w.db.Collection("users").FindOne(ctx, bson.M{"_id": userID}, options.FindOne().SetProjection(bson.M{"username": 1})).Decode(&user)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return "", err
	}
	if len(w.usernames) >= usernameCacheMax {
		clear(w.usernames)
	}
	w.usernames[key] = usernameEntry{username: user.Username, at: now}
	return user.Username, nil
}
func writerTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return true
}
func writerMap(value any) bson.M {
	switch v := value.(type) {
	case bson.M:
		return v
	case map[string]any:
		return bson.M(v)
	case bson.D:
		m := bson.M{}
		for _, e := range v {
			m[e.Key] = e.Value
		}
		return m
	}
	return bson.M{}
}
func eventID() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz"
	const limit = 256 - (256 % len(alphabet))
	output := make([]byte, 0, 17)
	for len(output) < 17 {
		var block [17]byte
		if _, err := rand.Read(block[:]); err != nil {
			return "", fmt.Errorf("eventlog id: %w", err)
		}
		for _, b := range block {
			if int(b) < limit {
				output = append(output, alphabet[int(b)%len(alphabet)])
				if len(output) == 17 {
					break
				}
			}
		}
	}
	return string(output), nil
}
