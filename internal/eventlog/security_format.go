package eventlog

import (
	"strings"
	"unicode/utf16"
)

const MaxSecurityDetail = 500

// SanitizeSecurityDetail mirrors securityLogFormat.js, including ECMAScript
// whitespace and its UTF-16 length limit. Splitting a supplementary character
// emits U+FFFD, the same replacement produced when JavaScript's dangling UTF-16
// surrogate is encoded as UTF-8 for MongoDB storage.
func SanitizeSecurityDetail(value any) string {
	if summaryNull(value) {
		return ""
	}
	text := summaryString(value)
	var out strings.Builder
	space := false
	for _, r := range text {
		if r <= 0x1f || r == 0x7f || geoTrim(string(r)) == "" {
			if out.Len() > 0 {
				space = true
			}
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(r)
	}
	cleaned := out.String()
	chars := utf16.Encode([]rune(cleaned))
	if len(chars) > MaxSecurityDetail {
		return string(utf16.Decode(chars[:MaxSecurityDetail-1])) + "…"
	}
	return cleaned
}
