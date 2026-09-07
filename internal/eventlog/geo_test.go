package eventlog

import (
	"math"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestGeoSourcePrecedenceAndLabels(t *testing.T) {
	headers := map[string]any{"cf-ipcountry": "XX", "fastly-client-city": "New%20York", "fastly-client-country-code": "US", "fastly-client-lat": "40.7north", "fastly-client-lon": "-74west", "x-geoip-city": "ignored"}
	location := LocationFromHeaders(headers)
	if location["city"] != "New York" || location["via"] != "Fastly" || location["latitude"] != 40.7 || location["longitude"] != -74.0 {
		t.Fatal(location)
	}
	if LocationLabel(location) != "New York, US" || OfficeLabel(location)["flag"] != "🇺🇸" || !HasCoordinates(location) {
		t.Fatal(location)
	}
	headers["cf-ipcity"] = "Unknown town"
	location = LocationFromHeaders(headers)
	if location["via"] != "Cloudflare" || HasCoordinates(location) {
		t.Fatal(location)
	}
	if LocationFromHeaders(map[string]any{"cf-ipcountry": "T1"}) != nil {
		t.Fatal("Tor treated as a country")
	}
	if LocationFromHeaders(map[string]any{"cf-iplatitude": "10", "cf-iplongitude": "20"}) != nil {
		t.Fatal("coordinates alone identify a source")
	}
	if LocationLabel(nil) != "" || OfficeLabel(nil)["text"] != "" || HasCoordinates(nil) {
		t.Fatal("nil location")
	}
}
func TestGeoParsingPreservesMalformedAndLiteralPlus(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"New+York", "New+York"}, {"New%20York", "New York"}, {"%E2%82%AC", "€"}, {"%ED%A0%80", "%ED%A0%80"}, {"bad%ZZ", "bad%ZZ"}, {"\ufeffParis\ufeff", "Paris"}, {"\u0085Paris", "\u0085Paris"}, {"%20Paris%20", " Paris "}} {
		if got := geoDecode(tc.raw); got != tc.want {
			t.Fatalf("%q -> %q want %q", tc.raw, got, tc.want)
		}
	}
	for _, tc := range []struct {
		raw   string
		want  float64
		valid bool
	}{{"  +1.2e2tail", 120, true}, {"1e+", 1, true}, {"0x20", 0, true}, {".25south", 0.25, true}, {"-1e-9999", 0, true}, {"Infinity", 0, false}, {"1e9999", 0, false}, {"", 0, false}, {"\ufeff3", 3, true}, {"\u00853", 0, false}} {
		got, ok := geoNumber(tc.raw)
		if got != tc.want || ok != tc.valid {
			t.Fatalf("%q -> %v,%v", tc.raw, got, ok)
		}
	}
	if CountryFlag(" ß ") != "🇸🇸" || CountryFlag("xx") != "" || CountryFlag("T1") != "" || CountryFlag("usa") != "" {
		t.Fatal("flag normalization")
	}
	if !HasCoordinates(bson.M{"latitude": math.NaN(), "longitude": math.Inf(1)}) {
		t.Fatal("hasCoordinates must preserve source typeof-number semantics")
	}
}
func TestGeoSourceDifferential(t *testing.T) {
	fixtures := []map[string]any{nil, {}, {"cf-ipcountry": "XX"}, {"cf-ipcountry": "T1", "x-geoip-city": "Fallback"}, {"cf-ipcountry": "XX", "cf-ipcity": "City"}, {"cf-ipcountry": 42, "cf-ipcity": []string{"ignored"}}, {"x-client-geo-city": "Google", "x-client-geo-country": "GB"}, {"cf-ipcity": "First", "x-vercel-ip-city": "Second"}}
	for _, source := range geoSources {
		for _, value := range []string{"New%20York", "New+York", "%ED%A0%80", "%E2%82%AC", "%E2%82", "%00", " \ufeffCity\ufeff ", "\u0085City"} {
			fixtures = append(fixtures, map[string]any{source.city: value, source.country: "GB", source.region: "Region", source.lat: "51.5degrees", source.lon: "-.12"})
		}
	}
	for _, value := range []any{"1e+", "1e-9999", "1e9999", "0x20", "+2.5tail", "-Infinity", "", "\ufeff2", "\u00852", nil, true, 2, []any{3, 4}} {
		fixtures = append(fixtures, map[string]any{"cf-ipcountry": "GB", "cf-iplatitude": value, "cf-iplongitude": "3"})
	}
	want := eventlogSource(t, "geoHeaders.js", `const fs=require('fs');const g=require(process.argv[1]);process.stdout.write(JSON.stringify(JSON.parse(fs.readFileSync(0,'utf8')).map(h=>{const l=g.locationFromHeaders(h);return {location:l,label:g.locationLabel(l),coordinates:g.hasCoordinates(l),office:g.officeLabel(l)}})));`, fixtures)
	got := []any{}
	for _, headers := range fixtures {
		location := LocationFromHeaders(headers)
		got = append(got, map[string]any{"location": location, "label": LocationLabel(location), "coordinates": HasCoordinates(location), "office": OfficeLabel(location)})
	}
	eventlogJSONEqual(t, want, got)
}
