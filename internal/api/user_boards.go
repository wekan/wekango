package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/wekan/wekango/internal/eventlog"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *service) userBoards(w http.ResponseWriter, r *http.Request) {
	u, err := s.boardReadUser(r)
	if err != nil {
		boardReadPublicError(w, err)
		return
	}
	id := r.PathValue("userID")
	if id != u.ID && !u.IsAdmin {
		boardReadPublicError(w, &boardReadError{403, "Forbidden", "Forbidden"})
		return
	}
	selector := bson.M{"archived": false, "members": bson.M{"$elemMatch": bson.M{"userId": id, "isActive": true}}}
	boards, e := boardReadFind(r.Context(), s.db, "boards", selector, options.Find().SetSort(bson.D{{Key: "sort", Value: 1}}))
	if e != nil {
		boardReadPublicError(w, boardReadInternal())
		return
	}
	// This guard's catalog severity is medium. blockOnSecurityEvent only acts
	// on high/critical events, so looking at this listing never blocks an account.
	s.recordRevokedBoardListing(r, u.ID, id)
	reply(w, http.StatusOK, boardReadVisibleSummaries(boards))
}

func (s *service) recordRevokedBoardListing(r *http.Request, caller, target string) {
	defer func() { _ = recover() }() // reporting cannot change the authorization result
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	n, err := s.db.Collection("boards").CountDocuments(ctx, bson.M{"archived": false, "members": bson.M{"$elemMatch": bson.M{"userId": target, "isActive": false}}})
	if err != nil || n == 0 {
		return
	}
	ip := utf16.Encode([]rune(s.clientKey(r)))
	if len(ip) > 64 {
		ip = ip[:64]
	}
	event := bson.M{"stream": "security", "severity": "medium", "category": "authz", "bleed": "StaleBleed", "action": "blocked", "source": "GET /api/users/:userId/boards", "cwe": "CWE-863", "detail": fmt.Sprintf("withheld %d board(s) whose membership is revoked", n), "userId": caller, "ip": string(utf16.Decode(ip)), "at": time.Now()}
	headers := map[string]any{}
	for key, values := range r.Header {
		headers[strings.ToLower(key)] = strings.Join(values, ", ")
	}
	if location := eventlog.LocationFromHeaders(headers); location != nil {
		event["location"] = location
	}
	_ = s.events.Fold(ctx, event)
}
