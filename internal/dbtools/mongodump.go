// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongodump/main/mongodump.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"time"

	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/progress"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongodump"
)

const (
	progressBarLength   = 24
	progressBarWaitTime = time.Second * 3
)

func runmongodump(args []string) (exitCode int) {
	// initialize command-line opts
	opts, err := mongodump.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %s", err.Error())
		log.Logv(log.Always, util.ShortUsage("mongodump"))
		return util.ExitFailure
	}

	// print help, if specified
	if opts.PrintHelp(false) {
		return util.ExitSuccess
	}

	// print version, if specified
	if opts.PrintVersion() {
		return util.ExitSuccess
	}

	// init logger
	log.SetVerbosity(opts.Verbosity)

	// verify uri options and log them
	opts.LogUnsupportedOptions()

	// kick off the progress bar manager
	progressManager := progress.NewBarWriter(
		log.Writer(0),
		progressBarWaitTime,
		progressBarLength,
		false,
	)
	progressManager.Start()
	defer progressManager.Stop()

	dump := mongodump.MongoDump{
		ToolOptions:     opts.ToolOptions,
		OutputOptions:   opts.OutputOptions,
		InputOptions:    opts.InputOptions,
		ProgressManager: progressManager,
	}

	finishedChan := signals.HandleWithInterrupt(dump.HandleInterrupt)
	defer close(finishedChan)

	if err = dump.Init(); err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}

	if err = dump.Dump(); err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}
	return util.ExitSuccess
}
