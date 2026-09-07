package eventlog

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSummaryModifiersPreserveIdentityAndLatest(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	evt := bson.M{"stream": "security", "source": "guard", "apiUserId": "account", "severity": "medium", "username": "User", "ip": "::ffff:203.0.113.9", "ipv6": "older", "detail": "", "message": nil, "kind": false}
	identity := SummaryIdentity(evt)
	if identity["kind"] != false || !reflect.DeepEqual(identity["cwe"], bson.M{"$exists": false}) || identity["apiUserId"] != "account" {
		t.Fatal(identity)
	}
	update := SummaryUpdate(evt, now, 2.5)
	set := update["$set"].(bson.M)
	if set["ip"] != "203.0.113.9" || set["ipv4"] != "203.0.113.9" || set["username"] != "User" || set["at"] != now {
		t.Fatal(update)
	}
	for _, field := range []string{"ipv6", "detail", "message"} {
		if _, ok := set[field]; ok {
			t.Fatal("latest empty/opposite family inserted", update)
		}
	}
	if _, ok := update["$unset"]; ok {
		t.Fatal("source does not clear old family field via $unset")
	}
	if update["$inc"].(bson.M)["count"] != 2.5 || update["$setOnInsert"].(bson.M)["firstAt"] != now {
		t.Fatal(update)
	}
}
func TestSummaryTypedNilLocationIsAbsent(t *testing.T) {
	var location bson.M
	update := SummaryUpdate(bson.M{"location": location}, time.Now(), 1)
	if _, exists := update["$set"].(bson.M)["location"]; exists {
		t.Fatal("typed nil location should behave like JavaScript null")
	}
}

func TestSummaryCompoundFieldsDoNotPanic(t *testing.T) {
	evt := bson.M{"stream": bson.M{"nested": "identity"}, "kind": bson.A{1, 2}, "message": bson.M{"nested": "latest"}}
	identity := SummaryIdentity(evt)
	update := SummaryUpdate(evt, time.Now(), 1)
	if !reflect.DeepEqual(identity["stream"], evt["stream"]) || !reflect.DeepEqual(identity["kind"], evt["kind"]) || !reflect.DeepEqual(update["$set"].(bson.M)["message"], evt["message"]) {
		t.Fatal("compound field changed")
	}
}

func TestSummaryECMAAddressWhitespace(t *testing.T) {
	for _, tc := range []struct{ input, v4, v6 string }{
		{"\ufeff203.0.113.9\ufeff", "203.0.113.9", ""},
		{"\u0085203.0.113.9\u0085", "", ""},
		{"\ufeff2001:db8::1\ufeff", "", "2001:db8::1"},
		{"\u00852001:db8::1\u0085", "", ""},
	} {
		v4, v6 := ClassifyAddress(tc.input)
		if v4 != tc.v4 || v6 != tc.v6 {
			t.Fatalf("classify %q=(%q,%q)", tc.input, v4, v6)
		}
	}
}

func TestSummaryActorCapKnownAndOverflow(t *testing.T) {
	now := time.Now()
	evt := bson.M{"username": "target", "ip": "203.0.113.9"}
	userKey := ActorKeyFor(bson.M{"kind": "user", "value": "target"})
	known := map[string]bool{userKey: true}
	for i := 0; i < 49; i++ {
		known[strings.Repeat("x", i+1)] = true
	}
	update := ActorUpdate(evt, now, 3, known)
	inc := update["$inc"].(bson.M)
	set := update["$set"].(bson.M)
	if inc["actors."+userKey+".count"] != float64(3) || inc["actorsOverflow"] != float64(3) || len(set) != 1 {
		t.Fatal(update)
	}
	delete(known, userKey)
	update = ActorUpdate(evt, now, 3, known)
	inc = update["$inc"].(bson.M)
	if inc["actorsOverflow"] != float64(3) || len(update["$set"].(bson.M)) != 3 {
		t.Fatal("last slot goes to user, IP overflows", update)
	}
}
func TestSummaryAddressClassificationPreservesSource(t *testing.T) {
	for _, tc := range []struct{ ip, v4, v6 string }{{"::ffff:203.0.113.9", "203.0.113.9", ""}, {"::203.0.113.9", "203.0.113.9", ""}, {"203.000.113.9", "", ""}, {"::ffff:203.000.113.9", "", "::ffff:203.000.113.9"}, {"FE80::1%eth0", "", "FE80::1%eth0"}, {"::::", "", "::::"}, {"unknown", "", ""}, {" 203.0.113.9 ", "203.0.113.9", ""}} {
		v4, v6 := ClassifyAddress(tc.ip)
		if v4 != tc.v4 || v6 != tc.v6 {
			t.Fatalf("%q got %q %q", tc.ip, v4, v6)
		}
	}
}
func TestSummaryFoldFirstLatestAndLegacyActors(t *testing.T) {
	old, newer := bson.DateTime(1000), bson.DateTime(3000)
	docs := []bson.M{{"stream": "security", "at": newer, "username": "latest", "count": 2.5, "detail": "latest text"}, {"stream": "security", "at": old, "username": "old", "count": 2}, {"stream": "security", "at": newer, "username": nil, "count": 0}, {"stream": "security", "at": bson.DateTime(2000), "actors": bson.D{{Key: "stored", Value: bson.M{"kind": "user", "value": "legacy", "count": float64(5), "at": old}}}, "actorsOverflow": float64(7)}}
	out := FoldEvents(docs)
	if len(out) != 1 {
		t.Fatal(out)
	}
	row := out[0]
	if row["count"] != 6.5 || row["firstAt"] != old || row["at"] != newer || row["username"] != nil || row["detail"] != "latest text" || row["actorsOverflow"] != float64(7) {
		t.Fatal(row)
	}
	if row["actors"].(bson.M)["stored"].(bson.M)["count"] != float64(5) {
		t.Fatal(row)
	}
	if docs[3]["actors"].(bson.D)[0].Value.(bson.M)["count"] != float64(5) {
		t.Fatal("input legacy tally mutated")
	}
}

func summaryTestJSON(v any) any {
	switch n := v.(type) {
	case time.Time:
		return n.UTC().Format("2006-01-02T15:04:05.000Z")
	case bson.DateTime:
		return n.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	case bson.M:
		m := map[string]any{}
		for k, v := range n {
			m[k] = summaryTestJSON(v)
		}
		return m
	case bson.D:
		m := map[string]any{}
		for _, e := range n {
			m[e.Key] = summaryTestJSON(e.Value)
		}
		return m
	case bson.A:
		a := []any{}
		for _, v := range n {
			a = append(a, summaryTestJSON(v))
		}
		return a
	case []bson.M:
		a := []any{}
		for _, v := range n {
			a = append(a, summaryTestJSON(v))
		}
		return a
	case map[string]any:
		return summaryTestJSON(bson.M(n))
	default:
		return v
	}
}

// TestSummarySourceDifferential invokes the actual dependency-free source,
// without a reimplemented reference or a database adapter. It is opt-in only
// when that independent checkout is not available to normal Go package users.
func TestSummarySourceDifferential(t *testing.T) {
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("set WEKAN_SOURCE_ROOT for eventLogSummary.js source differential")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	binary, err := exec.LookPath(node)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 10, 11, 12, 0, time.UTC)
	events := []bson.M{{}, {"stream": "api", "api": "GET /api/boards", "apiUserId": "u1", "username": "Ålice", "ip": "::ffff:203.0.113.9", "ipv6": "old", "detail": "retained"}, {"stream": nil, "kind": false, "username": false, "ip": "unknown", "detail": ""}, {"stream": "security", "username": "some.user", "ip": "fe80::1%eth0", "location": bson.M{"country": "FI"}}, {"stream": "security", "username": int32(42), "ip": "::::"}}
	events = append(events,
		bson.M{"stream": "security", "username": "bom-v4", "ip": "\ufeff203.0.113.9\ufeff"},
		bson.M{"stream": "security", "username": "nel-v4", "ip": "\u0085203.0.113.9\u0085"},
		bson.M{"stream": "security", "username": "bom-v6", "ip": "\ufeff2001:db8::1\ufeff"},
		bson.M{"stream": "security", "username": "nel-v6", "ip": "\u00852001:db8::1\u0085"},
		bson.M{"stream": bson.M{"nested": "identity"}, "kind": bson.A{"compound", 1}, "location": bson.M{"country": "FI"}, "message": bson.M{"nested": "message"}},
	)

	docs := []bson.M{{"stream": "security", "at": bson.DateTime(1000), "count": float64(2), "username": "first", "ip": "::203.0.113.9"}, {"stream": "security", "at": bson.DateTime(3000), "count": 2.5, "username": "last", "ip": "203.0.113.9", "detail": "last"}, {"stream": "security", "at": bson.DateTime(2000), "count": "4", "username": "middle"}, {"stream": "security", "at": bson.DateTime(3000), "username": nil}, {"stream": nil, "at": bson.DateTime(0)}, {"at": bson.DateTime(0)}}
	for i := 0; i < 60; i++ {
		docs = append(docs, bson.M{"stream": "capped", "at": bson.DateTime(i * 1000), "username": strings.Repeat("actor", i+1)})
	}
	docs = append(docs, bson.M{"stream": "security", "at": bson.DateTime(2000), "actors": bson.M{"legacy": bson.M{"kind": "user", "value": "Existing", "count": float64(8), "at": bson.DateTime(1000)}}, "actorsOverflow": float64(4)})
	actorMap := bson.M{}
	for i, value := range []string{"Z", "a", "A", "ä", "Å", "á", "_user", "user.name", "👩"} {
		actorMap[strings.Repeat("k", i+1)] = bson.M{"kind": "user", "value": value, "count": float64(2)}
	}
	known := map[string]bool{}
	for i := 0; i < 49; i++ {
		known[strings.Repeat("k", i+1)] = true
	}
	type packet struct {
		Event   any             `json:"event"`
		Now     any             `json:"now"`
		Times   float64         `json:"times"`
		Known   map[string]bool `json:"known"`
		Docs    any             `json:"docs"`
		Summary any             `json:"summary"`
	}
	const script = `const fs=require('fs'),path=require('path');const s=require(path.join(process.argv[1],'models/lib/eventLogSummary.js'));const input=JSON.parse(fs.readFileSync(0,'utf8'),(k,v)=>typeof v==='string'&&['at','firstAt','now'].includes(k)&&/^\d{4}-/.test(v)?new Date(v):v);const e=input.event,n=input.now,t=input.times;console.log(JSON.stringify({identity:s.summaryIdentity(e),update:s.summaryUpdate(e,n,t),actors:s.actorsOf(e),actorUpdate:s.actorUpdate(e,n,t,new Set(Object.keys(input.known))),actorList:s.actorList(input.summary),fold:s.foldEvents(input.docs)}));`
	for i, evt := range events {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			input, err := json.Marshal(packet{summaryTestJSON(evt), summaryTestJSON(now), 2.5, known, summaryTestJSON(docs), summaryTestJSON(bson.M{"actors": actorMap})})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-e", script, root)
			cmd.Stdin = bytes.NewReader(input)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node: %v %s", err, raw)
			}
			want := any(nil)
			if err = json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			gotRaw, err := json.Marshal(summaryTestJSON(bson.M{"identity": SummaryIdentity(evt), "update": SummaryUpdate(evt, now, 2.5), "actors": ActorsOf(evt), "actorUpdate": ActorUpdate(evt, now, 2.5, known), "actorList": ActorList(bson.M{"actors": actorMap}), "fold": FoldEvents(docs)}))
			if err != nil {
				t.Fatal(err)
			}
			var got any
			if err = json.Unmarshal(gotRaw, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				a, _ := json.MarshalIndent(want, "", " ")
				b, _ := json.MarshalIndent(got, "", " ")
				t.Fatalf("source:\n%s\nGo:\n%s", a, b)
			}
		})
	}
}
