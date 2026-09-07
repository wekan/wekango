package api

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Evaluate the installed application's hook, rather than reproducing its
// truthiness expression in the JavaScript oracle. Missing/empty values include
// the normal Collection2 unset result and raw historical import representations.
func TestSourceLoginDisabledStates(t *testing.T) {
	root := os.Getenv("WEKAN_SOURCE_ROOT")
	if root == "" {
		t.Skip("WEKAN_SOURCE_ROOT required for actual login hook differential")
	}
	node := os.Getenv("WEKAN_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	fixtures := []struct {
		name, expression string
		value            any
	}{
		{"missing", "undefined", nil},
		{"undefined", "undefined", bson.Undefined{}},
		{"null", "null", nil},
		{"false", "false", false},
		{"true", "true", true},
		{"empty-string", `""`, ""},
		{"string-false", `"false"`, "false"},
		{"string-zero", `"0"`, "0"},
		{"whitespace", `" "`, " "},
		{"zero-int32", "0", int32(0)},
		{"zero-int64", "0", int64(0)},
		{"zero-double", "0", float64(0)},
		{"negative-zero", "-0", math.Copysign(0, -1)},
		{"one-int32", "1", int32(1)},
		{"negative-int64", "-1", int64(-1)},
		{"fraction", "0.5", 0.5},
		{"nan", "NaN", math.NaN()},
		{"infinity", "Infinity", math.Inf(1)},
		{"negative-infinity", "-Infinity", math.Inf(-1)},
		{"empty-array", "[]", bson.A{}},
		{"false-array", "[false]", bson.A{false}},
		{"empty-object", "({})", bson.M{}},
		{"false-object", "({value:false})", bson.M{"value": false}},
		{"ordered-object", "({})", bson.D{}},
		{"bson-date", "new Date(0)", bson.DateTime(0)},
		{"time-date", "new Date(0)", time.Unix(0, 0)},
		{"object-id", "({toHexString(){return '000000000000000000000000'}})", bson.ObjectID{}},
	}
	expressions := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		expressions[i] = fixture.expression
	}
	input, err := json.Marshal(expressions)
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const fs = require('fs');
const vm = require('vm');
const source = fs.readFileSync(process.argv[1], 'utf8');
const hooks = [...source.matchAll(/Accounts\.validateLoginAttempt\(function\(options\)\s*\{([\s\S]*?)\n  \}\);/g)]
  .filter(match => match[1].includes('loginDisabled'));
if (hooks.length !== 1) throw new Error('Expected exactly one loginDisabled validation hook');
let hook;
vm.runInNewContext(hooks[0][0], {Accounts: {validateLoginAttempt(fn) {hook = fn;}}});
const expressions = JSON.parse(process.argv[2]);
const results = expressions.map(expression => {
  const value = vm.runInNewContext(expression);
  const user = expression === 'undefined' ? {} : {loginDisabled: value};
  return !hook({user});
});
process.stdout.write(JSON.stringify(results));
`
	output, err := exec.Command(node, "-e", script, filepath.Join(root, "server", "authentication.js"), string(input)).CombinedOutput()
	if err != nil {
		t.Fatalf("source hook: %v\n%s", err, output)
	}
	var expected []bool
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatalf("source result: %v\n%s", err, output)
	}
	if len(expected) != len(fixtures) {
		t.Fatalf("source returned %d results, want %d", len(expected), len(fixtures))
	}
	for i, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if got := loginDisabled(fixture.value); got != expected[i] {
				t.Fatalf("loginDisabled(%#v) = %v, source = %v", fixture.value, got, expected[i])
			}
		})
	}
}
