package eventlog

import (
	"math"
	"strings"
	"testing"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSecurityDetailWhitespaceAndCoercion(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{{nil, ""}, {false, "false"}, {0, "0"}, {math.Copysign(0, -1), "0"}, {math.Inf(1), "Infinity"}, {math.NaN(), "NaN"}, {1e-7, "1e-7"}, {bson.M{"password": "must not serialize"}, "[object Object]"}, {bson.A{"a", nil, "b"}, "a,,b"}, {" \x00hello\x01\t\r\nworld\x7f ", "hello world"}, {"\ufeffA\u2003B\u00a0C\ufeff", "A B C"}, {"\u0085keep\u0085", "\u0085keep\u0085"}} {
		if got := SanitizeSecurityDetail(tc.in); got != tc.want {
			t.Fatalf("%#v: %q want %q", tc.in, got, tc.want)
		}
	}
}
func TestSecurityDetailUTF16Boundary(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{strings.Repeat("a", 500), strings.Repeat("a", 500)}, {strings.Repeat("a", 501), strings.Repeat("a", 499) + "…"}, {strings.Repeat("😀", 250), strings.Repeat("😀", 250)}, {strings.Repeat("😀", 251), strings.Repeat("😀", 249) + "�…"}, {strings.Repeat("a", 499) + "😀", strings.Repeat("a", 499) + "…"}} {
		got := SanitizeSecurityDetail(tc.in)
		if got != tc.want {
			t.Fatalf("UTF16 result: len=%d want=%d", len(utf16.Encode([]rune(got))), len(utf16.Encode([]rune(tc.want))))
		}
		if len(utf16.Encode([]rune(got))) > 500 {
			t.Fatal("detail exceeds source limit")
		}
	}
}
func TestSecurityDetailSourceDifferential(t *testing.T) {
	values := []any{nil, false, true, 0, -123, 1e-7, 1e21, bson.M{"private": "do not serialize"}, bson.A{"a", nil, "b"}, bson.A{bson.M{}, bson.A{1, 2}}, "", strings.Repeat("a", 499), strings.Repeat("a", 500), strings.Repeat("a", 501), strings.Repeat("😀", 250), strings.Repeat("😀", 251), strings.Repeat("a", 499) + "😀"}
	for _, r := range []rune{0, 1, 9, 10, 13, 31, 32, 127, 0x85, 0xa0, 0x1680, 0x2000, 0x200b, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff} {
		values = append(values, string(r)+"A"+string(r)+string(r)+"B"+string(r))
	}
	want := eventlogSource(t, "securityLogFormat.js", `const fs=require('fs');const src=fs.readFileSync(process.argv[1],'utf8').replace(/export\s*\{[^}]+\};?\s*$/, 'return {sanitizeDetail};');const {sanitizeDetail}=new Function(src)();process.stdout.write(JSON.stringify(JSON.parse(fs.readFileSync(0,'utf8')).map(v=>Buffer.from(sanitizeDetail(v),'utf8').toString('utf8'))));`, values)
	got := make([]string, len(values))
	for i, v := range values {
		got[i] = SanitizeSecurityDetail(v)
	}
	eventlogJSONEqual(t, want, got)
}
