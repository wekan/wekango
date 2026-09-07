package eventlog

import (
	"context"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Reporter batches API traffic independently of request completion. A failed
// flush is discarded like the source: this is reporting, never authorization.
type Reporter struct {
	pending  *UsageAccumulator
	fold     func(context.Context, bson.M) error
	names    func(context.Context, []string) (map[string]string, error)
	interval time.Duration
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
}

// NewDatabaseReporter resolves current account names once per flush, as the
// source middleware does, before handing rows to the shared event writer.
func NewDatabaseReporter(db *mongo.Database, interval time.Duration) *Reporter {
	w := NewWriter(db)
	r := newReporter(interval, w.Fold)
	r.names = func(ctx context.Context, ids []string) (map[string]string, error) {
		out := map[string]string{}
		if len(ids) == 0 {
			return out, nil
		}
		cursor, err := db.Collection("users").Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"username": 1}))
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var user struct {
				ID       string `bson:"_id"`
				Username string `bson:"username"`
			}
			if err := cursor.Decode(&user); err != nil {
				return nil, err
			}
			out[user.ID] = user.Username
		}
		return out, cursor.Err()
	}
	go r.run()
	return r
}

func NewReporter(interval time.Duration, fold func(context.Context, bson.M) error) *Reporter {
	r := newReporter(interval, fold)
	go r.run()
	return r
}

func newReporter(interval time.Duration, fold func(context.Context, bson.M) error) *Reporter {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	r := &Reporter{pending: NewUsageAccumulator(), fold: fold, interval: interval, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	return r
}

func (r *Reporter) Add(event bson.M) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.pending.Add(event)
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close joins the worker after a final bounded flush, before storage is closed.
func (r *Reporter) Close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
	r.mu.Unlock()
	<-r.done
}

func (r *Reporter) flush() {
	defer func() { _ = recover() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows := r.pending.Drain()
	names := map[string]string{}
	if r.names != nil && len(rows) > 0 {
		ids := []string{}
		seen := map[string]bool{}
		for _, row := range rows {
			if id, ok := row["userId"].(string); ok && id != "" && !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		var err error
		names, err = r.names(ctx, ids)
		if err != nil {
			return
		}
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		event := bson.M{"stream": "api", "api": row["name"], "apiUserId": row["userId"], "userId": row["userId"], "ip": row["ip"], "count": row["count"], "at": row["at"]}
		id, _ := row["userId"].(string)
		event["username"] = names[id]
		if row["location"] != nil {
			event["location"] = row["location"]
		}
		if err := r.fold(ctx, event); err != nil {
			return
		}
	}
}

func (r *Reporter) run() {
	defer close(r.done)
	var timer *time.Timer
	var tick <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-r.stop:
			r.flush()
			return
		case <-r.wake:
			if r.pending.Size() >= 200 {
				r.flush()
			} else if timer == nil {
				timer = time.NewTimer(r.interval)
				tick = timer.C
			}
		case <-tick:
			timer = nil
			tick = nil
			r.flush()
		}
	}
}
