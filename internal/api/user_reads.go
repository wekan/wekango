package api

import (
	"context"
	"errors"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// registerUserReads ports the three read handlers in server/models/users.js.
// Self reads remove services only; admin detail reads additionally remove
// sessionData. The asymmetry preserves the existing REST response contract.
func (s *service) registerUserReads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/user", s.selfUser)
	mux.HandleFunc("GET /api/users", s.usersList)
	mux.HandleFunc("GET /api/users/{userID}", s.userByID)
	// Express routes accept one optional trailing slash.
	mux.HandleFunc("GET /api/user/{$}", s.selfUser)
	mux.HandleFunc("GET /api/users/{$}", s.usersList)
	mux.HandleFunc("GET /api/users/{userID}/{$}", s.userByID)
}

func (s *service) selfUser(w http.ResponseWriter, r *http.Request) {
	authenticated, e := s.boardReadUser(r)
	if e != nil {
		boardReadPublicError(w, e)
		return
	}
	var data bson.M
	if err := s.db.Collection("users").FindOne(r.Context(), bson.M{"_id": authenticated.ID}).Decode(&data); err != nil {
		boardReadPublicError(w, boardReadInternal())
		return
	}
	delete(data, "services")
	boards, err := s.userReadMemberships(r.Context(), authenticated.ID)
	if err != nil {
		boardReadPublicError(w, err)
		return
	}
	data["boards"] = boards
	reply(w, http.StatusOK, jsonDocument(data))
}

func (s *service) usersList(w http.ResponseWriter, r *http.Request) {
	if !s.boardReadAdmin(w, r) {
		return
	}
	users, err := boardReadFind(r.Context(), s.db, "users", bson.M{}, options.Find().SetProjection(bson.M{"_id": 1, "username": 1}))
	if err != nil {
		boardReadPublicError(w, boardReadInternal())
		return
	}
	// Source maps the projection a second time, so even a future query change
	// cannot accidentally expose a profile, password hash or session credential.
	result := make(bson.A, 0, len(users))
	for _, doc := range users {
		result = append(result, boardReadFields(doc, "_id", "username"))
	}
	reply(w, http.StatusOK, jsonDocument(result))
}

func (s *service) userByID(w http.ResponseWriter, r *http.Request) {
	if !s.boardReadAdmin(w, r) {
		return
	}
	id := r.PathValue("userID")
	var data bson.M
	err := s.db.Collection("users").FindOne(r.Context(), bson.M{"_id": id}).Decode(&data)
	if errors.Is(err, mongo.ErrNoDocuments) {
		err = s.db.Collection("users").FindOne(r.Context(), bson.M{"username": id}).Decode(&data)
	}
	// The source dereferences user._id when both lookups fail, whose public
	// error is InternalServerError (500), not a newly invented 404 response.
	if err != nil {
		boardReadPublicError(w, boardReadInternal())
		return
	}
	boards, e := s.userReadMemberships(r.Context(), data["_id"])
	if e != nil {
		boardReadPublicError(w, e)
		return
	}
	data["boards"] = boards
	delete(data, "services")
	delete(data, "sessionData")
	reply(w, http.StatusOK, jsonDocument(data))
}

// userReadMemberships preserves the first-match Array.find behavior. Archived
// boards, inactive memberships and all extra membership flags remain visible.
// Malformed non-array members or null/undefined entries encountered before a
// match throw in JavaScript and produce 500; entries after a match are ignored.
// A selector match without a strictly equal member.userId also produces 500.
func (s *service) userReadMemberships(ctx context.Context, id any) (bson.A, *boardReadError) {
	boards, err := boardReadFind(ctx, s.db, "boards", bson.M{"type": "board", "members.userId": id}, options.Find().SetProjection(bson.M{"_id": 1, "members": 1}))
	if err != nil {
		return nil, boardReadInternal()
	}
	result := make(bson.A, 0, len(boards))
	for _, board := range boards {
		members, ok := board["members"].(bson.A)
		if !ok {
			return nil, boardReadInternal()
		}
		var found bson.M
		for _, raw := range members {
			if raw == nil {
				return nil, boardReadInternal()
			}
			if _, undefined := raw.(bson.Undefined); undefined {
				return nil, boardReadInternal()
			}
			member := userReadMemberMap(raw)
			if userReadStrictID(member, id) {
				found = member
				break
			}
		}
		if found == nil {
			return nil, boardReadInternal()
		}
		copy := make(bson.M, len(found))
		for key, value := range found {
			if key != "userId" {
				copy[key] = value
			}
		}
		copy["boardId"] = board["_id"]
		result = append(result, copy)
	}
	return result, nil
}
func userReadMemberMap(raw any) bson.M {
	switch value := raw.(type) {
	case bson.M:
		return value
	case map[string]any:
		return bson.M(value)
	case bson.D:
		result := bson.M{}
		for _, entry := range value {
			result[entry.Key] = entry.Value
		}
		return result
	default:
		return nil
	}
}

// Ordinary Meteor IDs are strings. Preserve JS strict equality for malformed
// imported primitive IDs too; object/array/date IDs from separate fetches are
// never reference-equal, even if their BSON representations match.
func userReadStrictID(member bson.M, id any) bool {
	value, present := member["userId"]
	if !present {
		return false
	}
	switch target := id.(type) {
	case nil:
		return value == nil
	case string:
		got, ok := value.(string)
		return ok && got == target
	case bool:
		got, ok := value.(bool)
		return ok && got == target
	case int, int32, int64, float64:
		number := func(v any) (float64, bool) {
			switch n := v.(type) {
			case int:
				return float64(n), true
			case int32:
				return float64(n), true
			case int64:
				return float64(n), true
			case float64:
				return n, true
			}
			return 0, false
		}
		expected, _ := number(target)
		got, ok := number(value)
		return ok && got == expected
	default:
		return false
	}
}
