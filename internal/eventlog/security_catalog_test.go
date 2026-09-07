package eventlog

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSecurityCatalogDefaultsAndIndependentCopies(t *testing.T) {
	for _, key := range []string{"", "unknown.key", "__proto__", "constructor", "toString"} {
		entry := SecurityCategory(key)
		if entry["category"] != "unknown" || entry["bleed"] != "Generic" || entry["severity"] != "info" || entry["cwe"] != "" {
			t.Fatal(key, entry)
		}
	}
	first := SecurityCategory("authz.board-list")
	if first["category"] != "authz" || first["bleed"] != "StaleBleed" || first["severity"] != "medium" || first["cwe"] != "CWE-863" {
		t.Fatal(first)
	}
	first["bleed"] = "changed"
	if SecurityCategory("authz.board-list")["bleed"] != "StaleBleed" {
		t.Fatal("mutated shared catalog")
	}
	catalog := SecurityCatalog()
	if len(catalog) != 54 {
		t.Fatal(len(catalog))
	}
	catalog["brute.login"]["category"] = "changed"
	delete(catalog, "ssrf.fetch")
	if SecurityCategory("brute.login")["category"] != "brute-force" || len(SecurityCatalog()) != 54 {
		t.Fatal("inventory exposed shared maps")
	}
}
func TestSecurityCatalogSourceDifferential(t *testing.T) {
	want := eventlogSource(t, "securityCategories.js", `const fs=require('fs');const src=fs.readFileSync(process.argv[1],'utf8').replace(/export\s*\{[^}]+\};?\s*$/, 'return {CATALOG,categoryFor};');const {CATALOG,categoryFor}=new Function(src)();const defaults={};for(const key of ['', 'unknown.key','__proto__','constructor','toString'])Object.defineProperty(defaults,key,{value:categoryFor(key),enumerable:true});process.stdout.write(JSON.stringify({catalog:CATALOG,defaults}));`, nil)
	defaults := map[string]bson.M{}
	for _, key := range []string{"", "unknown.key", "__proto__", "constructor", "toString"} {
		defaults[key] = SecurityCategory(key)
	}
	eventlogJSONEqual(t, want, map[string]any{"catalog": SecurityCatalog(), "defaults": defaults})
}
