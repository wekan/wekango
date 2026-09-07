// Package migrations ports individual WeKan upgrade steps. A step here does not
// imply that the complete startup upgrade pipeline has run.
package migrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const checklistMarker = "checklist-minicard-unset"

// RunChecklistMinicard performs the check and run phases from WeKan's
// server/lib/schemaUpgradeSteps.js (snapshot in testdata). It preserves the exact
// one-time _wekan_migration marker, and never writes the schema-upgrade marker.
// Call only while application writers and other migration runners are stopped.
// This function is deliberately opt-in until the entire startup pipeline ports.
func RunChecklistMinicard(ctx context.Context, db *mongo.Database) (int64, error) {
	markers := db.Collection("_wekan_migration")
	projection := options.FindOne().SetProjection(bson.M{"_id": 1})
	err := markers.FindOne(ctx, bson.M{"_id": checklistMarker}, projection).Err()
	if err == nil {
		return 0, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return 0, fmt.Errorf("read checklist migration marker: %w", err)
	}
	checklists := db.Collection("checklists")
	selector := bson.M{"showChecklistAtMinicard": false}
	err = checklists.FindOne(ctx, selector, projection).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("check checklist defaults: %w", err)
	}
	result, err := checklists.UpdateMany(ctx, selector, bson.M{"$unset": bson.M{"showChecklistAtMinicard": ""}})
	if err != nil {
		return 0, fmt.Errorf("unset checklist defaults: %w", err)
	}
	_, err = markers.UpdateOne(ctx, bson.M{"_id": checklistMarker}, bson.M{"$set": bson.M{"at": time.Now()}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return result.ModifiedCount, fmt.Errorf("record checklist migration: %w", err)
	}
	return result.ModifiedCount, nil
}
