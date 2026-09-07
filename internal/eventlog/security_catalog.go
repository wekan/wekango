package eventlog

// Catalog from models/lib/securityCategories.js. Return fresh maps so a caller
// cannot change the category, severity or CWE associated with later events.
import "go.mongodb.org/mongo-driver/v2/bson"

type securityCategoryEntry struct{ category, bleed, severity, cwe string }

var securityCategories = map[string]securityCategoryEntry{
	"ssrf.redirect":               {"ssrf", "RedirectBleed", "high", "CWE-918"},
	"ssrf.attachment":             {"ssrf", "LiveBleed", "high", "CWE-918"},
	"ssrf.fetch":                  {"ssrf", "DnsBleed", "high", "CWE-918"},
	"ssrf.webhook":                {"ssrf", "IntegrationBleed", "high", "CWE-918"},
	"xss.source":                  {"xss", "SourceBleed", "high", "CWE-79"},
	"xss.mime":                    {"xss", "MimeBleed", "high", "CWE-79"},
	"xss.input":                   {"xss", "InputBleed", "medium", "CWE-79"},
	"spoofing.xff":                {"spoofing", "MetricsBleed", "medium", "CWE-290"},
	"authz.export":                {"authz", "ImpersonateBleed", "high", "CWE-863"},
	"authn.import":                {"authn", "ImportBleed", "critical", "CWE-306"},
	"authn.miniprofile":           {"authn", "MiniProfileBleed", "medium", "CWE-306"},
	"authz.board":                 {"authz", "BoardBleed", "high", "CWE-863"},
	"authz.position-history":      {"authz", "PositionHistoryBleed", "high", "CWE-639"},
	"auth-race.cas":               {"auth-race", "CasBleed", "high", "CWE-362"},
	"authn.cas-link":              {"authn", "CasBleed", "medium", "CWE-287"},
	"auth-race.oidc":              {"auth-race", "OIDCBleed", "high", "CWE-362"},
	"brute.invite":                {"brute-force", "InviteBleed", "high", "CWE-307"},
	"brute.login":                 {"brute-force", "BruteBleed", "medium", "CWE-307"},
	"brute.account-recovery":      {"brute-force", "ResetBleed", "high", "CWE-307"},
	"brute.lockout":               {"brute-force", "JamBleed", "high", "CWE-307"},
	"injection.shell":             {"injection", "ScannerBleed", "high", "CWE-78"},
	"file.mime":                   {"file", "MimeBleed", "high", "CWE-434"},
	"file.name":                   {"file", "FileBleed", "medium", "CWE-73"},
	"file.sanitize":               {"file", "FileBleed", "info", "CWE-73"},
	"file.content":                {"file", "FileBleed", "medium", "CWE-79"},
	"file.malware":                {"file", "MalwareBleed", "high", "CWE-509"},
	"file.size":                   {"file", "SpaceBleed", "low", "CWE-400"},
	"file.disk":                   {"file", "FloppyBleed", "low", "CWE-400"},
	"file.avatar-url":             {"ssrf", "RedirectBleed", "high", "CWE-918"},
	"file.policy":                 {"file", "PolicyBleed", "info", ""},
	"authz.canary":                {"authz", "CanaryBleed", "medium", "CWE-863"},
	"authz.checklist":             {"authz", "ChecklistBleed", "high", "CWE-863"},
	"authz.comment":               {"authz", "CommentBleed", "medium", "CWE-639"},
	"authz.file-path":             {"authz", "PathBleed", "high", "CWE-22"},
	"authz.parent":                {"authz", "ParentBleed", "high", "CWE-862"},
	"authz.share":                 {"authz", "RevokeBleed", "high", "CWE-863"},
	"authz.readonly":              {"authz", "ReadOnlyBleed", "medium", "CWE-863"},
	"authz.calendar":              {"authz", "CalendarBleed", "medium", "CWE-863"},
	"authz.assigned":              {"authz", "AssignedBleed", "medium", "CWE-863"},
	"authz.tenant":                {"authz", "TenantBleed", "medium", "CWE-862"},
	"authz.search-session":        {"authz", "SessionBleed", "medium", "CWE-639"},
	"authz.database":              {"authz", "DatabaseBleed", "high", "CWE-863"},
	"authz.swimlane-create":       {"authz", "SwimlaneBleed", "medium", "CWE-862"},
	"authz.rule-destination":      {"authz", "RuleBleed", "high", "CWE-862"},
	"authz.card-delete":           {"authz", "PurgeBleed", "critical", "CWE-639"},
	"authz.card-member":           {"authz", "GuestBleed", "high", "CWE-639"},
	"authz.board-list":            {"authz", "StaleBleed", "medium", "CWE-863"},
	"spoofing.author":             {"spoofing", "AuthorBleed", "medium", "CWE-345"},
	"authz.register":              {"authz", "SignupBleed", "critical", "CWE-862"},
	"authn.authentication-method": {"authn", "MembershipBleed", "high", "CWE-200"},
	"injection.nosql":             {"injection", "SelectorBleed", "high", "CWE-943"},
	"injection.sql":               {"injection", "EscapeBleed", "critical", "CWE-89"},
	"integrity.history":           {"integrity", "HistoryIntegrity", "critical", "CWE-345"},
	"integrity.file":              {"integrity", "StorageBleed", "high", "CWE-353"},
}

func SecurityCategory(key string) bson.M {
	entry, ok := securityCategories[key]
	if !ok {
		entry = securityCategoryEntry{"unknown", "Generic", "info", ""}
	}
	return bson.M{"category": entry.category, "bleed": entry.bleed, "severity": entry.severity, "cwe": entry.cwe}
}
func SecurityCatalog() map[string]bson.M {
	result := make(map[string]bson.M, len(securityCategories))
	for key := range securityCategories {
		result[key] = SecurityCategory(key)
	}
	return result
}
