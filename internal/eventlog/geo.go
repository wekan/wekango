package eventlog

// Proxy geography is DISPLAY ONLY. Never use these forgeable labels or
// coordinates for authentication, authorization, rate limiting or security keys.
import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type geoSource struct{ country, city, region, lat, lon, via string }

var geoSources = []geoSource{
	{"cf-ipcountry", "cf-ipcity", "cf-region", "cf-iplatitude", "cf-iplongitude", "Cloudflare"},
	{"fastly-client-country-code", "fastly-client-city", "fastly-client-region", "fastly-client-lat", "fastly-client-lon", "Fastly"},
	{"cloudfront-viewer-country", "cloudfront-viewer-city", "cloudfront-viewer-country-region-name", "cloudfront-viewer-latitude", "cloudfront-viewer-longitude", "CloudFront"},
	{"x-vercel-ip-country", "x-vercel-ip-city", "x-vercel-ip-country-region", "x-vercel-ip-latitude", "x-vercel-ip-longitude", "Vercel"},
	{"x-client-geo-country", "x-client-geo-city", "x-client-geo-region", "", "", "Google Cloud"},
	{"x-geoip-country", "x-geoip-city", "x-geoip-region", "x-geoip-latitude", "x-geoip-longitude", "proxy"},
}

func geoTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return (r >= 9 && r <= 13) || r == 32 || r == 0xa0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200a) || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
func geoDecode(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	s = geoTrim(s)
	if !strings.Contains(s, "%") {
		return s
	}
	decoded, err := url.PathUnescape(s)
	if err != nil || !utf8.ValidString(decoded) {
		return s
	}
	return decoded
}

var geoFloatPrefix = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)

func geoNumber(value any) (float64, bool) {
	prefix := geoFloatPrefix.FindString(geoTrim(usageString(value)))
	if prefix == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(prefix, 64)
	if (err != nil && n != 0) || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, false
	}
	return n, true
}

// LocationFromHeaders accepts lowercase Node-style header names. Non-string
// place labels are ignored; parseFloat semantics apply only to coordinates.
func LocationFromHeaders(headers map[string]any) bson.M {
	for _, source := range geoSources {
		country, city, region := geoDecode(headers[source.country]), geoDecode(headers[source.city]), geoDecode(headers[source.region])
		if country == "" && city == "" && region == "" {
			continue
		}
		if city == "" && region == "" && (country == "XX" || country == "T1") {
			continue
		}
		location := bson.M{"via": source.via}
		if country != "" {
			location["country"] = country
		}
		if city != "" {
			location["city"] = city
		}
		if region != "" {
			location["region"] = region
		}
		lat, lok := geoNumber(headers[source.lat])
		lon, rok := geoNumber(headers[source.lon])
		if source.lat != "" && source.lon != "" && lok && rok {
			location["latitude"] = lat
			location["longitude"] = lon
		}
		return location
	}
	return nil
}
func LocationLabel(location bson.M) string {
	city, region, country := location["city"], location["region"], location["country"]
	if usageTruthy(city) {
		if usageTruthy(country) {
			return usageString(city) + ", " + usageString(country)
		}
		return usageString(city)
	}
	if usageTruthy(region) {
		if usageTruthy(country) {
			return usageString(region) + ", " + usageString(country)
		}
		return usageString(region)
	}
	if usageTruthy(country) {
		return usageString(country)
	}
	return ""
}
func HasCoordinates(location bson.M) bool {
	return geoIsNumber(location["latitude"]) && geoIsNumber(location["longitude"])
}
func geoIsNumber(value any) bool {
	switch value.(type) {
	case float64, float32, int, int32, int64:
		return true
	}
	return false
}
func CountryFlag(code any) string {
	if !usageTruthy(code) {
		return ""
	}
	cc := cases.Upper(language.Und).String(geoTrim(usageString(code)))
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' || cc == "XX" {
		return ""
	}
	return string([]rune{0x1f1e6 + rune(cc[0]-'A'), 0x1f1e6 + rune(cc[1]-'A')})
}
func OfficeLabel(location bson.M) bson.M {
	text := ""
	for _, key := range []string{"city", "region", "country"} {
		if usageTruthy(location[key]) {
			text = usageString(location[key])
			break
		}
	}
	return bson.M{"flag": CountryFlag(location["country"]), "text": text}
}
