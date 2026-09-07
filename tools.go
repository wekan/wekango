package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wekan/wekango/internal/config"
	"github.com/wekan/wekango/internal/database"
	"github.com/wekan/wekango/internal/dbtools"
)

// Tool commands run in this process before starting Caddy or application writers.
// Symlinks retain the familiar tool names without duplicating executables.
func databaseToolCommand(argv []string) (string, []string, bool) {
	if len(argv) == 0 {
		return "", nil, false
	}
	name := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	if dbtools.Recognizes(name) {
		return name, argv[1:], true
	}
	if len(argv) > 1 && dbtools.Recognizes(argv[1]) {
		return argv[1], argv[2:], true
	}
	return "", nil, false
}

func toolUsesConfiguredDatabase(name string, args []string) bool {
	if name == "bsondump" {
		return false
	}
	for _, arg := range args {
		if arg == "--help" || arg == "--version" || arg == "-?" {
			return false
		}
		if strings.HasPrefix(arg, "mongodb://") || strings.HasPrefix(arg, "mongodb+srv://") {
			return false
		}
		for _, flag := range []string{"--uri", "--host", "--port", "--config"} {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return false
			}
		}
		// -h is the tools' host option, not their help option.
		if strings.HasPrefix(arg, "-h") && !strings.HasPrefix(arg, "--") {
			return false
		}
	}
	return true
}

func runDatabaseTool(name string, args []string) int {
	if !toolUsesConfiguredDatabase(name, args) {
		return dbtools.Run(name, args)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := config.Load(os.Getenv, filepath.Dir(executable))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	uri := cfg.MongoURL
	if uri == "" {
		server, err := database.Start(context.Background(), database.Config{Directory: cfg.SQLiteDirectory, SQLiteURL: cfg.SQLiteURL})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer server.Close()
		uri = server.URI()
	}
	toolArgs := append([]string{"--uri", uri}, args...)
	return dbtools.Run(name, toolArgs)
}
