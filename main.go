// WeKan Go is an incremental compatibility implementation, not yet a complete
// replacement for Meteor WeKan. See ROADMAP.md for the tested boundary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/wekan/wekango/internal/api"
	"github.com/wekan/wekango/internal/compatibility"
	"github.com/wekan/wekango/internal/config"
	"github.com/wekan/wekango/internal/database"
	"github.com/wekan/wekango/internal/eventlog"
	"github.com/wekan/wekango/internal/migrations"
	"github.com/wekan/wekango/internal/transport"
	"github.com/wekan/wekango/internal/version"
	"github.com/wekan/wekango/internal/web"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	if name, args, ok := databaseToolCommand(os.Args); ok {
		os.Exit(runDatabaseTool(name, args))
	}
	if err := run(); err != nil {
		slog.Error("WeKan could not run", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("WeKan Go", version.Version)
			return nil
		case "--compatibility":
			fmt.Print(string(compatibility.Manifest))
			return nil
		case "--help", "-h":
			fmt.Println("wekan [--version | --compatibility | --check-config | --migrate-checklist-minicard]\nAlso: wekan {bsondump|mongodump|mongorestore|mongoexport|mongoimport|mongofiles|mongostat|mongotop} [options]\nConfiguration uses WeKan environment variables; see ROADMAP.md for current coverage.")
			return nil
		case "--check-config", "--migrate-checklist-minicard":
		default:
			return fmt.Errorf("unknown option %q", os.Args[1])
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cfg, err := config.Load(os.Getenv, filepath.Dir(executable))
	if err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "--check-config" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"port": cfg.Port, "rootURL": cfg.RootURL, "database": cfg.Database, "externalDatabase": cfg.MongoURL != "", "writablePath": cfg.WritablePath, "attachmentsPath": cfg.AttachmentsPath, "avatarsPath": cfg.AvatarsPath, "sqliteDirectory": cfg.SQLiteDirectory, "sqliteURIOverride": cfg.SQLiteURL != "", "withAPI": cfg.WithAPI})
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Stop HTTP and disconnect clients before stopping embedded storage.
	databaseCtx, cancelDatabase := context.WithCancel(context.Background())
	defer cancelDatabase()
	uri := cfg.MongoURL
	if uri == "" {
		db, err := database.Start(databaseCtx, database.Config{Directory: cfg.SQLiteDirectory, SQLiteURL: cfg.SQLiteURL})
		if err != nil {
			return err
		}
		defer db.Close()
		uri = db.URI()
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return errors.New("invalid MongoDB connection configuration")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(c)
	}()
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	err = client.Ping(pingCtx, nil)
	pingCancel()
	if err != nil {
		return errors.New("database is unavailable (check MONGO_URL or SQLite configuration)")
	}
	db := client.Database(cfg.Database)
	if len(os.Args) > 1 && os.Args[1] == "--migrate-checklist-minicard" {
		n, err := migrations.RunChecklistMinicard(ctx, db)
		if err != nil {
			return err
		}
		fmt.Printf("checklist-minicard-unset: %d documents changed\n", n)
		return nil
	}
	schema := migrations.NewSchemaRunner(migrations.FilesystemOptions{WritablePath: cfg.WritablePath, Log: func(message string) {
		slog.Info("schema-upgrade", "message", message)
	}})
	apiOptions := api.Options{WithAPI: cfg.WithAPI, LoginExpiration: cfg.LoginExpiration, TrustedClientIPHeader: true}
	if n, err := envPositive("REST_LOGIN_MAX_FAILURES", 10); err != nil {
		return err
	} else {
		apiOptions.LoginMaxFailures = n
	}
	if n, err := envPositive("REST_LOGIN_FAILURE_WINDOW_SECONDS", 60); err != nil {
		return err
	} else {
		apiOptions.LoginFailureWindow = time.Duration(n) * time.Second
	}
	if n, err := envPositive("REST_LOGIN_LOCKOUT_SECONDS", 60); err != nil {
		return err
	} else {
		apiOptions.LoginLockout = time.Duration(n) * time.Second
	}
	usage := eventlog.NewDatabaseReporter(db, eventlog.UsageFlushInterval(os.Getenv("WEKAN_API_USAGE_FLUSH_MS")))
	defer usage.Close()
	apiOptions.Usage = usage
	security := eventlog.NewSecurityReporter(db)
	defer security.Close()
	apiOptions.Security = security
	apiHandler := api.New(db, apiOptions)
	mux := http.NewServeMux()
	mux.Handle("GET /schema-upgrade-status", schema.Handler())
	mux.Handle("/api/", apiHandler)
	mux.Handle("/api", apiHandler)
	mux.Handle("/users/", apiHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if client.Ping(c, nil) != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/", web.Handler())
	var handler http.Handler = mux
	if cfg.Prefix != "" {
		root := http.NewServeMux()
		root.Handle(cfg.Prefix+"/", http.StripPrefix(cfg.Prefix, mux))
		root.HandleFunc("GET "+cfg.Prefix, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, cfg.Prefix+"/", http.StatusTemporaryRedirect)
		})
		handler = root
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	if err := transport.Start(cfg, listener.Addr().String()); err != nil {
		return err
	}
	defer transport.Stop()
	if os.Getenv("WEKAN_SKIP_SCHEMA_UPGRADE") != "true" {
		upgradeCtx, cancelUpgrade := context.WithCancel(ctx)
		upgradeDone := make(chan struct{})
		go func() {
			defer close(upgradeDone)
			_, err := schema.Run(upgradeCtx, db, migrations.UpgradeOptions{
				AppVersion: version.Version, Force: os.Getenv("WEKAN_FORCE_SCHEMA_UPGRADE") == "true",
				Log: func(message string) { slog.Info("schema-upgrade", "message", message) },
			})
			if err != nil {
				slog.Error("schema upgrade will retry next start", "error", err)
			}
		}()
		// Stop and join the upgrade before disconnecting its client or SQLite storage.
		defer func() { cancelUpgrade(); <-upgradeDone }()
	} else {
		slog.Info("schema upgrade skipped", "WEKAN_SKIP_SCHEMA_UPGRADE", true)
	}
	slog.Info("WeKan Go compatibility preview started", "url", cfg.RootURL, "version", version.Version, "external_database", cfg.MongoURL != "")
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
func envPositive(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 8640000 {
		return 0, fmt.Errorf("%s must be a positive integer within supported limits", name)
	}
	return n, nil
}
