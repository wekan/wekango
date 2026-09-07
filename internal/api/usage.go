package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/wekan/wekango/internal/eventlog"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Usage authentication matches the global Meteor middleware's token lookup.
// Route authorization remains independent and may reject expired/disabled users.
func (s *service) usageUserID(r *http.Request) string {
	token := ""
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:6], "Bearer") && (auth[6] == ' ' || auth[6] == '\t') {
		token = strings.TrimLeft(auth[6:], " \t")
	}
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}
	if token == "" {
		return ""
	}
	var u struct {
		ID string `bson:"_id"`
	}
	if s.db.Collection("users").FindOne(r.Context(), bson.M{"services.resume.loginTokens.hashedToken": tokenHash(token)}, options.FindOne().SetProjection(bson.M{"_id": 1})).Decode(&u) != nil {
		return ""
	}
	return u.ID
}

func usageRoute(pattern string) string {
	_, route, ok := strings.Cut(pattern, " ")
	if !ok {
		route = pattern
	}
	// Optional-slash aliases represent the same Express route, not a new
	// endpoint containing ServeMux's end-of-path marker.
	route = strings.TrimSuffix(route, "/{$}")
	// Registered Go wildcards have the same parameter boundaries as Express.
	// The existing API routes use ID in Go and Id in their public patterns.
	route = strings.ReplaceAll(route, "{", ":")
	route = strings.ReplaceAll(route, "}", "")
	for _, name := range []string{"board", "list", "swimlane", "card", "user"} {
		route = strings.ReplaceAll(route, ":"+name+"ID", ":"+name+"Id")
	}
	return route
}

func (s *service) recordUsage(r *http.Request, userID string) {
	// Reporting is best-effort and must not turn a completed response into panic.
	defer func() { _ = recover() }()
	headers := make(map[string]any, len(r.Header))
	for name, values := range r.Header {
		headers[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	event := bson.M{"name": eventlog.APIName(r.Method, usageRoute(r.Pattern)), "userId": userID, "ip": s.clientKey(r), "at": time.Now()}
	if location := eventlog.LocationFromHeaders(headers); location != nil {
		event["location"] = location
	}
	s.options.Usage.Add(event)
}
