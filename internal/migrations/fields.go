package migrations

// These steps preserve the selectors and counts in WeKan's
// server/lib/schemaUpgradeSteps.js. In particular, null and false are stored
// choices, not missing defaults; and board-allows-defaults counts fields fixed.
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

var BoardAllowsTrueDefaults = []string{
	"allowsSubtasks", "allowsSubtasksOnMinicard", "allowsAttachments", "allowsAttachmentsOnMinicard",
	"allowsChecklists", "allowsChecklistsOnMinicard", "allowsComments", "allowsDescriptionTitle",
	"allowsDescriptionTitleOnMinicard", "allowsDescriptionText", "allowsCoverAttachmentOnMinicard",
	"allowsActivities", "allowsLabels", "allowsLabelsOnMinicard", "allowsCreator", "allowsCustomFields",
	"allowsAssignee", "allowsAssigneeOnMinicard", "allowsMembers", "allowsMembersOnMinicard",
	"allowsRequestedBy", "allowsRequestedByOnMinicard", "allowsCardSortingByNumber", "allowsShowLists",
	"allowsAssignedBy", "allowsAssignedByOnMinicard", "allowsReceivedDate", "allowsReceivedDateOnMinicard",
	"allowsStartDate", "allowsStartDateOnMinicard", "allowsEndDate", "allowsEndDateOnMinicard",
	"allowsDueDate", "allowsDueDateOnMinicard",
}
var archivedCollections = []string{"boards", "swimlanes", "lists", "cards"}
var sortedCollections = []string{"cards", "lists", "swimlanes", "checklists", "checklistItems"}

func fieldExists(ctx context.Context, db *mongo.Database, collection string, filter bson.M) (bool, error) {
	err := db.Collection(collection).FindOne(ctx, filter, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}
func fieldCheckCollections(ctx context.Context, db *mongo.Database, collections []string, filter bson.M) (bool, error) {
	for _, name := range collections {
		found, err := fieldExists(ctx, db, name, filter)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}
func fieldUpdateCollections(ctx context.Context, db *mongo.Database, collections []string, filter, update bson.M) (Result, error) {
	var result Result
	for _, name := range collections {
		r, err := db.Collection(name).UpdateMany(ctx, filter, update)
		if err != nil {
			return result, err
		}
		result.Fixed += r.ModifiedCount
	}
	return result, nil
}
func CheckArchivedFlagBackfill(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldCheckCollections(ctx, db, archivedCollections, bson.M{"archived": bson.M{"$exists": false}})
}
func RunArchivedFlagBackfill(ctx context.Context, db *mongo.Database) (Result, error) {
	return fieldUpdateCollections(ctx, db, archivedCollections, bson.M{"archived": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"archived": false}})
}
func CheckBoardAllowsDefaults(ctx context.Context, db *mongo.Database) (bool, error) {
	clauses := bson.A{}
	for _, field := range BoardAllowsTrueDefaults {
		clauses = append(clauses, bson.M{field: bson.M{"$exists": false}})
	}
	return fieldExists(ctx, db, "boards", bson.M{"$or": clauses})
}
func RunBoardAllowsDefaults(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	for _, field := range BoardAllowsTrueDefaults {
		r, err := db.Collection("boards").UpdateMany(ctx, bson.M{field: bson.M{"$exists": false}}, bson.M{"$set": bson.M{field: true}})
		if err != nil {
			return result, err
		}
		result.Fixed += r.ModifiedCount
	}
	return result, nil
}
func CheckBoardPermissionLowercase(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldExists(ctx, db, "boards", bson.M{"permission": bson.M{"$in": bson.A{"PUBLIC", "PRIVATE", "Public", "Private"}}})
}
func RunBoardPermissionLowercase(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	for _, from := range []string{"PUBLIC", "Public", "PRIVATE", "Private"} {
		r, err := db.Collection("boards").UpdateMany(ctx, bson.M{"permission": from}, bson.M{"$set": bson.M{"permission": strings.ToLower(from)}})
		if err != nil {
			return result, err
		}
		result.Fixed += r.ModifiedCount
	}
	return result, nil
}
func nonfiniteSortFilter() bson.M {
	return bson.M{"sort": bson.M{"$type": "number"}, "$nor": bson.A{bson.M{"sort": bson.M{"$gte": -math.MaxFloat64, "$lte": math.MaxFloat64}}}}
}
func CheckNonfiniteSortRepair(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldCheckCollections(ctx, db, sortedCollections, nonfiniteSortFilter())
}
func RunNonfiniteSortRepair(ctx context.Context, db *mongo.Database) (Result, error) {
	return fieldUpdateCollections(ctx, db, sortedCollections, nonfiniteSortFilter(), bson.M{"$set": bson.M{"sort": 0}})
}
func CheckCustomFieldsBoardIDs(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldExists(ctx, db, "customFields", bson.M{"boardId": bson.M{"$exists": true}})
}
func RunCustomFieldsBoardIDs(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	cursor, err := db.Collection("customFields").Find(ctx, bson.M{"boardId": bson.M{"$exists": true}})
	if err != nil {
		return result, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err = cursor.Decode(&doc); err != nil {
			return result, err
		}
		update := bson.M{"$unset": bson.M{"boardId": ""}}
		if len(fieldArray(doc["boardIds"])) == 0 {
			update["$set"] = bson.M{"boardIds": bson.A{fieldString(doc["boardId"])}}
		}
		if _, err = db.Collection("customFields").UpdateOne(ctx, bson.M{"_id": doc["_id"]}, update); err != nil {
			return result, err
		}
		result.Fixed++
	}
	return result, cursor.Err()
}
func memberFilter() bson.M {
	return bson.M{"members": bson.M{"$elemMatch": bson.M{"isActive": bson.M{"$exists": false}}}}
}
func memberNeedsDefault(member any) bool {
	if !fieldTruthy(member) {
		return false
	}
	_, exists := fieldMap(member)["isActive"]
	return !exists
}
func CheckBoardMembersIsActive(ctx context.Context, db *mongo.Database) (bool, error) {
	found, err := fieldExists(ctx, db, "boards", memberFilter())
	if err == nil {
		return found, nil
	}
	cursor, err := db.Collection("boards").Find(ctx, bson.M{"members.0": bson.M{"$exists": true}}, options.Find().SetProjection(bson.M{"members": 1}))
	if err != nil {
		return false, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err = cursor.Decode(&doc); err != nil {
			return false, err
		}
		for _, m := range fieldArray(doc["members"]) {
			if memberNeedsDefault(m) {
				return true, nil
			}
		}
	}
	return false, cursor.Err()
}
func RunBoardMembersIsActive(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	cursor, err := db.Collection("boards").Find(ctx, memberFilter(), options.Find().SetProjection(bson.M{"members": 1}))
	if err != nil {
		cursor, err = db.Collection("boards").Find(ctx, bson.M{"members.0": bson.M{"$exists": true}}, options.Find().SetProjection(bson.M{"members": 1}))
	}
	if err != nil {
		return result, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err = cursor.Decode(&doc); err != nil {
			return result, err
		}
		members := fieldArray(doc["members"])
		changed := false
		for i, m := range members {
			if !memberNeedsDefault(m) {
				continue
			}
			patched := fieldMap(m)
			if patched == nil {
				patched = bson.M{}
			}
			patched["isActive"] = true
			members[i] = patched
			changed = true
		}
		if !changed {
			continue
		}
		if _, err = db.Collection("boards").UpdateOne(ctx, bson.M{"_id": doc["_id"]}, bson.M{"$set": bson.M{"members": members}}); err != nil {
			return result, err
		}
		result.Fixed++
	}
	return result, cursor.Err()
}
func CheckChecklistItemsEmbedded(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldExists(ctx, db, "checklists", bson.M{"items.0": bson.M{"$exists": true}})
}
func RunChecklistItemsEmbedded(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	now := time.Now()
	cursor, err := db.Collection("checklists").Find(ctx, bson.M{"items.0": bson.M{"$exists": true}})
	if err != nil {
		return result, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var checklist bson.M
		if err = cursor.Decode(&checklist); err != nil {
			return result, err
		}
		items := fieldArray(checklist["items"])
		sort.SliceStable(items, func(i, j int) bool {
			return fieldNumber(fieldMap(items[i])["sort"])-fieldNumber(fieldMap(items[j])["sort"]) < 0
		})
		existing, err := db.Collection("checklistItems").Find(ctx, bson.M{"checklistId": fieldString(checklist["_id"])}, options.Find().SetProjection(bson.M{"title": 1, "sort": 1, "isFinished": 1}))
		if err != nil {
			return result, err
		}
		seen := map[string]bool{}
		for existing.Next(ctx) {
			var item bson.M
			if err = existing.Decode(&item); err != nil {
				existing.Close(ctx)
				return result, err
			}
			seen[fieldItemKey(item)] = true
		}
		err = existing.Err()
		existing.Close(ctx)
		if err != nil {
			return result, err
		}
		docs := []any{}
		for i, raw := range items {
			item := fieldMap(raw)
			title := item["title"]
			if !fieldTruthy(title) {
				title = "Checklist"
			}
			cardID := checklist["cardId"]
			if !fieldTruthy(cardID) {
				cardID = ""
			}
			id, err := fieldRandomID()
			if err != nil {
				return result, err
			}
			doc := bson.M{"_id": id, "title": title, "sort": i, "isFinished": fieldTruthy(item["isFinished"]), "checklistId": fieldString(checklist["_id"]), "cardId": fieldString(cardID), "createdAt": now, "modifiedAt": now}
			if seen[fieldItemKey(doc)] {
				continue
			}
			docs = append(docs, doc)
		}
		if len(docs) > 0 {
			if _, err = db.Collection("checklistItems").InsertMany(ctx, docs, options.InsertMany().SetOrdered(false)); err != nil {
				return result, err
			}
			result.Fixed += int64(len(docs))
		}
		if _, err = db.Collection("checklists").UpdateOne(ctx, bson.M{"_id": checklist["_id"]}, bson.M{"$unset": bson.M{"items": 1}}); err != nil {
			return result, err
		}
	}
	return result, cursor.Err()
}
func fieldItemKey(doc bson.M) string {
	title, ok := doc["title"]
	t := "undefined"
	if ok {
		t = fieldString(title)
	}
	s := "undefined"
	if v, ok := doc["sort"]; ok {
		s = fieldString(v)
	}
	return t + "\x00" + s + "\x00" + strconv.FormatBool(fieldTruthy(doc["isFinished"]))
}
func fieldRandomID() (string, error) {
	const chars = "23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz"
	out := make([]byte, 0, 17)
	var b [32]byte
	for len(out) < 17 {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		for _, v := range b {
			if int(v) >= 256-256%len(chars) {
				continue
			}
			out = append(out, chars[int(v)%len(chars)])
			if len(out) == 17 {
				break
			}
		}
	}
	return string(out), nil
}
func fieldArray(value any) []any {
	switch v := value.(type) {
	case bson.A:
		return []any(v)
	case []any:
		return v
	}
	return nil
}
func fieldMap(value any) bson.M {
	switch v := value.(type) {
	case bson.M:
		return v
	case map[string]any:
		return bson.M(v)
	case bson.D:
		m := bson.M{}
		for _, e := range v {
			m[e.Key] = e.Value
		}
		return m
	}
	return nil
}
func fieldTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0 && !math.IsNaN(v)
	}
	return true
}
func fieldNumber(value any) float64 {
	if !fieldTruthy(value) {
		return 0
	}
	switch v := value.(type) {
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case bool:
		return 1
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if strings.TrimSpace(v) == "" {
			return 0
		}
		if err == nil {
			return n
		}
	}
	return math.NaN()
}
func fieldString(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bson.ObjectID:
		return v.Hex()
	case bson.M, bson.D, map[string]any:
		return "[object Object]"
	case bson.A, []any:
		parts := []string{}
		for _, item := range fieldArray(v) {
			if item == nil {
				parts = append(parts, "")
			} else {
				parts = append(parts, fieldString(item))
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(value)
	}
}

var fieldExtensionMIME = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"jpe":  "image/jpeg",
	"jfif": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"bmp":  "image/bmp",
	"svg":  "image/svg+xml",
	"avif": "image/avif",
	"heic": "image/heic",
	"heif": "image/heif",
	"tif":  "image/tiff",
	"tiff": "image/tiff",
	"ico":  "image/x-icon",
	"mp4":  "video/mp4",
	"webm": "video/webm",
	"ogv":  "video/ogg",
	"mov":  "video/quicktime",
	"m4v":  "video/x-m4v",
	"mkv":  "video/x-matroska",
	"avi":  "video/x-msvideo",
	"mp3":  "audio/mpeg",
	"ogg":  "audio/ogg",
	"oga":  "audio/ogg",
	"wav":  "audio/wav",
	"flac": "audio/flac",
	"m4a":  "audio/mp4",
	"aac":  "audio/aac",
	"opus": "audio/opus",
	"pdf":  "application/pdf",
	"json": "application/json",
	"txt":  "text/plain",
	"md":   "text/markdown",
	"csv":  "text/csv",
	"log":  "text/plain",
	"ini":  "text/plain",
	"conf": "text/plain",
	"yml":  "text/yaml",
	"yaml": "text/yaml",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

func CheckAttachmentKindFlags(ctx context.Context, db *mongo.Database) (bool, error) {
	return fieldExists(ctx, db, "attachments", bson.M{"isImage": bson.M{"$exists": false}, "isVideo": bson.M{"$exists": false}})
}
func RunAttachmentKindFlags(ctx context.Context, db *mongo.Database) (Result, error) {
	var result Result
	projection := bson.M{}
	for _, key := range []string{"name", "type", "mime", "mime-type", "contentType", "extension", "ext", "versions", "isImage", "isVideo", "isAudio", "isPDF", "isJSON", "isText", "meta"} {
		projection[key] = 1
	}
	cursor, err := db.Collection("attachments").Find(ctx, bson.M{}, options.Find().SetProjection(projection))
	if err != nil {
		return result, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err = cursor.Decode(&doc); err != nil {
			return result, err
		}
		fix := fieldAttachmentKindFix(doc)
		if len(fix) == 0 {
			continue
		}
		if _, err = db.Collection("attachments").UpdateOne(ctx, bson.M{"_id": doc["_id"]}, bson.M{"$set": fix}); err != nil {
			return result, err
		}
		result.Fixed++
	}
	return result, cursor.Err()
}

// The source's unresolved condition is unreachable: a nonempty fix contains
// only true flags or nonempty extension/type values. Unknown files remain alone.
func fieldAttachmentKindFix(doc bson.M) bson.M {
	original := fieldMap(fieldMap(doc["versions"])["original"])
	stated := ""
	for _, v := range []any{doc["type"], doc["mime"], doc["mime-type"], doc["contentType"], original["type"], fieldMap(doc["meta"])["type"]} {
		if s, ok := v.(string); ok && strings.Contains(s, "/") {
			stated = strings.ToLower(s)
			break
		}
	}
	extension := ""
	for _, v := range []any{doc["extension"], doc["ext"], original["extension"]} {
		if s, ok := v.(string); ok && s != "" && s != "." {
			extension = strings.ToLower(strings.TrimPrefix(s, "."))
			break
		}
	}
	if extension == "" {
		name := doc["name"]
		if !fieldTruthy(name) {
			name = fieldMap(doc["original"])["name"]
		}
		if s, ok := name.(string); ok {
			s = strings.SplitN(strings.SplitN(s, "?", 2)[0], "#", 2)[0]
			s = strings.ReplaceAll(s, "\\", "/")
			s = s[strings.LastIndex(s, "/")+1:]
			if dot := strings.LastIndex(s, "."); dot > 0 && dot < len(s)-1 {
				extension = strings.ToLower(s[dot+1:])
			}
		}
	}
	mime := stated
	if mime == "" {
		mime = fieldExtensionMIME[extension]
	}
	fix := bson.M{}
	for flag, prefix := range map[string]string{"isImage": "image/", "isVideo": "video/", "isAudio": "audio/", "isText": "text/"} {
		if strings.HasPrefix(mime, prefix) && doc[flag] != true {
			fix[flag] = true
		}
	}
	for flag, kind := range map[string]string{"isPDF": "application/pdf", "isJSON": "application/json"} {
		if mime == kind && doc[flag] != true {
			fix[flag] = true
		}
	}
	if extension != "" && !fieldTruthy(doc["extension"]) && !fieldTruthy(doc["ext"]) {
		fix["extension"] = extension
		fix["ext"] = extension
	}
	if mime != "" && !fieldTruthy(doc["type"]) {
		fix["type"] = mime
	}
	return fix
}
