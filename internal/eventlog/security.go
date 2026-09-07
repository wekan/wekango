package eventlog

import (
	"context"
	"math"
	"sync"
	"time"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SecurityContext carries identity resolved by the transport, never a user ID
// taken from the request body. HTTP and future DDP adapters supply this context.
type SecurityContext struct {
	UserID   string
	IP       string
	Location any
}

func securityOr(values ...any) any {
	for _, value := range values {
		if summaryTruthy(value) {
			return value
		}
	}
	return ""
}

func securitySlice(value any, limit int) string {
	units := utf16.Encode([]rune(summaryString(value)))
	if len(units) > limit {
		units = units[:limit]
	}
	return string(utf16.Decode(units))
}

// SecurityDocument ports securityLog.record's catalog merge and stored shape.
// Request/DDP identity discovery happens in the transport adapter. Explicit
// event fields win over the catalog, including falsey values and nulls.
func SecurityDocument(evt bson.M, actor SecurityContext, now time.Time) bson.M {
	m := bson.M{}
	if summaryTruthy(evt["key"]) {
		m = SecurityCategory(summaryString(evt["key"]))
	}
	for key, value := range evt {
		m[key] = value
	}
	if !summaryTruthy(m["userId"]) && actor.UserID != "" {
		m["userId"] = actor.UserID
	}
	if !summaryTruthy(m["ip"]) && actor.IP != "" {
		m["ip"] = actor.IP
	}
	if !summaryTruthy(m["location"]) && summaryTruthy(actor.Location) {
		m["location"] = actor.Location
	}
	doc := bson.M{"stream": "security", "at": now,
		"severity": securityOr(m["severity"], "info"), "category": securityOr(m["category"], "unknown"),
		"bleed": securityOr(m["bleed"], "Generic"), "action": securityOr(m["action"], "detected"),
		"source": securityOr(m["source"]), "cwe": securityOr(m["cwe"]), "detail": SanitizeSecurityDetail(m["detail"])}
	if id := securityOr(m["userId"], m["userid"]); summaryTruthy(id) {
		doc["userId"] = summaryString(id)
	}
	if summaryTruthy(m["username"]) {
		doc["username"] = securitySlice(m["username"], 100)
	}
	if summaryTruthy(m["ip"]) {
		doc["ip"] = securitySlice(m["ip"], 64)
	}
	if summaryTruthy(m["location"]) {
		doc["location"] = securityClone(m["location"])
	}
	count := summaryNumber(m["count"])
	switch value := m["count"].(type) {
	case string:
		count = jsStringNumber(value)
	case bson.A, []any:
		count = jsStringNumber(summaryString(value))
	}
	if math.IsNaN(count) || math.IsInf(count, 0) || count <= 0 {
		count = 1
	} else {
		count = math.Floor(count)
	}
	doc["count"] = count
	return doc
}

// ShouldBlockSecurityAccount matches blockOnSecurityEvent: only an identified
// account, an actual refusal, and high/critical severity qualify.
func ShouldBlockSecurityAccount(evt bson.M) bool {
	if !summaryTruthy(evt["userId"]) || evt["action"] != "blocked" {
		return false
	}
	severity := evt["severity"]
	if !summaryTruthy(severity) && summaryTruthy(evt["key"]) {
		severity = SecurityCategory(summaryString(evt["key"]))["severity"]
	}
	return severity == "high" || severity == "critical"
}

func SecurityBlockReason(evt bson.M) string {
	cat := bson.M{}
	if summaryTruthy(evt["key"]) {
		cat = SecurityCategory(summaryString(evt["key"]))
	}
	name := securityOr(evt["bleed"], cat["bleed"], evt["category"], cat["category"], "a security guard")
	where := ""
	if summaryTruthy(evt["source"]) {
		where = " at " + summaryString(evt["source"])
	}
	return "Attempted " + summaryString(name) + where + " while logged in"
}

// BlockSecurityAccount changes only the named account using Meteor's existing
// fields. The address is evidence, never a selector or an address-wide ban.
func BlockSecurityAccount(ctx context.Context, db *mongo.Database, evt bson.M, now time.Time) (bool, error) {
	if !ShouldBlockSecurityAccount(evt) {
		return false, nil
	}
	_, err := db.Collection("users").UpdateOne(ctx, bson.M{"_id": evt["userId"]}, bson.M{"$set": bson.M{
		"loginDisabled": true, "services.securityBlock": bson.M{"at": now, "reason": SecurityBlockReason(evt),
			"bleed": securityOr(evt["bleed"]), "source": securityOr(evt["source"]), "ip": securityOr(evt["ip"])},
	}})
	return err == nil, err
}

// SecurityReporter starts independent best-effort effects after a guard has
// refused the operation. Neither reporting failure nor blocking failure changes
// that refusal. Close joins admitted work before the application closes storage.
type SecurityReporter struct {
	mu      sync.Mutex
	closed  bool
	pending sync.WaitGroup
	fold    func(context.Context, bson.M) error
	block   func(context.Context, bson.M) error
	now     func() time.Time
}

func NewSecurityReporter(db *mongo.Database) *SecurityReporter {
	w := NewWriter(db)
	return &SecurityReporter{fold: w.Fold, now: time.Now, block: func(ctx context.Context, evt bson.M) error {
		_, err := BlockSecurityAccount(ctx, db, evt, time.Now())
		return err
	}}
}

func (r *SecurityReporter) Record(evt bson.M, actor SecurityContext) {
	defer func() { _ = recover() }()
	if r == nil {
		return
	}
	doc := securityClone(SecurityDocument(evt, actor, r.now())).(bson.M)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.pending.Add(2)
	r.mu.Unlock()
	for _, effect := range []func(context.Context, bson.M) error{r.fold, r.block} {
		go func(effect func(context.Context, bson.M) error) {
			defer r.pending.Done()
			defer func() { _ = recover() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = effect(ctx, doc)
		}(effect)
	}
}

func (r *SecurityReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.pending.Wait()
}

func securityClone(value any) any {
	switch value := value.(type) {
	case bson.M:
		out := bson.M{}
		for k, v := range value {
			out[k] = securityClone(v)
		}
		return out
	case map[string]any:
		out := bson.M{}
		for k, v := range value {
			out[k] = securityClone(v)
		}
		return out
	case bson.A:
		out := make(bson.A, len(value))
		for i, v := range value {
			out[i] = securityClone(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = securityClone(v)
		}
		return out
	case bson.D:
		out := make(bson.D, len(value))
		for i, v := range value {
			out[i] = bson.E{Key: v.Key, Value: securityClone(v.Value)}
		}
		return out
	default:
		return value
	}
}
