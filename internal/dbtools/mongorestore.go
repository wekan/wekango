// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongorestore/main/mongorestore.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongorestore"
)

func runmongorestore(args []string) (exitCode int) {
	opts, err := mongorestore.ParseOptions(args, VersionStr, GitCommit)

	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %s", err.Error())
		log.Logv(log.Always, util.ShortUsage("mongorestore"))
		return util.ExitFailure
	}

	// print help or version info, if specified
	if opts.PrintHelp(false) {
		return util.ExitSuccess
	}

	if opts.PrintVersion() {
		return util.ExitSuccess
	}

	restore, err := mongorestore.New(opts)
	if err != nil {
		log.Logv(log.Always, err.Error())
		return util.ExitFailure
	}
	defer restore.Close()

	finishedChan := signals.HandleWithInterrupt(restore.HandleInterrupt)
	defer close(finishedChan)

	result := restore.Restore()
	if result.Err != nil {
		log.Logvf(log.Always, "Failed: %v", result.Err)
	}

	if restore.ToolOptions.WriteConcern.Acknowledged() {
		log.Logvf(
			log.Always,
			"%v document(s) restored successfully. %v document(s) failed to restore.",
			result.Successes,
			result.Failures,
		)
	} else {
		log.Logvf(log.Always, "done")
	}

	if result.Err != nil {
		return util.ExitFailure
	}
	return util.ExitSuccess
}
