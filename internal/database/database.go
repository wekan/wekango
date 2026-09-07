// Package database embeds the maintained FerretDB v1 SQLite backend behind its
// public library API. It does not create or translate a second SQLite schema.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"github.com/FerretDB/FerretDB/ferretdb"
)

// Config selects a persistent directory, containing FerretDB's database files.
// Only one running server may own a directory. Back up existing deployments
// before opening them: FerretDB itself may upgrade its index metadata.
type Config struct {
	Directory string
	// SQLiteURL preserves FERRETDB_SQLITE_URL, including explicit SQLite options.
	// When supplied it takes precedence over Directory.
	SQLiteURL string
	Logger    *slog.Logger
}

// Server owns an in-process FerretDB instance. Its MongoDB endpoint is loopback
// only, but is not authenticated: other processes belonging to local users can
// reach it. Run the application in an isolated service account/network namespace
// when the host is shared with untrusted users.
type Server struct {
	uri    string
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Start opens the SQLite directory and starts a private ephemeral TCP listener.
// Cancellation closes connections, the listener, and the SQLite backend. The
// caller should disconnect its MongoDB clients before Close for quick shutdown.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Directory == "" && cfg.SQLiteURL == "" {
		return nil, errors.New("database directory is required")
	}
	sqliteURI := cfg.SQLiteURL
	if sqliteURI == "" {
		directory, err := filepath.Abs(cfg.Directory)
		if err != nil {
			return nil, fmt.Errorf("resolve database directory: %w", err)
		}
		if err = os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		sqliteURI = (&url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(directory) + "/"}).String()
	}
	instance, err := ferretdb.New(&ferretdb.Config{
		Listener: ferretdb.ListenerConfig{TCP: "127.0.0.1:0"},
		Logger:   cfg.Logger, Handler: "sqlite", SQLiteURL: sqliteURI,
	})
	if err != nil {
		return nil, fmt.Errorf("start embedded FerretDB: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	server := &Server{cancel: cancel, done: make(chan struct{})}
	go func() {
		server.err = instance.Run(runCtx)
		close(server.done)
	}()
	server.uri = instance.MongoDBURI()
	return server, nil
}

// URI returns the MongoDB connection URI, with no database name selected.
func (s *Server) URI() string { return s.uri }

// Done closes after the backend and every listener have stopped.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close stops the database and waits for storage resources to be released.
// It is safe to call repeatedly or concurrently.
func (s *Server) Close() error {
	s.cancel()
	<-s.done
	return s.err
}
