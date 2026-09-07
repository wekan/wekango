package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const MaxActors = 50

var IdentityFields = []string{"stream", "bleed", "category", "action", "source", "severity", "cwe", "type", "db", "kind", "api", "apiUserId"}
var LatestFields = []string{"userId", "username", "ip", "ipv4", "ipv6", "location", "detail", "message"}

func summaryNull(v any) bool {
	switch n := v.(type) {
	case nil:
		return true
	case bson.M:
		return n == nil
	case bson.D:
		return n == nil
	case bson.A:
		return n == nil
	case map[string]any:
		return n == nil
	case []any:
		return n == nil
	}
	return false
}

func summaryTruthy(v any) bool {
	if summaryNull(v) {
		return false
	}
	switch n := v.(type) {
	case nil:
		return false
	case bool:
		return n
	case string:
		return n != ""
	case float64:
		return n != 0 && !math.IsNaN(n)
	case int:
		return n != 0
	case int32:
		return n != 0
	case int64:
		return n != 0
	default:
		return true
	}
}
func summaryNumber(v any) float64 {
	switch n := v.(type) {
	case nil:
		return 0
	case bool:
		if n {
			return 1
		}
		return 0
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case bson.DateTime:
		return float64(n)
	case time.Time:
		return float64(n.UnixMilli())
	case string:
		if geoTrim(n) == "" {
			return 0
		}
		f, e := strconv.ParseFloat(geoTrim(n), 64)
		if e == nil {
			return f
		}
	}
	return math.NaN()
}
func summaryString(v any) string {
	if summaryNull(v) {
		return "null"
	}
	switch n := v.(type) {
	case nil:
		return "null"
	case string:
		return n
	case bson.M, bson.D, map[string]any:
		return "[object Object]"
	case bson.A:
		parts := make([]string, len(n))
		for i, v := range n {
			if v != nil {
				parts[i] = summaryString(v)
			}
		}
		return strings.Join(parts, ",")
	case []any:
		return summaryString(bson.A(n))
	case bson.ObjectID:
		return n.Hex()
	case float64:
		if n == 0 {
			return "0"
		}
		if math.IsInf(n, 1) {
			return "Infinity"
		}
		if math.IsInf(n, -1) {
			return "-Infinity"
		}
		if math.Abs(n) >= 1e21 || math.Abs(n) < 1e-6 {
			value := strconv.FormatFloat(n, 'e', -1, 64)
			if parts := strings.Split(value, "e"); len(parts) == 2 {
				sign := ""
				exponent := parts[1]
				if strings.HasPrefix(exponent, "+") || strings.HasPrefix(exponent, "-") {
					sign = exponent[:1]
					exponent = exponent[1:]
				}
				exponent = strings.TrimLeft(exponent, "0")
				return parts[0] + "e" + sign + exponent
			}
			return value
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
func summaryPresent(v any) bool {
	if summaryNull(v) {
		return false
	}
	if value, ok := v.(string); ok {
		return value != ""
	}
	return true
}
func summaryMap(v any) bson.M {
	switch d := v.(type) {
	case bson.M:
		return d
	case bson.D:
		m := bson.M{}
		for _, e := range d {
			m[e.Key] = e.Value
		}
		return m
	case map[string]any:
		return bson.M(d)
	default:
		return nil
	}
}
func summaryEntries(v any) bson.D {
	switch d := v.(type) {
	case bson.D:
		return d
	case bson.M:
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := bson.D{}
		for _, k := range keys {
			out = append(out, bson.E{Key: k, Value: d[k]})
		}
		return out
	case map[string]any:
		return summaryEntries(bson.M(d))
	case bson.A:
		out := bson.D{}
		for i, v := range d {
			out = append(out, bson.E{Key: strconv.Itoa(i), Value: v})
		}
		return out
	}
	return nil
}

var summaryMapped = regexp.MustCompile(`(?i)^::(?:ffff:)?((?:\d{1,3}\.){3}\d{1,3})$`)
var summaryIPv4 = regexp.MustCompile(`^(?:\d{1,3}\.){3}\d{1,3}$`)
var summaryIPv6 = regexp.MustCompile(`(?i)^[0-9a-f:.]+$`)

func summaryIsIPv4(v string) bool {
	if !summaryIPv4.MatchString(v) {
		return false
	}
	for _, part := range strings.Split(v, ".") {
		if len(part) > 1 && part[0] == '0' {
			return false
		}
		n, _ := strconv.Atoi(part)
		if n > 255 {
			return false
		}
	}
	return true
}

// ClassifyAddress matches ipAddress.js, including its deliberately permissive
// IPv6 classifier; replacing it with a full IP validator changes report data.
func ClassifyAddress(address any) (ipv4, ipv6 string) {
	v := ""
	if address != nil {
		v = geoTrim(summaryString(address))
	}
	if v == "" || v == "unknown" {
		return
	}
	if match := summaryMapped.FindStringSubmatch(v); match != nil && summaryIsIPv4(match[1]) {
		return match[1], ""
	}
	if summaryIsIPv4(v) {
		return v, ""
	}
	bare := strings.Split(v, "%")[0]
	if strings.Contains(bare, ":") && summaryIPv6.MatchString(bare) {
		return "", v
	}
	return
}
func ActorsOf(evt bson.M) bson.A {
	out := bson.A{}
	if summaryTruthy(evt["username"]) {
		out = append(out, bson.M{"kind": "user", "value": summaryString(evt["username"])})
	}
	if summaryTruthy(evt["ip"]) {
		v4, v6 := ClassifyAddress(evt["ip"])
		if v4 != "" {
			out = append(out, bson.M{"kind": "ip", "value": v4, "family": "ipv4"})
		} else if v6 != "" {
			out = append(out, bson.M{"kind": "ip", "value": v6, "family": "ipv6"})
		}
	}
	return out
}
func ActorKeyFor(actor bson.M) string {
	if !summaryTruthy(actor["value"]) {
		return ""
	}
	kind := "undefined"
	if v, ok := actor["kind"]; ok {
		kind = summaryString(v)
	}
	hash := sha256.Sum256([]byte(kind + ":" + summaryString(actor["value"])))
	return hex.EncodeToString(hash[:8])
}
func ActorUpdate(evt bson.M, now time.Time, times float64, known map[string]bool) bson.M {
	set, inc := bson.M{}, bson.M{}
	room := MaxActors - len(known)
	for _, raw := range ActorsOf(evt) {
		actor := raw.(bson.M)
		key := ActorKeyFor(actor)
		if key == "" {
			continue
		}
		if known[key] {
			set["actors."+key+".at"] = now
			inc["actors."+key+".count"] = times
			continue
		}
		if room <= 0 {
			n, _ := inc["actorsOverflow"].(float64)
			inc["actorsOverflow"] = n + times
			continue
		}
		room--
		set["actors."+key+".at"] = now
		set["actors."+key+".kind"] = actor["kind"]
		set["actors."+key+".value"] = actor["value"]
		if family, ok := actor["family"]; ok {
			set["actors."+key+".family"] = family
		}
		inc["actors."+key+".count"] = times
	}
	return bson.M{"$set": set, "$inc": inc}
}
func SummaryIdentity(evt bson.M) bson.M {
	identity := bson.M{}
	for _, field := range IdentityFields {
		if value, ok := evt[field]; ok && summaryPresent(value) {
			identity[field] = value
		} else {
			identity[field] = bson.M{"$exists": false}
		}
	}
	return identity
}
func SummaryUpdate(evt bson.M, now time.Time, times float64) bson.M {
	set := bson.M{"at": now}
	insert := bson.M{"firstAt": now}
	for _, f := range IdentityFields {
		if v, ok := evt[f]; ok && summaryPresent(v) {
			insert[f] = v
		}
	}
	for _, f := range LatestFields {
		if v, ok := evt[f]; ok && summaryPresent(v) {
			set[f] = v
		}
	}
	if summaryTruthy(evt["ip"]) {
		v4, v6 := ClassifyAddress(evt["ip"])
		if v4 != "" {
			set["ipv4"] = v4
			set["ip"] = v4
			delete(set, "ipv6")
		}
		if v6 != "" {
			set["ipv6"] = v6
			set["ip"] = v6
			delete(set, "ipv4")
		}
	}
	return bson.M{"$set": set, "$setOnInsert": insert, "$inc": bson.M{"count": times}}
}

func ActorList(summary bson.M) bson.A {
	actors := []bson.M{}
	for _, e := range summaryEntries(summary["actors"]) {
		a := bson.M{"key": e.Key}
		for k, v := range summaryMap(e.Value) {
			a[k] = v
		}
		if summaryTruthy(a["value"]) {
			actors = append(actors, a)
		}
	}
	collator := collate.New(language.English)
	sort.SliceStable(actors, func(i, j int) bool {
		a, b := actors[i], actors[j]
		an, bn := summaryNumber(a["count"]), summaryNumber(b["count"])
		if !summaryTruthy(a["count"]) {
			an = 0
		}
		if !summaryTruthy(b["count"]) {
			bn = 0
		}
		delta := bn - an
		if delta != 0 && !math.IsNaN(delta) {
			return delta < 0
		}
		return collator.CompareString(summaryString(a["value"]), summaryString(b["value"])) < 0
	})
	if len(actors) > MaxActors {
		actors = actors[:MaxActors]
	}
	out := make(bson.A, len(actors))
	for i, a := range actors {
		out[i] = a
	}
	return out
}

func summaryDate(v any) float64 {
	if !summaryTruthy(v) {
		return 0
	}
	switch n := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02", time.RFC1123, time.RFC1123Z, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, e := time.Parse(layout, n); e == nil {
				return float64(t.UnixMilli())
			}
		}
		return math.NaN()
	default:
		number := summaryNumber(v)
		if math.Abs(number) > 8.64e15 {
			return math.NaN()
		}
		return math.Trunc(number)
	}
}
func summaryDateValue(ms float64) any {
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return nil
	}
	return bson.DateTime(int64(ms))
}
func summaryNumericCount(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
func summaryCopyMap(m bson.M) bson.M {
	out := bson.M{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func summaryAddActors(summary, doc bson.M, at any, times float64) {
	actors := summary["actors"].(bson.M)
	if raw, ok := doc["actors"]; ok && !summaryNull(raw) {
		switch raw.(type) {
		case bson.M, bson.D, map[string]any, bson.A:
			for _, pair := range summaryEntries(raw) {
				entry := summaryMap(pair.Value)
				existing := summaryMap(actors[pair.Key])
				count := summaryNumber(entry["count"])
				if !summaryTruthy(entry["count"]) {
					count = 0
				}
				if existing != nil {
					existing["count"] = summaryNumber(existing["count"]) + count
					if summaryTruthy(entry["at"]) && (!summaryTruthy(existing["at"]) || summaryDate(entry["at"]) > summaryDate(existing["at"])) {
						existing["at"] = entry["at"]
					}
				} else if len(actors) < MaxActors {
					actors[pair.Key] = summaryCopyMap(entry)
				} else {
					summary["actorsOverflow"] = summaryNumber(summary["actorsOverflow"]) + count
				}
			}
			overflow := summaryNumber(doc["actorsOverflow"])
			if !summaryTruthy(doc["actorsOverflow"]) {
				overflow = 0
			}
			summary["actorsOverflow"] = summaryNumber(summary["actorsOverflow"]) + overflow
			return
		}
	}
	for _, raw := range ActorsOf(doc) {
		actor := raw.(bson.M)
		key := ActorKeyFor(actor)
		if key == "" {
			continue
		}
		existing := summaryMap(actors[key])
		if existing != nil {
			existing["count"] = summaryNumber(existing["count"]) + times
			if summaryDate(at) > summaryDate(existing["at"]) {
				existing["at"] = at
			}
		} else if len(actors) < MaxActors {
			entry := summaryCopyMap(actor)
			entry["count"] = times
			entry["at"] = at
			actors[key] = entry
		} else {
			summary["actorsOverflow"] = summaryNumber(summary["actorsOverflow"]) + times
		}
	}
}

// FoldEvents preserves encounter order and the source's distinction between
// undefined/missing identity fields and explicit null. Stored legacy actor
// tallies are copied and merged instead of being counted as one new actor.
func FoldEvents(docs []bson.M) []bson.M {
	type record struct {
		doc         bson.M
		first, last float64
	}
	rows := []*record{}
	byKey := map[string]*record{}
	for _, doc := range docs {
		parts := make([]string, len(IdentityFields))
		for i, f := range IdentityFields {
			if v, ok := doc[f]; ok {
				parts[i] = summaryString(v)
			}
		}
		key := strings.Join(parts, "\x00")
		at := summaryDate(doc["at"])
		when := summaryDateValue(at)
		times, ok := summaryNumericCount(doc["count"])
		if !ok || !(times > 0) {
			times = 1
		}
		existing := byKey[key]
		if existing == nil {
			out := bson.M{"count": times, "firstAt": when, "at": when, "actors": bson.M{}, "actorsOverflow": float64(0)}
			for _, f := range IdentityFields {
				if v, ok := doc[f]; ok {
					out[f] = v
				}
			}
			for _, f := range LatestFields {
				if v, ok := doc[f]; ok {
					out[f] = v
				}
			}
			summaryAddActors(out, doc, when, times)
			row := &record{out, at, at}
			byKey[key] = row
			rows = append(rows, row)
			continue
		}
		summaryAddActors(existing.doc, doc, when, times)
		existing.doc["count"] = summaryNumber(existing.doc["count"]) + times
		if at < existing.first {
			existing.first = at
			existing.doc["firstAt"] = when
		}
		if at >= existing.last {
			existing.last = at
			existing.doc["at"] = when
			for _, f := range LatestFields {
				if v, ok := doc[f]; ok {
					existing.doc[f] = v
				}
			}
		}
	}
	out := make([]bson.M, len(rows))
	for i, row := range rows {
		out[i] = row.doc
	}
	return out
}
