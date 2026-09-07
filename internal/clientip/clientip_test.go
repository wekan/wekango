package clientip

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHopSelection(t *testing.T) {
	for _, tc := range []struct{ count, chain, want string }{
		{"", "forged", "192.0.2.1"}, {"0", "forged", "192.0.2.1"}, {"-1", "forged", "192.0.2.1"},
		{"1", "forged, 198.51.100.2", "198.51.100.2"}, {"2", "forged, 198.51.100.2, 203.0.113.3", "198.51.100.2"},
		{"2", "198.51.100.2", "192.0.2.1"}, {"2", " , 198.51.100.2, ,203.0.113.3, ", "198.51.100.2"},
		{"1junk", "literal-client-key", "literal-client-key"}, {"+2.5", "a,b,c", "b"},
		{"0x2", "a,b,c", "192.0.2.1"}, {"+0X2", "a,b,c", "192.0.2.1"}, {"08", "a,b,c", "192.0.2.1"}, {"99999999999999999999999999", "a,b,c", "192.0.2.1"},
		{"\ufeff1", "\ufeffclient\ufeff", "client"}, {"\u00851", "a,b", "192.0.2.1"},
	} {
		got := Resolve(http.Header{"X-Forwarded-For": []string{tc.chain}}, "192.0.2.1:9000", Count(tc.count))
		if got != tc.want {
			t.Errorf("%q %q = %q, want %q", tc.count, tc.chain, got, tc.want)
		}
	}
	if got := Resolve(http.Header{"X-Forwarded-For": []string{"spoof", "client,proxy"}}, "[::1]:80", 2); got != "client" {
		t.Fatal(got)
	}
	if got := Resolve(nil, "[2001:db8::1]:8000", 2); got != "2001:db8::1" {
		t.Fatal(got)
	}
	if got := Resolve(nil, "", 0); got != "unknown" {
		t.Fatal(got)
	}
}

func TestSourceClientKeyDifferential(t *testing.T) {
	source := os.Getenv("WEKAN_SOURCE_ROOT")
	if source == "" {
		t.Skip("requires reference WeKan source")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	type input struct {
		Headers map[string]string `json:"headers"`
		Socket  string            `json:"socketAddress"`
		Count   string            `json:"forwardedCount"`
	}
	fixtures := []input{}
	for _, count := range []string{"", "0", "-1", "1", "2", "  +2junk", "0x2", "+0X2", "0b10", "08", "1.9", "bad", "9999999999999999999999999999", "\ufeff2", "\u00851"} {
		for _, chain := range []string{"", "spoof", "spoof,client,proxy", " ,,client, ,proxy,", "\ufeffclient\ufeff,proxy", "\u0085client,proxy"} {
			fixtures = append(fixtures, input{map[string]string{"x-forwarded-for": chain}, "192.0.2.1", count})
		}
	}
	data, _ := json.Marshal(fixtures)
	cmd := exec.Command(node, "-e", `const fs=require('node:fs');const {resolveClientKey}=require(process.argv[1]);process.stdout.write(JSON.stringify(JSON.parse(fs.readFileSync(0,'utf8')).map(resolveClientKey)));`, filepath.Join(source, "server/lib/loginAttemptThrottle.js"))
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source: %v %s", err, out)
	}
	var want []string
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	for i, fixture := range fixtures {
		got := Resolve(http.Header{"X-Forwarded-For": []string{fixture.Headers["x-forwarded-for"]}}, fixture.Socket, Count(fixture.Count))
		if got != want[i] {
			t.Fatalf("fixture %+v: got %q want %q", fixture, got, want[i])
		}
	}
}
