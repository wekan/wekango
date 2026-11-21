// Seed a NEW disposable SQLite directory for the browser smoke test.
// Never points at an existing deployment or modifies existing files.
package main

import (
	"context"
	"fmt"
	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"os"
	"time"
)

const browserFixturePrehash = "aa6b910ee7802996f5fdbad379244fde9eeacd7d8b3320e668f9fc37e324f99a"

func main() {
	if len(os.Args) != 2 {
		panic("usage: browserfixture NEW_SQLITE_DIRECTORY")
	}
	if _, e := os.Stat(os.Args[1]); !os.IsNotExist(e) {
		panic("refusing existing directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server, e := database.Start(ctx, database.Config{Directory: os.Args[1]})
	must(e)
	defer server.Close()
	client, e := mongo.Connect(options.Client().ApplyURI(server.URI()))
	must(e)
	defer client.Disconnect(context.Background())
	db := client.Database("wekan")
	// Meteor's prehash of the fixture's "browser-fixture-password", as a
	// constant: no fast hash next to a password outside meteor_prehash.go
	// (TestNoFastHashOfPasswords, code scanning #3).
	hash, e := bcrypt.GenerateFromPassword([]byte(browserFixturePrehash), 10)
	must(e)
	_, e = db.Collection("users").InsertOne(ctx, bson.M{"_id": "browser-user", "username": "browser-user", "authenticationMethod": "password", "profile": bson.M{"fullname": "Browser Test"}, "services": bson.M{"password": bson.M{"bcrypt": string(hash)}, "securityBlock": bson.M{"reason": "Historical refusal", "source": "api"}}})
	must(e)
	_, e = db.Collection("users").InsertOne(ctx, bson.M{"_id": "browser-admin-id", "username": "browser-admin", "loginDisabled": "", "isAdmin": true, "authenticationMethod": "password", "sessionData": bson.M{"private": "server-only-state"}, "services": bson.M{"password": bson.M{"bcrypt": string(hash)}, "resume": bson.M{"loginTokens": bson.A{}}}})
	must(e)
	_, e = db.Collection("boards").InsertOne(ctx, bson.M{"_id": "browser-board", "type": "board", "archived": false, "title": "Existing SQLite board", "description": "Stored with FerretDB before the application starts.", "members": bson.A{bson.M{"userId": "browser-user", "isActive": true, "isAdmin": true}}})
	must(e)
	_, e = db.Collection("boards").InsertOne(ctx, bson.M{"_id": "revoked-board", "type": "board", "archived": false, "title": "Revoked private board", "members": bson.A{bson.M{"userId": "browser-user", "isActive": false}}})
	must(e)
	_, e = db.Collection("swimlanes").InsertOne(ctx, bson.M{"_id": "browser-lane", "boardId": "browser-board", "title": "Default", "sort": 0, "archived": false})
	must(e)
	_, e = db.Collection("lists").InsertOne(ctx, bson.M{"_id": "browser-list", "boardId": "browser-board", "title": "Existing list", "sort": 0, "archived": false})
	must(e)
	_, e = db.Collection("cards").InsertOne(ctx, bson.M{"_id": "browser-card", "boardId": "browser-board", "listId": "browser-list", "swimlaneId": "browser-lane", "title": "Existing SQLite card", "sort": 0, "archived": false})
	must(e)
	fmt.Println("Disposable browser fixture created")
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
