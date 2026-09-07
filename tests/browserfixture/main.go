// Seed a NEW disposable SQLite directory for the browser smoke test.
// Never points at an existing deployment or modifies existing files.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"os"
	"time"
)

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
	digest := sha256.Sum256([]byte("browser-fixture-password"))
	hash, e := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(digest[:])), 10)
	must(e)
	_, e = db.Collection("users").InsertOne(ctx, bson.M{"_id": "browser-user", "username": "browser-user", "authenticationMethod": "password", "profile": bson.M{"fullname": "Browser Test"}, "services": bson.M{"password": bson.M{"bcrypt": string(hash)}, "resume": bson.M{"loginTokens": bson.A{}}}})
	must(e)
	_, e = db.Collection("boards").InsertOne(ctx, bson.M{"_id": "browser-board", "title": "Existing SQLite board", "description": "Stored with FerretDB before the application starts.", "members": bson.A{bson.M{"userId": "browser-user", "isActive": true, "isAdmin": true}}})
	must(e)
	fmt.Println("Disposable browser fixture created")
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
