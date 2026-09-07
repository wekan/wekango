package api

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// registerBoardReads ports the read handlers in server/models/{boards,lists,
// swimlanes,cards}.js. The existing single-board handler is registered by New.
// Shared API usage accounting wraps all routes in api.go.
func (s *service) registerBoardReads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/users/{userID}/boards", s.userBoards)
	mux.HandleFunc("GET /api/boards", s.publicBoards)
	mux.HandleFunc("GET /api/boards_count", s.boardCounts)
	mux.HandleFunc("GET /api/boards/{boardID}/lists", s.boardLists)
	mux.HandleFunc("GET /api/boards/{boardID}/lists/{listID}", s.boardList)
	mux.HandleFunc("GET /api/boards/{boardID}/swimlanes", s.boardSwimlanes)
	mux.HandleFunc("GET /api/boards/{boardID}/swimlanes/{swimlaneID}", s.boardSwimlane)
	mux.HandleFunc("GET /api/boards/{boardID}/lists/{listID}/cards", s.listCards)
	mux.HandleFunc("GET /api/boards/{boardID}/swimlanes/{swimlaneID}/cards", s.swimlaneCards)
	mux.HandleFunc("GET /api/cards/{cardID}", s.cardByID)
	mux.HandleFunc("GET /api/boards/{boardID}/lists/{listID}/cards/{cardID}", s.scopedCard)
}

type boardReadError struct {
	status       int
	name, reason string
}

func boardReadUnauthorized() *boardReadError {
	return &boardReadError{401, "Unauthorized", "Unauthorized"}
}
func boardReadInternal() *boardReadError {
	return &boardReadError{500, "InternalServerError", "Internal server error"}
}
func (s *service) boardReadUser(r *http.Request) (*user, *boardReadError) {
	u, _, err := s.authenticate(r)
	if err != nil {
		return nil, boardReadUnauthorized()
	}
	return u, nil
}

// boardReadAccess follows Authentication.checkBoardAccess, including archived
// boards and the admin override. Public visibility does not bypass membership.
func (s *service) boardReadAccess(r *http.Request, boardID string) *boardReadError {
	u, err := s.boardReadUser(r)
	if err != nil {
		return err
	}
	var board bson.M
	e := s.db.Collection("boards").FindOne(r.Context(), bson.M{"_id": boardID}).Decode(&board)
	if errors.Is(e, mongo.ErrNoDocuments) {
		return &boardReadError{404, "NotFound", "Board not found"}
	}
	if e != nil {
		return boardReadInternal()
	}
	members, ok := board["members"].(bson.A)
	if !ok {
		return boardReadInternal()
	}
	for _, raw := range members {
		if raw == nil {
			return boardReadInternal()
		}
		m, ok := raw.(bson.D)
		var member bson.M
		if ok {
			member = bson.M{}
			for _, entry := range m {
				member[entry.Key] = entry.Value
			}
		} else {
			member, _ = raw.(bson.M)
		}
		if member["userId"] == u.ID && boardReadTruthy(member["isActive"]) && !boardReadTruthy(member["isNoComments"]) && !boardReadTruthy(member["isCommentOnly"]) && !boardReadTruthy(member["isWorker"]) {
			return nil
		}
	}
	if u.IsAdmin {
		return nil
	}
	return &boardReadError{403, "Forbidden", "Forbidden"}
}
func boardReadTruthy(v any) bool {
	switch n := v.(type) {
	case nil:
		return false
	case bool:
		return n
	case string:
		return n != ""
	case int:
		return n != 0
	case int32:
		return n != 0
	case int64:
		return n != 0
	case float64:
		return n != 0 && !math.IsNaN(n)
	default:
		return true
	}
}
func boardReadPublicError(w http.ResponseWriter, e *boardReadError) {
	reply(w, e.status, bson.M{"error": e.reason})
}

// Older lists/swimlanes handlers deliberately return HTTP 200 with the raw
// enumerable Meteor.Error fields. Missing children return an empty 200 instead.
func boardReadLegacyError(w http.ResponseWriter, e *boardReadError) {
	if e.status == 500 {
		reply(w, 200, bson.M{})
		return
	}
	reply(w, 200, bson.M{"isClientSafe": true, "error": e.name, "reason": e.reason, "message": e.reason + " [" + e.name + "]", "errorType": "Meteor.Error", "statusCode": e.status})
}

// Card GETs have no local catch. Match Express finalhandler's production HTTP
// response, without exposing development-only JavaScript stack traces.
func boardReadCardError(w http.ResponseWriter, e *boardReadError) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	body := fmt.Sprintf("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Error</title>\n</head>\n<body>\n<pre>%s</pre>\n</body>\n</html>\n", html.EscapeString(http.StatusText(e.status)))
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(e.status)
	_, _ = fmt.Fprint(w, body)
}
func boardReadFind(ctx context.Context, db *mongo.Database, collection string, selector bson.M, opts ...options.Lister[options.FindOptions]) ([]bson.M, error) {
	cursor, e := db.Collection(collection).Find(ctx, selector, opts...)
	if e != nil {
		return nil, e
	}
	defer cursor.Close(ctx)
	docs := []bson.M{}
	e = cursor.All(ctx, &docs)
	return docs, e
}
func boardReadFields(doc bson.M, keys ...string) bson.M {
	out := bson.M{}
	for _, key := range keys {
		if value, ok := doc[key]; ok {
			out[key] = value
		}
	}
	return out
}

var boardReadCaretTitle = regexp.MustCompile(`^\^[^\r\n\x{2028}\x{2029}]*\^$`)

func boardReadTrimTitle(title string) string {
	// JavaScript trim includes BOM and excludes NEL; Go TrimSpace differs.
	return strings.TrimFunc(title, func(r rune) bool {
		return (r >= 9 && r <= 13) || r == 32 || r == 0xa0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200a) || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}

func boardReadVisibleSummaries(boards []bson.M) []bson.M {
	out := []bson.M{}
	for _, b := range boards {
		if b["type"] != "board" {
			continue
		}
		if title, ok := b["title"].(string); ok && boardReadCaretTitle.MatchString(boardReadTrimTitle(title)) {
			continue
		}
		out = append(out, boardReadFields(b, "_id", "title"))
	}
	return out
}
func boardReadAny(docs []bson.M) []any {
	out := make([]any, len(docs))
	for i, d := range docs {
		out[i] = d
	}
	return out
}
func (s *service) boardReadAdmin(w http.ResponseWriter, r *http.Request) bool {
	u, e := s.boardReadUser(r)
	if e != nil {
		boardReadPublicError(w, e)
		return false
	}
	if !u.IsAdmin {
		boardReadPublicError(w, &boardReadError{403, "Forbidden", "Forbidden"})
		return false
	}
	return true
}
func (s *service) publicBoards(w http.ResponseWriter, r *http.Request) {
	if !s.boardReadAdmin(w, r) {
		return
	}
	docs, e := boardReadFind(r.Context(), s.db, "boards", bson.M{"permission": "public"}, options.Find().SetSort(bson.D{{Key: "sort", Value: 1}}))
	if e != nil {
		boardReadPublicError(w, boardReadInternal())
		return
	}
	reply(w, 200, jsonDocument(bson.A(boardReadAny(boardReadVisibleSummaries(docs)))))
}
func (s *service) boardCounts(w http.ResponseWriter, r *http.Request) {
	if !s.boardReadAdmin(w, r) {
		return
	}
	counts := bson.M{}
	for _, visibility := range []string{"private", "public"} {
		n, e := s.db.Collection("boards").CountDocuments(r.Context(), bson.M{"permission": visibility})
		if e != nil {
			boardReadPublicError(w, boardReadInternal())
			return
		}
		counts[visibility] = n
	}
	reply(w, 200, counts)
}
func boardReadDate(value any) (float64, bool) {
	switch v := value.(type) {
	case bson.DateTime:
		return float64(v), true
	case time.Time:
		return float64(v.UnixMilli()), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02", time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "Jan 2, 2006", "January 2, 2006"} {
			if parsed, e := time.Parse(layout, v); e == nil {
				return float64(parsed.UnixMilli()), true
			}
		}
	}
	return 0, false
}
func (s *service) boardLists(w http.ResponseWriter, r *http.Request) {
	bid := r.PathValue("boardID")
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadLegacyError(w, e)
		return
	}
	lists, e := boardReadFind(r.Context(), s.db, "lists", bson.M{"boardId": bid, "archived": false})
	if e != nil {
		boardReadLegacyError(w, boardReadInternal())
		return
	}
	cards, e := boardReadFind(r.Context(), s.db, "cards", bson.M{"boardId": bid}, options.Find().SetProjection(bson.M{"listId": 1, "modifiedAt": 1, "dateLastActivity": 1}))
	if e != nil {
		boardReadLegacyError(w, boardReadInternal())
		return
	}
	newest := map[string]float64{}
	for _, card := range cards {
		id, ok := card["listId"].(string)
		if !ok || id == "" {
			continue
		}
		for _, field := range []string{"modifiedAt", "dateLastActivity"} {
			t, ok := boardReadDate(card[field])
			if !ok {
				continue
			}
			previous, exists := newest[id]
			if !exists || t > previous {
				newest[id] = t
			}
		}
	}
	out := bson.A{}
	for _, l := range lists {
		d := boardReadFields(l, "_id", "title")
		d["modifiedAt"] = nil
		if boardReadTruthy(l["modifiedAt"]) {
			d["modifiedAt"] = l["modifiedAt"]
		}
		d["cardsModifiedAt"] = nil
		if id, ok := l["_id"].(string); ok {
			if t, found := newest[id]; found && math.Abs(t) <= 8.64e15 {
				d["cardsModifiedAt"] = bson.DateTime(int64(t))
			}
		}
		out = append(out, d)
	}
	reply(w, 200, jsonDocument(out))
}
func (s *service) boardSwimlanes(w http.ResponseWriter, r *http.Request) {
	bid := r.PathValue("boardID")
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadLegacyError(w, e)
		return
	}
	docs, e := boardReadFind(r.Context(), s.db, "swimlanes", bson.M{"boardId": bid, "archived": false})
	if e != nil {
		boardReadLegacyError(w, boardReadInternal())
		return
	}
	out := bson.A{}
	for _, d := range docs {
		out = append(out, boardReadFields(d, "_id", "title"))
	}
	reply(w, 200, jsonDocument(out))
}
func (s *service) boardReadChild(w http.ResponseWriter, r *http.Request, coll, key string) {
	bid := r.PathValue("boardID")
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadLegacyError(w, e)
		return
	}
	var d bson.M
	e := s.db.Collection(coll).FindOne(r.Context(), bson.M{"_id": r.PathValue(key), "boardId": bid, "archived": false}).Decode(&d)
	if errors.Is(e, mongo.ErrNoDocuments) {
		w.WriteHeader(200)
		return
	}
	if e != nil {
		boardReadLegacyError(w, boardReadInternal())
		return
	}
	reply(w, 200, jsonDocument(d))
}
func (s *service) boardList(w http.ResponseWriter, r *http.Request) {
	s.boardReadChild(w, r, "lists", "listID")
}
func (s *service) boardSwimlane(w http.ResponseWriter, r *http.Request) {
	s.boardReadChild(w, r, "swimlanes", "swimlaneID")
}
func (s *service) boardReadCards(w http.ResponseWriter, r *http.Request, scope, pathKey, otherScope string) {
	bid := r.PathValue("boardID")
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadCardError(w, e)
		return
	}
	docs, e := boardReadFind(r.Context(), s.db, "cards", bson.M{"boardId": bid, scope: r.PathValue(pathKey), "archived": false}, options.Find().SetSort(bson.D{{Key: "sort", Value: 1}}))
	if e != nil {
		boardReadCardError(w, boardReadInternal())
		return
	}
	out := bson.A{}
	for _, d := range docs {
		out = append(out, boardReadFields(d, "_id", "title", "description", otherScope, "receivedAt", "startAt", "dueAt", "endAt", "assignees", "sort"))
	}
	reply(w, 200, jsonDocument(out))
}
func (s *service) listCards(w http.ResponseWriter, r *http.Request) {
	s.boardReadCards(w, r, "listId", "listID", "swimlaneId")
}
func (s *service) swimlaneCards(w http.ResponseWriter, r *http.Request) {
	s.boardReadCards(w, r, "swimlaneId", "swimlaneID", "listId")
}
func (s *service) cardByID(w http.ResponseWriter, r *http.Request) {
	var d bson.M
	e := s.db.Collection("cards").FindOne(r.Context(), bson.M{"_id": r.PathValue("cardID")}).Decode(&d)
	if errors.Is(e, mongo.ErrNoDocuments) {
		reply(w, 404, bson.M{"error": "Card not found"})
		return
	}
	if e != nil {
		boardReadCardError(w, boardReadInternal())
		return
	}
	bid, _ := d["boardId"].(string)
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadCardError(w, e)
		return
	}
	reply(w, 200, jsonDocument(d))
}
func (s *service) scopedCard(w http.ResponseWriter, r *http.Request) {
	bid := r.PathValue("boardID")
	if e := s.boardReadAccess(r, bid); e != nil {
		boardReadCardError(w, e)
		return
	}
	var d bson.M
	e := s.db.Collection("cards").FindOne(r.Context(), bson.M{"_id": r.PathValue("cardID"), "boardId": bid, "listId": r.PathValue("listID"), "archived": false}).Decode(&d)
	if errors.Is(e, mongo.ErrNoDocuments) {
		w.WriteHeader(200)
		return
	}
	if e != nil {
		boardReadCardError(w, boardReadInternal())
		return
	}
	reply(w, 200, jsonDocument(d))
}
