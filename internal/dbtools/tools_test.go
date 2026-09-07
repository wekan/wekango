package dbtools

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestToolNamesAndHelp(t *testing.T) {
	for _, name := range []string{"bsondump", "mongodump", "mongorestore", "mongoexport", "mongoimport", "mongofiles", "mongostat", "mongotop"} {
		if !Recognizes(name) {
			t.Fatal(name)
		}
		if code := Run(name, []string{"--help"}); code != 0 {
			t.Fatalf("%s help: %d", name, code)
		}
		if code := Run(name, []string{"--version"}); code != 0 {
			t.Fatalf("%s version: %d", name, code)
		}
		if code := Run(name, []string{"--not-a-real-option"}); code == 0 {
			t.Fatalf("%s accepted unknown option", name)
		}
	}
	if Recognizes("shell") {
		t.Fatal("unexpected tool")
	}
}

func TestDatabaseBackupAndImportRoundTrips(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server, err := database.Start(ctx, database.Config{Directory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := mongo.Connect(options.Client().ApplyURI(server.URI()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("wekan")
	original := bson.D{{Key: "_id", Value: "fixture"}, {Key: "title", Value: "Stored WeKan board"}, {Key: "createdAt", Value: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}, {Key: "nested", Value: bson.D{{Key: "flag", Value: true}}}, {Key: "binary", Value: bson.Binary{Subtype: 0, Data: []byte{1, 2, 255}}}}
	if _, err := db.Collection("boards").InsertOne(ctx, original); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	archive := filepath.Join(root, "backup.archive")
	run := func(name string, args ...string) {
		t.Helper()
		if code := Run(name, append([]string{"--uri", server.URI()}, args...)); code != 0 {
			t.Fatalf("%s failed: %d", name, code)
		}
	}
	run("mongodump", "--db", "wekan", "--archive="+archive)
	run("mongorestore", "--archive="+archive, "--nsFrom=wekan.*", "--nsTo=restored.*")
	var got bson.D
	if err := client.Database("restored").Collection("boards").FindOne(ctx, bson.M{"_id": "fixture"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	a, _ := bson.MarshalExtJSON(original, true, false)
	b, _ := bson.MarshalExtJSON(got, true, false)
	if string(a) != string(b) {
		t.Fatalf("BSON backup changed data: %s != %s", a, b)
	}
	configFile := filepath.Join(root, "connection.yaml")
	if err := os.WriteFile(configFile, []byte("uri: "+server.URI()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configuredOutput := filepath.Join(root, "configured.json")
	if code := Run("mongoexport", []string{"--config=" + configFile, "--db=wekan", "--collection=boards", "--jsonFormat=canonical", "--out=" + configuredOutput}); code != 0 {
		t.Fatalf("maintained YAML config: %d", code)
	}
	configured, err := os.ReadFile(configuredOutput)
	if err != nil || strings.TrimSpace(string(configured)) != string(a) {
		t.Fatalf("YAML target/data: %s %v", configured, err)
	}
	if err := os.WriteFile(configFile, []byte("uri: [invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run("mongoexport", []string{"--config=" + configFile, "--db=wekan", "--collection=boards"}); code == 0 {
		t.Fatal("invalid YAML accepted")
	}
	jsonFile := filepath.Join(root, "boards.json")
	run("mongoexport", "--db=wekan", "--collection=boards", "--jsonFormat=canonical", "--out="+jsonFile)
	run("mongoimport", "--db=imported", "--collection=boards", "--file="+jsonFile)
	if err := client.Database("imported").Collection("boards").FindOne(ctx, bson.M{"_id": "fixture"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	b, _ = bson.MarshalExtJSON(got, true, false)
	if string(a) != string(b) {
		t.Fatalf("JSON roundtrip changed data: %s != %s", a, b)
	}
	gridSource := filepath.Join(root, "source.bin")
	gridOutput := filepath.Join(root, "retrieved.bin")
	bytes := []byte{0, 1, 2, 255, 0, 42}
	if err := os.WriteFile(gridSource, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	run("mongofiles", "--db=wekan", "--local="+gridSource, "put", "attachment.bin")
	run("mongofiles", "--db=wekan", "--local="+gridOutput, "get", "attachment.bin")
	actual, err := os.ReadFile(gridOutput)
	if err != nil || string(actual) != string(bytes) {
		t.Fatalf("GridFS roundtrip: %v %v", actual, err)
	}
	run("mongofiles", "--db=wekan", "delete", "attachment.bin")
	n, err := db.Collection("fs.files").CountDocuments(ctx, bson.M{})
	if err != nil || n != 0 {
		t.Fatalf("GridFS delete: %d %v", n, err)
	}
	raw, _ := bson.Marshal(original)
	bsonFile := filepath.Join(root, "board.bson")
	if err := os.WriteFile(bsonFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run("bsondump", []string{"--bsonFile=" + bsonFile, "--outFile=" + filepath.Join(root, "decoded.json")}); code != 0 {
		t.Fatalf("bsondump: %d", code)
	}
	if err := os.WriteFile(bsonFile, []byte{99, 1}, 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run("bsondump", []string{"--bsonFile=" + bsonFile}); code == 0 {
		t.Fatal("malformed BSON accepted")
	}
}
