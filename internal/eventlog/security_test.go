package eventlog

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestSecurityReporterBlocksOnlyNamedAccountAndClonesInput(t *testing.T) {
	ctx, db := writerDB(t)
	tokens := bson.A{bson.M{"hashedToken": "keep-token"}}
	for _, id := range []string{"offender", "neighbor"} {
		_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": id, "username": id, "ip": "192.0.2.4", "profile": bson.M{"fullname": "Keep"}, "services": bson.M{"password": bson.M{"bcrypt": "keep-verifier"}, "resume": bson.M{"loginTokens": tokens}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	location := bson.M{"country": "FI"}
	event := bson.M{"key": "authz.export", "action": "blocked", "source": "export", "detail": "refused", "location": location, "count": 25}
	reporter := NewSecurityReporter(db)
	reporter.Record(event, SecurityContext{UserID: "offender", IP: "192.0.2.4"})
	event["source"] = "mutated"
	location["country"] = "mutated"
	reporter.Close()
	var offender, neighbor bson.M
	if err := db.Collection("users").FindOne(ctx, bson.M{"_id": "offender"}).Decode(&offender); err != nil {
		t.Fatal(err)
	}
	if err := db.Collection("users").FindOne(ctx, bson.M{"_id": "neighbor"}).Decode(&neighbor); err != nil {
		t.Fatal(err)
	}
	if offender["loginDisabled"] != true {
		t.Fatalf("offender not disabled: %#v", offender)
	}
	if _, ok := neighbor["loginDisabled"]; ok {
		t.Fatalf("same-address neighbor changed: %#v", neighbor)
	}
	services := writerMap(offender["services"])
	if writerMap(services["password"])["bcrypt"] != "keep-verifier" || len(writerMap(services["resume"])["loginTokens"].(bson.A)) != 1 || writerMap(writerMap(services["resume"])["loginTokens"].(bson.A)[0])["hashedToken"] != "keep-token" {
		t.Fatalf("existing credentials lost: %#v", services)
	}
	if writerMap(offender["profile"])["fullname"] != "Keep" {
		t.Fatalf("profile changed: %#v", offender)
	}
	block := writerMap(services["securityBlock"])
	if block["reason"] != "Attempted ImpersonateBleed at export while logged in" || block["bleed"] != "ImpersonateBleed" || block["source"] != "export" || block["ip"] != "192.0.2.4" {
		t.Fatalf("block schema: %#v", block)
	}
	if _, ok := block["at"].(bson.DateTime); !ok {
		t.Fatalf("block date: %#v", block)
	}
	row := writerRead(t, ctx, db, bson.M{"stream": "security"})
	if row["source"] != "export" || row["category"] != "authz" || row["severity"] != "high" || row["cwe"] != "CWE-863" || row["userId"] != "offender" || writerMap(row["location"])["country"] != "FI" {
		t.Fatalf("normalized/cloned schema: %#v", row)
	}
	// Source fold counts each record once even if a canary supplied count=25.
	if writerNumber(row["count"]) != 1 {
		t.Fatalf("source fold weight changed: %#v", row)
	}
	for _, key := range []string{"key", "req", "request", "userid"} {
		if _, ok := row[key]; ok {
			t.Errorf("unexpected field %s", key)
		}
	}
}

func TestSecurityReporterMediumUnauthenticatedAndSanitizedDoNotBlock(t *testing.T) {
	ctx, db := writerDB(t)
	for _, id := range []string{"medium", "sanitized"} {
		if _, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": id}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewSecurityReporter(db)
	r.Record(bson.M{"key": "authz.board-list", "action": "blocked", "source": "medium"}, SecurityContext{UserID: "medium", IP: "192.0.2.4"})
	r.Record(bson.M{"key": "authz.export", "action": "sanitized", "source": "sanitized"}, SecurityContext{UserID: "sanitized", IP: "192.0.2.4"})
	r.Record(bson.M{"key": "authz.export", "action": "blocked", "source": "anonymous"}, SecurityContext{IP: "192.0.2.4"})
	r.Close()
	if n, err := db.Collection("users").CountDocuments(ctx, bson.M{"loginDisabled": true}); err != nil || n != 0 {
		t.Fatalf("unexpected blocking %d %v", n, err)
	}
	if n, err := db.Collection(Collection).CountDocuments(ctx, bson.M{}); err != nil || n != 3 {
		t.Fatalf("missing logged events %d %v", n, err)
	}
}

func TestSecurityReporterFoldFailureDoesNotPreventAccountBlock(t *testing.T) {
	ctx, db := writerDB(t)
	if _, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "offender"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection(Collection).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "category", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection(Collection).InsertOne(ctx, bson.M{"_id": "occupied", "category": "authz", "stream": "other"}); err != nil {
		t.Fatal(err)
	}
	r := NewSecurityReporter(db)
	r.Record(bson.M{"key": "authz.export", "action": "blocked"}, SecurityContext{UserID: "offender"})
	r.Close()
	var u bson.M
	if err := db.Collection("users").FindOne(ctx, bson.M{"_id": "offender"}).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u["loginDisabled"] != true {
		t.Fatalf("failed fold prevented block %#v", u)
	}
	if n, err := db.Collection(Collection).CountDocuments(ctx, bson.M{}); err != nil || n != 1 {
		t.Fatalf("failure fixture did not reject fold %d %v", n, err)
	}
}

func TestSecurityReporterBlockFailureDoesNotPreventFold(t *testing.T) {
	ctx, db := writerDB(t)
	// A scalar services field cannot accept the nested services.securityBlock.
	if _, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "malformed", "services": "scalar"}); err != nil {
		t.Fatal(err)
	}
	r := NewSecurityReporter(db)
	r.Record(bson.M{"key": "authz.export", "action": "blocked"}, SecurityContext{UserID: "malformed"})
	r.Close()
	row := writerRead(t, ctx, db, bson.M{"stream": "security"})
	if row["userId"] != "malformed" {
		t.Fatalf("block failure prevented fold %#v", row)
	}
}

func TestSecurityReporterCloseRacesAdmissions(t *testing.T) {
	ctx, db := writerDB(t)
	r := NewSecurityReporter(db)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				r.Record(bson.M{"source": "race"}, SecurityContext{})
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); r.Close() }()
	wg.Wait()
	r.Close()
	before, err := db.Collection(Collection).CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	r.Record(bson.M{"source": "after-close"}, SecurityContext{})
	after, err := db.Collection(Collection).CountDocuments(ctx, bson.M{})
	if err != nil || before != after {
		t.Fatalf("admitted after close %d %d %v", before, after, err)
	}
}

func TestSecurityBlockMissingAccountAndCancelledContext(t *testing.T) {
	ctx, db := writerDB(t)
	event := bson.M{"userId": "missing", "action": "blocked", "severity": "high"}
	ok, err := BlockSecurityAccount(ctx, db, event, time.Now())
	// Meteor returns true after updateAsync even when no account matched.
	if !ok || err != nil {
		t.Fatalf("missing account result %v %v", ok, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := BlockSecurityAccount(cancelled, db, event, time.Now()); err == nil {
		t.Fatal("cancelled write should fail")
	}
}

func TestSecurityHelpersSourceDifferential(t *testing.T) {
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("WEKAN_SOURCE_ROOT is required to compare actual Meteor security logger")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	if _, err := exec.LookPath(node); err != nil {
		t.Skip("Node is unavailable for security logger differential")
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	cases := []bson.M{
		{}, {"key": "authz.export", "action": "blocked", "userId": "a", "source": "api"},
		{"key": "authz.export", "severity": "medium", "bleed": "Explicit", "category": "custom", "action": "blocked", "userid": 42, "count": "3.9"},
		{"key": "unknown", "severity": "", "category": "", "bleed": "", "source": "", "detail": nil, "count": 0.5},
		{"key": "file.name", "userId": "", "userid": "alias", "ip": "", "location": nil, "username": strings.Repeat("x", 101), "count": "0x10"},
		{"key": "authz.export", "userId": "u", "action": "sanitized", "count": -1, "detail": "password=secret"},
		{"key": "authz.export", "action": "blocked", "userId": "explicit", "ip": "203.0.113.1", "location": bson.M{"country": "SE"}},
		{"key": "authz.export", "severity": bson.M{"bad": true}, "action": bson.A{"blocked"}, "userId": "u"},
	}
	for _, count := range []any{"0b11", "0o12", "+0x10", "-0x10", "Inf", "Infinity", bson.A{"2.8"}, bson.A{}, bson.A{1, 2}, 0.5, "\ufeff 4 ", "\u00854"} {
		cases = append(cases, bson.M{"count": count})
	}
	input := map[string]any{"events": cases, "now": now.Format(time.RFC3339Nano), "actor": map[string]any{"UserID": "context-user", "IP": "192.0.2.1", "Location": bson.M{"country": "FI"}}}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	script := `const fs=require('fs'),vm=require('vm'),path=require('path');
const root=process.argv[1],input=JSON.parse(fs.readFileSync(0,'utf8'));
const categories=require(path.join(root,'models/lib/securityCategories.js'));
const format=require(path.join(root,'models/lib/securityLogFormat.js'));
let docs=[];
const ActualDate=Date;class FixedDate extends ActualDate{constructor(...a){super(...(a.length?a:[input.now]));}}
const sandbox={Date:FixedDate,process:{env:{}},require(p){if(p.includes('securityCategories'))return categories;if(p.includes('securityLogFormat'))return format;throw Error(p)},
foldEventFireAndForget(doc){docs.push(doc)},blockAccountForSecurityEvent(){},currentReportRequest(){return {userId:input.actor.UserID,headers:{},socket:{remoteAddress:input.actor.IP}}},resolveClientKey(){return input.actor.IP},locationFromHeaders(){return input.actor.Location}};
vm.createContext(sandbox);
let source=fs.readFileSync(path.join(root,'server/lib/securityLog.js'),'utf8').replace(/^import .*;$/gm,'').replace(/^const \{ (resolveClientKey|locationFromHeaders) \} = require\(.*\);$/gm,'').replace('export function record','function record').replace('export default { record };','');
vm.runInContext(source,sandbox);
let block=fs.readFileSync(path.join(root,'server/lib/blockOnSecurityEvent.js'),'utf8').replace(/^import .*;$/gm,'').replace(/^const \{ categoryFor \} = require\(.*\);$/gm,'').replace(/export (async )?function/g,(_,a)=>(a||'')+'function');
vm.runInContext(block,sandbox);
const results=input.events.map(evt=>{docs=[];sandbox.record(evt);return {doc:docs[0],block:sandbox.shouldBlockAccount(evt),reason:sandbox.blockReason(evt)}});
process.stdout.write(JSON.stringify(results));`
	cmd := exec.Command(node, "-e", script, filepath.Clean(root))
	cmd.Stdin = strings.NewReader(string(encoded))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual source execution: %v\n%s", err, output)
	}
	var expected []map[string]any
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	for i, event := range cases {
		doc := SecurityDocument(event, SecurityContext{UserID: "context-user", IP: "192.0.2.1", Location: bson.M{"country": "FI"}}, now)
		actual := map[string]any{"doc": doc, "block": ShouldBlockSecurityAccount(event), "reason": SecurityBlockReason(event)}
		b, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		var normalized map[string]any
		if err := json.Unmarshal(b, &normalized); err != nil {
			t.Fatal(err)
		}
		// JavaScript JSON dates include millisecond zeros; Go time JSON may omit them.
		normalized["doc"].(map[string]any)["at"] = now.Format("2006-01-02T15:04:05.000Z")
		if !reflect.DeepEqual(normalized, expected[i]) {
			t.Errorf("case %d source mismatch\nGo: %#v\nJS: %#v", i, normalized, expected[i])
		}
	}
}

func TestSecurityReporterEffectsHaveIndependentBoundedContexts(t *testing.T) {
	seen := make(chan string, 2)
	check := func(ctx context.Context, effect string) {
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 0 || remaining > 5*time.Second {
			t.Errorf("%s context is not bounded to five seconds: %v %v", effect, deadline, ok)
		}
		seen <- effect
	}
	r := &SecurityReporter{now: time.Now,
		fold:  func(ctx context.Context, _ bson.M) error { check(ctx, "fold"); panic("failing log backend") },
		block: func(ctx context.Context, _ bson.M) error { check(ctx, "block"); return nil },
	}
	r.Record(bson.M{"userId": "u", "action": "blocked", "severity": "high"}, SecurityContext{})
	r.Close()
	if len(seen) != 2 {
		t.Fatalf("panic prevented independent side effect: %d", len(seen))
	}
}

func TestSecurityDocumentDefaultsAndUserIDAlias(t *testing.T) {
	now := time.Now()
	doc := SecurityDocument(bson.M{"userid": 42}, SecurityContext{}, now)
	if doc["userId"] != "42" || doc["stream"] != "security" || doc["severity"] != "info" || doc["category"] != "unknown" || doc["bleed"] != "Generic" || doc["action"] != "detected" || doc["detail"] != "" {
		t.Fatalf("default/alias schema: %#v", doc)
	}
	for _, field := range []string{"ip", "username", "location", "userid", "key"} {
		if _, ok := doc[field]; ok {
			t.Errorf("absent context invented %s", field)
		}
	}
	if ShouldBlockSecurityAccount(bson.M{"userid": "alias", "severity": "critical", "action": "blocked"}) {
		t.Fatal("raw blocking helper accepted alias before document normalization")
	}
	if reason := SecurityBlockReason(bson.M{}); reason != "Attempted a security guard while logged in" {
		t.Fatal(reason)
	}
}
