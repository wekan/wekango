package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/eventlog"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestAPIUsageUsesRoutePatternsAndCurrentAccounts(t *testing.T) {
	db := testDB(t)
	insert(t, db, "users", bson.M{"_id": "caller", "username": "alice", "services": bson.M{"resume": bson.M{"loginTokens": bson.A{tokenDoc("token", time.Now())}}}})
	reporter := eventlog.NewDatabaseReporter(db, time.Hour)
	handler := New(db, Options{WithAPI: true, Usage: reporter})
	for _, id := range []string{"one", "two"} {
		if w := request(handler, http.MethodGet, "/api/boards/"+id, "token", "", ""); w.Code == 200 {
			t.Fatal("nonexistent board allowed")
		}
	}
	request(handler, http.MethodGet, "/api/guessed-one?foo=bar", "", "", "")
	request(handler, http.MethodGet, "/api/guessed-two", "", "", "")
	request(handler, http.MethodPost, "/users/login", "", `{"username":"none","password":"bad"}`, "")
	reporter.Close()
	var rows []bson.M
	cursor, err := db.Collection("eventlog").Find(context.Background(), bson.M{"stream": "api"})
	if err != nil {
		t.Fatal(err)
	}
	if err = cursor.All(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows %#v", rows)
	}
	for _, row := range rows {
		// Existing source foldEvent ignores the accumulator's row.count and
		// increments once per flush; preserve and document this source bug.
		if fmt.Sprint(row["count"]) != "1" {
			t.Fatalf("source fold count changed: %#v", row)
		}
		switch row["api"] {
		case "GET /api/boards/:boardId":
			if row["apiUserId"] != "caller" || row["username"] != "alice" {
				t.Fatal(row)
			}
		case "GET (no route)":
			if _, ok := row["apiUserId"]; ok {
				t.Fatal(row)
			}
		default:
			t.Fatalf("unbounded or non-API name: %#v", row)
		}
	}
}

func TestDisabledAPIIsNotUsage(t *testing.T) {
	var events []bson.M
	reporter := eventlog.NewReporter(time.Hour, func(_ context.Context, event bson.M) error { events = append(events, event); return nil })
	handler := New(testDB(t), Options{Usage: reporter})
	if w := request(handler, "GET", "/api/boards", "", "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	reporter.Close()
	if len(events) != 0 {
		t.Fatal(events)
	}
}
