package database

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FerretDB/FerretDB/ferretdb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func connect(t *testing.T, uri string) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return client
}

// Seed with FerretDB's public API directly, then open those exact files through
// the application adapter. This guards against a parallel or translated schema.
func TestExistingFerretDBFilesAndRestart(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "existing database")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := ferretdb.New(&ferretdb.Config{
		Listener: ferretdb.ListenerConfig{TCP: "127.0.0.1:0"},
		Handler:  "sqlite", SQLiteURL: SQLiteURL(directory),
		Logger: quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rawDone := make(chan error, 1)
	go func() { rawDone <- raw.Run(ctx) }()
	client := connect(t, raw.MongoDBURI())
	opCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	_, err = client.Database("wekan").Collection("boards").InsertOne(opCtx, bson.M{
		"_id": "board-one", "title": "Existing board", "members": bson.A{bson.M{"userId": "member-one", "isAdmin": true}},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := client.Disconnect(opCtx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	if err := <-rawDone; err != nil {
		t.Fatal(err)
	}

	server, err := Start(context.Background(), Config{Directory: directory, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	uri, err := url.Parse(server.URI())
	if err != nil {
		t.Fatal(err)
	}
	if uri.Hostname() != "127.0.0.1" || uri.Port() == "0" || uri.Port() == "" {
		t.Fatalf("non-private endpoint: %s", server.URI())
	}
	client = connect(t, server.URI())
	collection := client.Database("wekan").Collection("boards")
	var board bson.M
	if err := collection.FindOne(opCtx, bson.M{"_id": "board-one"}).Decode(&board); err != nil {
		t.Fatal(err)
	}
	if board["title"] != "Existing board" {
		t.Fatalf("unexpected board: %#v", board)
	}
	if _, err := collection.UpdateOne(opCtx, bson.M{"_id": "board-one"}, bson.M{"$set": bson.M{"title": "Updated board"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.Disconnect(opCtx); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", uri.Host, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("listener remained open after shutdown")
	}

	server, err = Start(context.Background(), Config{Directory: directory, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client = connect(t, server.URI())
	if err := client.Database("wekan").Collection("boards").FindOne(opCtx, bson.M{"_id": "board-one"}).Decode(&board); err != nil {
		t.Fatal(err)
	}
	if board["title"] != "Updated board" {
		t.Fatalf("write did not persist: %#v", board)
	}
	if err := client.Disconnect(opCtx); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidDirectory(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Fatal("accepted empty directory")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), Config{Directory: file}); err == nil {
		t.Fatal("accepted file as directory")
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, Config{Directory: t.TempDir()}); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	server, err := Start(ctx, Config{Directory: t.TempDir(), Logger: quietLogger()})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case <-server.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("canceled database did not stop")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitSQLiteURL(t *testing.T) {
	directory := t.TempDir()
	sqliteURL := SQLiteURL(directory) + "?_pragma=busy_timeout(10000)"
	// An explicit URI wins over Directory, so the invalid fallback must not open.
	server, err := Start(context.Background(), Config{SQLiteURL: sqliteURL, Directory: "\x00", Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"https://example.com/", "file://remote-host/data/", "file:" + directory} {
		if server, err := Start(context.Background(), Config{SQLiteURL: invalid, Logger: quietLogger()}); err == nil {
			_ = server.Close()
			t.Fatalf("accepted invalid SQLite URI %q", invalid)
		}
	}
}

// The URI names the directory itself on Unix and on Windows: a Windows path's
// space or braces escaped in the opaque part was a directory that does not exist.
func TestSQLiteURLKeepsTheDirectory(t *testing.T) {
	for directory, want := range map[string]string{
		"/var/lib/wekan/files/db":                                    "/var/lib/wekan/files/db/",
		"/tmp/existing database/{}":                                  "/tmp/existing database/{}/",
		"C:/Users/RUNNER~1/AppData/Local/Temp/existing database/001": "C:/Users/RUNNER~1/AppData/Local/Temp/existing database/001/",
		"C:/Users/RUNNER~1/AppData/Local/Temp/Test{}3117740374/001":  "C:/Users/RUNNER~1/AppData/Local/Temp/Test{}3117740374/001/",
	} {
		got := SQLiteURL(directory)
		uri, err := url.Parse(got)
		if err != nil {
			t.Fatalf("%s: %s does not parse: %v", directory, got, err)
		}
		// As FerretDB's parseURI reads it: the path, else the opaque part as is.
		path := uri.Path
		if path == "" {
			path = uri.Opaque
		}
		if uri.Scheme != "file" || uri.Host != "" || path != want {
			t.Fatalf("%s: %s is the directory %q, not %q", directory, got, path, want)
		}
	}
	// Negative: the old escaped opaque form names another directory.
	old := (&url.URL{Scheme: "file", OmitHost: true, Path: "C:/existing database/"}).String()
	if uri, _ := url.Parse(old); uri.Opaque == "C:/existing database/" {
		t.Fatal("negative: the old form was expected to escape the space, so this test proves nothing")
	}
}
