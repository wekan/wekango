package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUsageNamingAndAPIBoundary(t *testing.T) {
	for _, tc := range []struct{ method, route, want string }{{"", "", "GET (no route)"}, {"post", "/api/boards/:boardId/lists", "POST /api/boards/:boardId/lists"}, {"get", "", "GET (no route)"}} {
		if got := APIName(tc.method, tc.route); got != tc.want {
			t.Fatal(got)
		}
	}
	for _, s := range []string{"/api", "/api?x=1", "/api/boards", "/api/?x=1"} {
		if !IsAPIRequest(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "/apiary", "/users/login", "/API", "/health", "https://host/api", "/api#x"} {
		if IsAPIRequest(s) {
			t.Fatal(s)
		}
	}
	if UsageKey("endpoint", "account") != "account\x00endpoint" {
		t.Fatal("usage identity changed")
	}
}
func TestUsageFirstAndLatestSemantics(t *testing.T) {
	a := NewUsageAccumulator(2)
	if a.Add(bson.M{}) {
		t.Fatal("accepted unnamed call")
	}
	events := []bson.M{{"name": "GET /api/a", "userId": "alice", "at": int64(30), "ip": "first", "location": bson.M{"city": "First"}}, {"name": "GET /api/a", "userId": "alice", "at": int64(10), "ip": "older", "location": bson.M{"city": "Older"}}, {"name": "GET /api/a", "userId": "alice", "at": int64(30), "ip": "equal"}, {"name": "GET /api/a", "userId": "alice", "at": int64(40), "ip": "", "location": nil}, {"name": "GET /api/a", "userId": "alice", "at": int64(50), "ip": "latest", "location": bson.M{}}, {"name": "GET /api/a", "userId": "bob", "at": int64(5)}}
	for _, e := range events {
		if !a.Add(e) {
			t.Fatal("unexpected overflow")
		}
	}
	if a.Add(bson.M{"name": "POST /api/b", "at": int64(99)}) {
		t.Fatal("cap ignored")
	}
	if a.Size() != 2 {
		t.Fatal(a.Size())
	}
	rows := a.DrainAt(time.UnixMilli(100))
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	r := rows[0]
	if r["count"] != int64(5) || r["firstAt"] != int64(30) || r["at"] != int64(50) || r["ip"] != "latest" || len(r["location"].(bson.M)) != 0 {
		t.Fatalf("first/latest changed: %#v", r)
	}
	if rows[1]["userId"] != "bob" || rows[2]["firstAt"] != int64(30) || rows[2]["count"] != int64(1) || rows[2]["name"] != "(no route) (over 2 tracked)" {
		t.Fatal(rows)
	}
	if a.Size() != 0 || len(a.Drain()) != 0 {
		t.Fatal("drain did not reset")
	}
	if !a.Add(events[0]) {
		t.Fatal("did not reset overflow capacity")
	}
}
func TestUsageDefaultCapAndZeroCap(t *testing.T) {
	a := NewUsageAccumulator()
	for i := 0; i < 510; i++ {
		got := a.Add(bson.M{"name": fmt.Sprint(i), "at": int64(i)})
		if got != (i < 500) {
			t.Fatal(i)
		}
	}
	rows := a.DrainAt(time.UnixMilli(1000))
	if len(rows) != 501 || rows[500]["count"] != int64(10) {
		t.Fatal("overflow dropped")
	}
	a = NewUsageAccumulator(0)
	a.Add(bson.M{"name": "x"})
	rows = a.DrainAt(time.UnixMilli(1000))
	if len(rows) != 1 || rows[0]["firstAt"] != rows[0]["at"] {
		t.Fatal(rows)
	}
}
func TestUsageMissingDateStaysMissing(t *testing.T) {
	a := NewUsageAccumulator()
	a.Add(bson.M{"name": "x", "ip": "first"})
	a.Add(bson.M{"name": "x", "at": 100, "ip": "second"})
	r := a.Drain()[0]
	if _, ok := r["at"]; ok {
		t.Fatal("undefined comparison was treated as zero")
	}
	if r["ip"] != "first" {
		t.Fatal(r)
	}
}
func TestUsageConcurrentCalls(t *testing.T) {
	a := NewUsageAccumulator()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				a.Add(bson.M{"name": "GET /api/a", "at": j})
			}
		}()
	}
	wg.Wait()
	if rows := a.Drain(); len(rows) != 1 || rows[0]["count"] != int64(2000) {
		t.Fatal(rows)
	}
}

func eventlogSource(t *testing.T, module, script string, input any) []byte {
	t.Helper()
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("requires actual WeKan source")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--no-warnings", "-e", script, filepath.Join(root, "models/lib", module))
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source differential: %v %s", err, out)
	}
	return out
}
func eventlogJSONEqual(t *testing.T, want []byte, got any) {
	t.Helper()
	actual, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err = json.Unmarshal(actual, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %s\nwant %s", actual, want)
	}
}
func TestUsageSourceDifferential(t *testing.T) {
	type fixture struct {
		Limit  int      `json:"limit"`
		Events []bson.M `json:"events"`
	}
	fixtures := []fixture{{0, []bson.M{{"name": "a", "at": 30}}}, {2, []bson.M{{"name": "a", "userId": "u", "at": 30, "ip": "old"}, {"name": "a", "userId": "u", "at": 20, "ip": "older"}, {"name": "a", "userId": "u", "at": 40}, {"name": "a", "userId": "v", "at": 4}, {"name": "b", "at": 9}, {"name": "a", "userId": "u", "at": 40, "ip": "equal"}, {"name": "a", "userId": "u", "at": 50, "location": bson.M{}}, {"name": "c", "at": 80}}}, {1, []bson.M{{}, {"name": "a"}, {"name": "a", "at": 10, "ip": "no"}}}}
	many := fixture{Limit: 500}
	for i := 0; i < 505; i++ {
		many.Events = append(many.Events, bson.M{"name": fmt.Sprint(i), "at": i})
	}
	fixtures = append(fixtures, many)
	want := eventlogSource(t, "apiUsage.js", `const fs=require('fs');const RealDate=Date;global.Date=class extends RealDate{constructor(...args){super(...(args.length?args:[1000]))}};const {UsageAccumulator}=require(process.argv[1]);const fixtures=JSON.parse(fs.readFileSync(0,'utf8'));process.stdout.write(JSON.stringify(fixtures.map(f=>{const a=new UsageAccumulator({maxTracked:f.limit});const added=f.events.map(e=>a.add(e));const size=a.size;const rows=a.drain().map(r=>({...r,...(r.at instanceof Date?{at:r.at.getTime()}:{}),...(r.firstAt instanceof Date?{firstAt:r.firstAt.getTime()}:{} )}));return {added,size,rows,empty:a.drain()}})));`, fixtures)
	got := []any{}
	for _, f := range fixtures {
		a := NewUsageAccumulator(f.Limit)
		added := []bool{}
		for _, e := range f.Events {
			added = append(added, a.Add(e))
		}
		size := a.Size()
		rows := a.DrainAt(time.UnixMilli(1000))
		for _, r := range rows {
			for _, key := range []string{"at", "firstAt"} {
				if stamp, ok := r[key].(time.Time); ok {
					r[key] = stamp.UnixMilli()
				}
			}
		}
		got = append(got, map[string]any{"added": added, "size": size, "rows": rows, "empty": a.Drain()})
	}
	eventlogJSONEqual(t, want, got)
}

func TestUsageFlushInterval(t *testing.T) {
	for value, want := range map[string]time.Duration{"": 10000, "0": 10000, "bad": 10000, " 2.9 ": 2, "2e3": 2000, "0x20": 32, "0b11": 3, "0o10": 8, "-2": 1, "Infinity": 1, "Inf": 10000, "1_000": 10000, "2147483648": 1, "0.4": 1, "0xFFFFFFFFFFFFFFFFFFFFFFFF": 1, "0x+1": 10000} {
		if got := UsageFlushInterval(value); got != want*time.Millisecond {
			t.Fatalf("%q: %v want %v ms", value, got, want)
		}
	}
}
func TestUsageFlushSourceDifferential(t *testing.T) {
	values := []string{"", "0", "NaN", "bad", "1junk", " 2.9 ", "2e3", "0x20", "0b11", "0o10", "-2", "Infinity", "+Infinity", "-Infinity", "Inf", "1_000", "2147483648", "2147483647.5", "0.4", "0xFFFFFFFFFFFFFFFFFFFFFFFF", "0x+1", "-0x1", "\ufeff2", "\u00852", "1e-999", "1e999", "1e+"}
	// Evaluate the actual source's FLUSH_MS expression; use a real Node timer to
	// observe conversion, and cancel it immediately without waiting.
	want := eventlogSource(t, "../../server/lib/apiUsageLog.js", `const fs=require('fs');const src=fs.readFileSync(process.argv[1],'utf8');const expression=src.match(/const FLUSH_MS = ([^;]+);/)[1];const values=JSON.parse(fs.readFileSync(0,'utf8'));const out=values.map(v=>{process.env.WEKAN_API_USAGE_FLUSH_MS=v;const n=eval(expression);const timer=setTimeout(()=>{},n);const result=timer._idleNext.msecs;clearTimeout(timer);return result});process.stdout.write(JSON.stringify(out));`, values)
	got := []int64{}
	for _, value := range values {
		got = append(got, int64(UsageFlushInterval(value)/time.Millisecond))
	}
	eventlogJSONEqual(t, want, got)
}
