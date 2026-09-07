package eventlog

// Pure port of models/lib/apiUsage.js. Counts are informational and deliberately
// buffered: a process crash before a flush can lose the current usage window.
import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const MaxTracked = 500
const Unmatched = "(no route)"

func APIName(method, routePath string) string {
	if method == "" {
		method = "GET"
	}
	if routePath == "" {
		routePath = Unmatched
	}
	return strings.ToUpper(method) + " " + routePath
}
func IsAPIRequest(url string) bool {
	path := strings.SplitN(url, "?", 2)[0]
	return path == "/api" || strings.HasPrefix(path, "/api/")
}
func UsageKey(name, userID string) string { return userID + "\x00" + name }

type UsageAccumulator struct {
	mu         sync.Mutex
	maxTracked int
	pending    map[string]bson.M
	order      []string
	overflow   int64
}

func NewUsageAccumulator(maxTracked ...int) *UsageAccumulator {
	limit := MaxTracked
	if len(maxTracked) > 0 {
		limit = maxTracked[0]
	}
	return &UsageAccumulator{maxTracked: limit, pending: map[string]bson.M{}}
}

// Add returns whether the call has its own tracked row. Overflow is counted even
// when false is returned. firstAt is the first insertion, not the earliest date.
func (a *UsageAccumulator) Add(event bson.M) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	name := event["name"]
	if !usageTruthy(name) {
		return false
	}
	userID := event["userId"]
	if !usageTruthy(userID) {
		userID = ""
	}
	key := UsageKey(usageString(name), usageString(userID))
	if row := a.pending[key]; row != nil {
		row["count"] = row["count"].(int64) + 1
		newAt, newPresent := event["at"]
		oldAt, oldPresent := row["at"]
		if newPresent && oldPresent && usageLater(newAt, oldAt) {
			row["at"] = event["at"]
			if usageTruthy(event["ip"]) {
				row["ip"] = event["ip"]
			}
			if usageTruthy(event["location"]) {
				row["location"] = event["location"]
			}
		}
		return true
	}
	if len(a.pending) >= a.maxTracked {
		a.overflow++
		return false
	}
	ip := event["ip"]
	if !usageTruthy(ip) {
		ip = ""
	}
	location := event["location"]
	if !usageTruthy(location) {
		location = nil
	}
	row := bson.M{"name": name, "userId": userID, "ip": ip, "location": location, "count": int64(1)}
	if at, ok := event["at"]; ok {
		row["firstAt"] = at
		row["at"] = at
	}
	a.pending[key] = row
	a.order = append(a.order, key)
	return true
}
func (a *UsageAccumulator) Size() int       { a.mu.Lock(); defer a.mu.Unlock(); return len(a.pending) }
func (a *UsageAccumulator) Drain() []bson.M { return a.DrainAt(time.Now()) }

// DrainAt injects the clock for the overflow row; ordinary rows retain their
// request times. Insertion order matches JavaScript Map iteration order.
func (a *UsageAccumulator) DrainAt(now time.Time) []bson.M {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows := make([]bson.M, 0, len(a.order)+1)
	for _, key := range a.order {
		rows = append(rows, a.pending[key])
	}
	if a.overflow > 0 {
		row := bson.M{"name": fmt.Sprintf("%s (over %d tracked)", Unmatched, a.maxTracked), "userId": "", "ip": "", "location": nil, "count": a.overflow, "at": now}
		if len(rows) > 0 {
			if first, ok := rows[0]["firstAt"]; ok {
				row["firstAt"] = first
			}
		} else {
			row["firstAt"] = now
		}
		rows = append(rows, row)
	}
	a.pending = map[string]bson.M{}
	a.order = nil
	a.overflow = 0
	return rows
}
func usageTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0 && !math.IsNaN(v)
	}
	return true
}
func usageString(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bson.M, bson.D, map[string]any:
		return "[object Object]"
	case bson.A:
		return usageString([]any(v))
	case []string:
		parts := make([]any, len(v))
		for i, s := range v {
			parts[i] = s
		}
		return usageString(parts)
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			if x != nil {
				parts[i] = usageString(x)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}
func usageNumber(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case time.Time:
		return float64(v.UnixMilli())
	case bson.DateTime:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		s := geoTrim(v)
		if s == "" {
			return 0
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	}
	return math.NaN()
}
func usageLater(a, b any) bool {
	if sa, ok := a.(string); ok {
		if sb, ok := b.(string); ok {
			return sa > sb
		}
	}
	if a == nil || b == nil {
		return usageNumber(a) > usageNumber(b)
	}
	return usageNumber(a) > usageNumber(b)
}

// UsageFlushInterval matches Number(WEKAN_API_USAGE_FLUSH_MS) || 10000 followed by
// Node's timer normalization. Fractions truncate and out-of-range timers become
// one millisecond; blank/zero/NaN use the ten-second application default.
func UsageFlushInterval(value string) time.Duration {
	s := geoTrim(value)
	var milliseconds float64
	var err error
	if s != "" {
		if len(s) > 2 && (strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") || strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B") || strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O")) {
			base := 16
			switch s[1] {
			case 'b', 'B':
				base = 2
			case 'o', 'O':
				base = 8
			}
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "_+-") {
				milliseconds = math.NaN()
			} else {
				milliseconds, _ = new(big.Float).SetInt(n).Float64()
			}
		} else if s != "Infinity" && s != "+Infinity" && s != "-Infinity" && geoFloatPrefix.FindString(s) != s {
			milliseconds = math.NaN()
		} else {
			milliseconds, err = strconv.ParseFloat(s, 64)
			if err != nil && !math.IsInf(milliseconds, 0) {
				milliseconds = math.NaN()
			}
		}
	}
	if milliseconds == 0 || math.IsNaN(milliseconds) {
		milliseconds = 10000
	}
	if milliseconds < 1 || milliseconds > 2147483647 || math.IsInf(milliseconds, 0) {
		milliseconds = 1
	}
	return time.Duration(int64(milliseconds)) * time.Millisecond
}
