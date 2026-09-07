package eventlog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestReporterBatchesAndJoins(t *testing.T) {
	var events []bson.M
	r := NewReporter(time.Hour, func(_ context.Context, event bson.M) error { events = append(events, event); return nil })
	var workers sync.WaitGroup
	for i := 0; i < 10; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 10; j++ {
				r.Add(bson.M{"name": "GET /api/boards/:boardId", "userId": "user", "ip": "192.0.2.1", "at": time.Now()})
			}
		}()
	}
	workers.Wait()
	r.Close()
	r.Close()
	r.Add(bson.M{"name": "ignored", "at": time.Now()})
	if len(events) != 1 || events[0]["stream"] != "api" || fmt.Sprint(events[0]["count"]) != "100" {
		t.Fatalf("events: %#v", events)
	}
}

func TestReporterTimerAndThreshold(t *testing.T) {
	for _, count := range []int{1, 200} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			seen := make(chan struct{}, 201)
			interval := time.Hour
			if count == 1 {
				interval = time.Millisecond
			}
			r := NewReporter(interval, func(context.Context, bson.M) error { seen <- struct{}{}; return nil })
			defer r.Close()
			for i := 0; i < count; i++ {
				r.Add(bson.M{"name": fmt.Sprint(i), "at": time.Now()})
			}
			select {
			case <-seen:
			case <-time.After(3 * time.Second):
				t.Fatal("flush did not run")
			}
		})
	}
}

func TestReporterFailureCannotEscape(t *testing.T) {
	for _, crash := range []bool{false, true} {
		r := NewReporter(time.Hour, func(context.Context, bson.M) error {
			if crash {
				panic("unavailable")
			}
			return errors.New("unavailable")
		})
		r.Add(bson.M{"name": "GET /api/boards", "at": time.Now()})
		r.Close()
	}
}
