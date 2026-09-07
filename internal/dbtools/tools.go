// Package dbtools embeds the Apache-2.0 MongoDB Database Tools command entry
// points. Run is a standalone CLI operation: invoke it before starting HTTP or
// database services and return its status from main. Upstream logging, option
// parsing, standard streams, and signal handling are process-wide.
package dbtools

import (
	"fmt"
	"os"

	"github.com/mongodb/mongo-tools/common/util"
)

const (
	VersionStr = "main-5df87866650a"
	GitCommit  = "5df87866650a3ed661b5da6869f430a971ba25e5"
)

// Recognizes reports whether name selects an embedded Database Tools command.
func Recognizes(name string) bool {
	switch name {
	case "bsondump", "mongodump", "mongorestore", "mongoexport", "mongoimport", "mongofiles", "mongostat", "mongotop":
		return true
	}
	return false
}

// Run uses each upstream tool's arguments, output formats and exit statuses.
// It does not launch another executable or start a WeKan application server.
// This is not a concurrent in-process server API: upstream tools own the
// process's signal handlers and console for the duration of the command.
func Run(name string, args []string) int {
	switch name {
	case "bsondump":
		return runbsondump(args)
	case "mongodump":
		return runmongodump(args)
	case "mongorestore":
		return runmongorestore(args)
	case "mongoexport":
		return runmongoexport(args)
	case "mongoimport":
		return runmongoimport(args)
	case "mongofiles":
		return runmongofiles(args)
	case "mongostat":
		return runmongostat(args)
	case "mongotop":
		return runmongotop(args)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "unknown database tool: %s\n", name)
		return util.ExitFailure
	}
}
