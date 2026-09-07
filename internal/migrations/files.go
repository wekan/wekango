package migrations

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// FilesystemOptions uses the same WRITABLE_PATH meaning as WeKan. Empty means
// the working directory. Log receives recoverable copy failures.
type FilesystemOptions struct {
	WritablePath string
	Log          func(string)
}

type brokenFilesystemVersion struct {
	collection string
	document   bson.M
	name       string
	version    bson.M
}

// CheckFilesystemPaths performs fs-path-heal's metadata and disk existence
// probe. Existing recorded paths remain authoritative, including old layouts.
func CheckFilesystemPaths(ctx context.Context, db *mongo.Database, opts FilesystemOptions) (bool, error) {
	broken, err := findBrokenFilesystemVersions(ctx, db, true)
	return len(broken) != 0, err
}

// RunFilesystemPaths ports fs-path-heal from server/lib/schemaUpgradeSteps.js.
// It copies, never moves, historical files and does not modify GridFS versions
// unless they explicitly declare filesystem storage. Missing volumes stay
// unresolved so the startup runner can retry without recording a clean version.
func RunFilesystemPaths(ctx context.Context, db *mongo.Database, opts FilesystemOptions) (Result, error) {
	var result Result
	broken, err := findBrokenFilesystemVersions(ctx, db, false)
	if err != nil {
		return result, err
	}
	writable := opts.WritablePath
	if writable == "" {
		writable, err = os.Getwd()
		if err != nil {
			return result, err
		}
	}
	root := writable
	if !strings.HasSuffix(writable, "/files") && !strings.HasSuffix(writable, `\files`) {
		root = filepath.Join(writable, "files")
	}
	for _, item := range broken {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		healed, err := healFilesystemVersion(ctx, db, opts, writable, root, item)
		if err != nil {
			return result, err
		}
		if healed {
			result.Fixed++
		} else {
			result.Unresolved++
		}
	}
	return result, nil
}

func fsMap(value any) bson.M {
	switch v := value.(type) {
	case bson.M:
		return v
	case bson.D:
		m := bson.M{}
		for _, e := range v {
			m[e.Key] = e.Value
		}
		return m
	case map[string]any:
		return bson.M(v)
	}
	return nil
}

func fsTruthy(value any) bool {
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

func fsString(value any) string {
	if value == nil {
		return "null"
	}
	if id, ok := value.(bson.ObjectID); ok {
		return id.Hex()
	}
	return fmt.Sprint(value)
}

func filesystemVersionNeedsHeal(version bson.M) bool {
	if version == nil {
		return false
	}
	grid := fsTruthy(fsMap(version["meta"])["gridFsFileId"])
	if version["storage"] != "fs" && (fsTruthy(version["storage"]) || grid) {
		return false
	}
	if fsTruthy(version["path"]) {
		if _, err := os.Stat(fsString(version["path"])); err == nil {
			return false
		}
	}
	return true
}

func findBrokenFilesystemVersions(ctx context.Context, db *mongo.Database, first bool) ([]brokenFilesystemVersion, error) {
	var broken []brokenFilesystemVersion
	for _, collection := range []string{"attachments", "avatars"} {
		cursor, err := db.Collection(collection).Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"name": 1, "versions": 1}))
		if err != nil {
			return nil, err
		}
		for cursor.Next(ctx) {
			var doc bson.M
			if err := cursor.Decode(&doc); err != nil {
				_ = cursor.Close(ctx)
				return nil, err
			}
			// BSON D preserves version order, which determines candidate selection if
			// historical versions reference the same file. Request it separately from
			// Raw instead of ranging over Go's unordered map.
			var versions bson.D
			if raw := cursor.Current.Lookup("versions"); raw.Type == bson.TypeEmbeddedDocument {
				_ = bson.Unmarshal(raw.Value, &versions)
			}
			for _, entry := range versions {
				version := fsMap(entry.Value)
				if filesystemVersionNeedsHeal(version) {
					broken = append(broken, brokenFilesystemVersion{collection, doc, entry.Key, version})
					if first {
						return broken, cursor.Close(ctx)
					}
				}
			}
		}
		err = cursor.Err()
		closeErr := cursor.Close(ctx)
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return broken, nil
}

func healFilesystemVersion(ctx context.Context, db *mongo.Database, opts FilesystemOptions, writable, root string, item brokenFilesystemVersion) (bool, error) {
	dest := filepath.Join(root, item.collection)
	id := fsString(item.document["_id"])
	name := id
	if fsTruthy(item.document["name"]) {
		name = fsString(item.document["name"])
	}
	base := ""
	if fsTruthy(item.version["path"]) {
		base = filepath.Base(strings.ReplaceAll(fsString(item.version["path"]), `\`, "/"))
	}
	candidates := []string{}
	if base != "" {
		candidates = append(candidates, filepath.Join(dest, base))
	}
	candidates = append(candidates, filepath.Join(dest, id), filepath.Join(dest, id+"-"+item.name+"-"+name), filepath.Join(dest, id+"_"+name))
	roots := []string{root}
	if root != writable {
		roots = append(roots, writable)
	}
	for _, r := range roots {
		if base != "" {
			candidates = append(candidates, filepath.Join(r, "uploads", item.collection, base))
		}
		candidates = append(candidates, filepath.Join(r, "uploads", item.collection, id), filepath.Join(r, id+"-"+name))
		if base != "" {
			candidates = append(candidates, filepath.Join(r, item.collection, base))
		}
		candidates = append(candidates, filepath.Join(r, item.collection, id))
	}
	found := ""
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
			found = candidate
			break
		}
	}
	if found == "" {
		entries, _ := os.ReadDir(dest)
		for _, entry := range entries {
			n := entry.Name()
			if n == id || strings.HasPrefix(n, id+"-"+item.name+"-") || strings.HasPrefix(n, id+"-original-") || strings.HasPrefix(n, id+"_") {
				found = filepath.Join(dest, n)
				break
			}
		}
	}
	if found == "" {
		return false, nil
	}
	final := found
	fromDir, _ := filepath.Abs(filepath.Dir(found))
	toDir, _ := filepath.Abs(dest)
	if fromDir != toDir {
		filename := utf16.Encode([]rune(id + "-" + item.name + "-" + filepath.Base(found)))
		if len(filename) > 220 {
			filename = filename[:220]
		}
		final = filepath.Join(dest, string(utf16.Decode(filename)))
		err := os.MkdirAll(dest, 0777)
		if err == nil {
			err = copyFilesystemVersion(found, final)
		}
		if err != nil {
			if opts.Log != nil {
				opts.Log(fmt.Sprintf("heal %s/%s: copy failed: %v", item.collection, id, err))
			}
			return false, nil
		}
	}
	set := bson.M{"versions." + item.name + ".path": final, "versions." + item.name + ".storage": "fs"}
	if stat, err := os.Stat(final); err == nil {
		set["versions."+item.name+".size"] = stat.Size()
	}
	if item.name == "original" {
		set["path"] = final
	}
	_, err := db.Collection(item.collection).UpdateOne(ctx, bson.M{"_id": item.document["_id"]}, bson.M{"$set": set})
	return err == nil, err
}

func copyFilesystemVersion(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	stat, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, stat.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
