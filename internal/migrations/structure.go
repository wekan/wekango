package migrations

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func structuralString(v any) string {
	if v == nil {
		return "null"
	}
	if id, ok := v.(bson.ObjectID); ok {
		return id.Hex()
	}
	return fmt.Sprint(v)
}
func structuralField(d bson.M, k string) string {
	v, ok := d[k]
	if !ok {
		return "undefined"
	}
	return structuralString(v)
}
func structuralNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case bson.DateTime:
		return float64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	case string:
		if strings.TrimSpace(n) == "" {
			return 0
		}
		f, e := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if e != nil {
			return math.NaN()
		}
		return f
	}
	return 0
}
func structuralTruthy(v any) bool {
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
func structuralMissing() bson.M { return bson.M{"$in": bson.A{nil, ""}} }
func structuralStamped() bson.M {
	return bson.M{"swimlaneId": bson.M{"$nin": bson.A{nil, ""}}, "type": bson.M{"$ne": "template-list"}}
}
func structuralMarkers() bson.M {
	return bson.M{"$or": bson.A{bson.M{"fixMissingListsCompleted": true}, bson.M{"comprehensiveMigrationCompleted": true}}}
}
func structuralFind(ctx context.Context, db *mongo.Database, coll string, filter, projection bson.M) ([]bson.M, error) {
	c, e := db.Collection(coll).Find(ctx, filter, options.Find().SetProjection(projection))
	if e != nil {
		return nil, e
	}
	defer c.Close(ctx)
	var out []bson.M
	e = c.All(ctx, &out)
	return out, e
}
func structuralExists(ctx context.Context, db *mongo.Database, coll string, filter bson.M) (bool, error) {
	e := db.Collection(coll).FindOne(ctx, filter, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(e, mongo.ErrNoDocuments) {
		return false, nil
	}
	return e == nil, e
}

// structuralCancellationContext preserves Done/Err/Value but hides the deadline
// from the driver's automatic server-side maxTimeMS injection. The embedded
// FerretDB distinct command currently rejects that optional MongoDB field.
type structuralCancellationContext struct{ context.Context }

func (structuralCancellationContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func structuralDistinct(ctx context.Context, db *mongo.Database, coll, key string, filter bson.M) ([]string, error) {
	var values []any
	result := db.Collection(coll).Distinct(structuralCancellationContext{ctx}, key, filter)
	if err := result.Err(); err != nil {
		return nil, err
	}
	e := result.Decode(&values)
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, structuralString(v))
	}
	return out, e
}
func structuralSet(values []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range values {
		m[v] = true
	}
	return m
}
func structuralID() (string, error) {
	const chars = "23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz"
	out := make([]byte, 0, 17)
	for len(out) < 17 {
		var b [1]byte
		if _, e := rand.Read(b[:]); e != nil {
			return "", e
		}
		if int(b[0]) < 256-256%len(chars) {
			out = append(out, chars[int(b[0])%len(chars)])
		}
	}
	return string(out), nil
}
func structuralLanes(ctx context.Context, db *mongo.Database) (map[string]string, map[string]string, error) {
	all, e := structuralFind(ctx, db, "swimlanes", bson.M{}, bson.M{"_id": 1, "boardId": 1, "archived": 1, "type": 1, "sort": 1})
	if e != nil {
		return nil, nil, e
	}
	sort.SliceStable(all, func(i, j int) bool { return structuralNumber(all[i]["sort"]) < structuralNumber(all[j]["sort"]) })
	first, owners := map[string]string{}, map[string]string{}
	for _, s := range all {
		id, b := structuralField(s, "_id"), structuralField(s, "boardId")
		owners[id] = b
		if s["archived"] == true || s["type"] == "template-swimlane" {
			continue
		}
		if _, ok := first[b]; !ok {
			first[b] = id
		}
	}
	return first, owners, nil
}

// CheckSwimlaneStructure preserves the upstream bounded probes and distinct joins.
func CheckSwimlaneStructure(ctx context.Context, db *mongo.Database) (bool, error) {
	if yes, e := structuralExists(ctx, db, "cards", bson.M{"swimlaneId": structuralMissing()}); yes || e != nil {
		return yes, e
	}
	boards, e := structuralDistinct(ctx, db, "boards", "_id", bson.M{})
	if e != nil {
		return false, e
	}
	laneBoards, e := structuralDistinct(ctx, db, "swimlanes", "boardId", bson.M{})
	if e != nil {
		return false, e
	}
	owned := structuralSet(laneBoards)
	for _, b := range boards {
		if !owned[b] {
			return true, nil
		}
	}
	lists, e := structuralDistinct(ctx, db, "lists", "_id", bson.M{})
	if e != nil {
		return false, e
	}
	used, e := structuralDistinct(ctx, db, "cards", "listId", bson.M{})
	if e != nil {
		return false, e
	}
	ids := structuralSet(lists)
	for _, id := range used {
		if structuralUsable(id) && !ids[id] {
			return true, nil
		}
	}
	_, owners, e := structuralLanes(ctx, db)
	if e != nil {
		return false, e
	}
	used, e = structuralDistinct(ctx, db, "cards", "swimlaneId", bson.M{"archived": false})
	if e != nil {
		return false, e
	}
	for _, id := range used {
		if !structuralUsable(id) {
			continue
		}
		owner := owners[id]
		if owner == "" {
			return true, nil
		}
		bs, e := structuralDistinct(ctx, db, "cards", "boardId", bson.M{"swimlaneId": id, "archived": false})
		if e != nil {
			return false, e
		}
		for _, b := range bs {
			if b != owner {
				return true, nil
			}
		}
	}
	return false, nil
}
func structuralUsable(id string) bool { return id != "" && id != "null" && id != "undefined" }

// RunSwimlaneStructure does not stamp shared lists or resurrect archived lanes.
func RunSwimlaneStructure(ctx context.Context, db *mongo.Database) (result Result, err error) {
	now := time.Now()
	boards, e := structuralFind(ctx, db, "boards", bson.M{}, bson.M{"_id": 1})
	if e != nil {
		return result, e
	}
	defaults, owners, e := structuralLanes(ctx, db)
	if e != nil {
		return result, e
	}
	for _, b := range boards {
		bid := structuralField(b, "_id")
		if _, ok := defaults[bid]; ok {
			continue
		}
		id, e := structuralID()
		if e != nil {
			return result, e
		}
		_, e = db.Collection("swimlanes").InsertOne(ctx, bson.M{"_id": id, "title": "Default", "boardId": bid, "archived": false, "sort": 0, "type": "swimlane", "createdAt": now, "modifiedAt": now, "updatedAt": now})
		if e != nil {
			return result, e
		}
		defaults[bid] = id
		result.Fixed++
	}
	for b, id := range defaults {
		r, e := db.Collection("cards").UpdateMany(ctx, bson.M{"boardId": b, "swimlaneId": structuralMissing()}, bson.M{"$set": bson.M{"swimlaneId": id}})
		if e != nil {
			return result, e
		}
		result.Fixed += r.ModifiedCount
	}
	lists, e := structuralFind(ctx, db, "lists", bson.M{}, bson.M{"_id": 1, "boardId": 1, "swimlaneId": 1, "sort": 1})
	if e != nil {
		return result, e
	}
	ids := map[string]bool{}
	first := map[string]bson.M{}
	for _, l := range lists {
		ids[structuralField(l, "_id")] = true
	}
	sort.SliceStable(lists, func(i, j int) bool { return structuralNumber(lists[i]["sort"]) < structuralNumber(lists[j]["sort"]) })
	for _, l := range lists {
		b := structuralField(l, "boardId")
		if first[b] == nil {
			first[b] = l
		}
	}
	used, e := structuralDistinct(ctx, db, "cards", "listId", bson.M{})
	if e != nil {
		return result, e
	}
	for _, bad := range used {
		if !structuralUsable(bad) || ids[bad] {
			continue
		}
		bs, e := structuralDistinct(ctx, db, "cards", "boardId", bson.M{"listId": bad})
		if e != nil {
			return result, e
		}
		for _, b := range bs {
			target := first[b]
			if target == nil {
				id, e := structuralID()
				if e != nil {
					return result, e
				}
				target = bson.M{"_id": id, "title": "Rescued Data", "boardId": b, "swimlaneId": defaults[b], "archived": false, "sort": 0, "type": "list", "createdAt": now, "modifiedAt": now, "updatedAt": now}
				if _, e = db.Collection("lists").InsertOne(ctx, target); e != nil {
					return result, e
				}
				first[b] = target
			}
			lane := defaults[b]
			if structuralTruthy(target["swimlaneId"]) {
				lane = structuralString(target["swimlaneId"])
			}
			r, e := db.Collection("cards").UpdateMany(ctx, bson.M{"boardId": b, "listId": bad}, bson.M{"$set": bson.M{"listId": structuralField(target, "_id"), "swimlaneId": lane}})
			if e != nil {
				return result, e
			}
			result.Fixed += r.ModifiedCount
		}
	}
	used, e = structuralDistinct(ctx, db, "cards", "swimlaneId", bson.M{"archived": false})
	if e != nil {
		return result, e
	}
	for _, id := range used {
		if !structuralUsable(id) {
			continue
		}
		bs, e := structuralDistinct(ctx, db, "cards", "boardId", bson.M{"swimlaneId": id, "archived": false})
		if e != nil {
			return result, e
		}
		for _, b := range bs {
			if owners[id] == b {
				continue
			}
			target := defaults[b]
			if target == "" || target == id {
				continue
			}
			r, e := db.Collection("cards").UpdateMany(ctx, bson.M{"boardId": b, "swimlaneId": id, "archived": false}, bson.M{"$set": bson.M{"swimlaneId": target}})
			if e != nil {
				return result, e
			}
			result.Fixed += r.ModifiedCount
		}
	}
	return result, nil
}
func structuralMismatch(ctx context.Context, db *mongo.Database, l bson.M) (bool, error) {
	if !structuralTruthy(l["swimlaneId"]) {
		return false, nil
	}
	return structuralExists(ctx, db, "cards", bson.M{"listId": structuralField(l, "_id"), "swimlaneId": bson.M{"$nin": bson.A{structuralField(l, "swimlaneId"), nil, ""}}})
}
func structuralDuplicate(lists []bson.M) bool {
	seen := map[string]bool{}
	for _, l := range lists {
		if l["archived"] == true {
			continue
		}
		k := structuralField(l, "title")
		if seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

// CheckMergePerSwimlaneLists intentionally checks mismatches in archived lists too,
// as upstream does; the run phase's symptom detection excludes those lists.
func CheckMergePerSwimlaneLists(ctx context.Context, db *mongo.Database) (bool, error) {
	if yes, e := structuralExists(ctx, db, "boards", structuralMarkers()); yes || e != nil {
		return yes, e
	}
	bs, e := structuralDistinct(ctx, db, "lists", "boardId", structuralStamped())
	if e != nil {
		return false, e
	}
	for _, b := range bs {
		ls, e := structuralFind(ctx, db, "lists", bson.M{"boardId": b, "type": bson.M{"$ne": "template-list"}}, bson.M{"title": 1, "swimlaneId": 1, "archived": 1})
		if e != nil {
			return false, e
		}
		if structuralDuplicate(ls) {
			return true, nil
		}
		for _, l := range ls {
			if yes, e := structuralMismatch(ctx, db, l); yes || e != nil {
				return yes, e
			}
		}
	}
	return false, nil
}

// RunMergePerSwimlaneLists relinks cards and history, preserves card lanes and
// renamed columns, and clears only the upstream era markers.
func RunMergePerSwimlaneLists(ctx context.Context, db *mongo.Database) (result Result, err error) {
	ms, e := structuralFind(ctx, db, "boards", structuralMarkers(), bson.M{"_id": 1})
	if e != nil {
		return result, e
	}
	markers := map[string]bool{}
	var boards []string
	for _, m := range ms {
		b := structuralField(m, "_id")
		markers[b] = true
		boards = append(boards, b)
	}
	stamped, e := structuralDistinct(ctx, db, "lists", "boardId", structuralStamped())
	if e != nil {
		return result, e
	}
	for _, b := range stamped {
		if !markers[b] {
			boards = append(boards, b)
		}
	}
	for _, b := range boards {
		ls, e := structuralFind(ctx, db, "lists", bson.M{"boardId": b, "type": bson.M{"$ne": "template-list"}}, bson.M{"title": 1, "swimlaneId": 1, "archived": 1, "sort": 1, "createdAt": 1})
		if e != nil {
			return result, e
		}
		active := []bson.M{}
		for _, l := range ls {
			if l["archived"] != true {
				active = append(active, l)
			}
		}
		if !markers[b] && !structuralDuplicate(active) {
			mismatch := false
			for _, l := range active {
				yes, e := structuralMismatch(ctx, db, l)
				if e != nil {
					return result, e
				}
				if yes {
					mismatch = true
					break
				}
			}
			if !mismatch {
				continue
			}
		}
		groups := map[string][]bson.M{}
		var order []string
		for _, l := range active {
			k := structuralField(l, "title")
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], l)
		}
		for _, k := range order {
			g := groups[k]
			if len(g) < 2 {
				continue
			}
			sort.SliceStable(g, func(i, j int) bool {
				a, z := g[i], g[j]
				if structuralTruthy(a["swimlaneId"]) != structuralTruthy(z["swimlaneId"]) {
					return !structuralTruthy(a["swimlaneId"])
				}
				if delta := structuralNumber(a["createdAt"]) - structuralNumber(z["createdAt"]); delta != 0 && !math.IsNaN(delta) {
					return structuralNumber(a["createdAt"]) < structuralNumber(z["createdAt"])
				}
				return structuralNumber(a["sort"]) < structuralNumber(z["sort"])
			})
			for _, m := range g[1:] {
				filter := bson.M{"listId": structuralField(m, "_id")}
				update := bson.M{"$set": bson.M{"listId": structuralField(g[0], "_id")}}
				if _, e = db.Collection("cards").UpdateMany(ctx, filter, update); e != nil {
					return result, e
				}
				_, _ = db.Collection("activities").UpdateMany(ctx, filter, update)
				if _, e = db.Collection("lists").DeleteOne(ctx, bson.M{"_id": m["_id"]}); e != nil {
					return result, e
				}
				result.Fixed++
			}
		}
		filter := structuralStamped()
		filter["boardId"] = b
		r, e := db.Collection("lists").UpdateMany(ctx, filter, bson.M{"$set": bson.M{"swimlaneId": ""}})
		if e != nil {
			return result, e
		}
		result.Fixed += r.ModifiedCount
		if _, e = db.Collection("boards").UpdateOne(ctx, bson.M{"_id": b}, bson.M{"$unset": bson.M{"fixMissingListsCompleted": 1, "fixMissingListsCompletedAt": 1, "comprehensiveMigrationCompleted": 1}}); e != nil {
			return result, e
		}
	}
	return result, nil
}
