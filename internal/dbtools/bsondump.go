// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools bsondump/main/bsondump.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"github.com/mongodb/mongo-tools/bsondump"
	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
)

func runbsondump(args []string) (exitCode int) {
	// initialize command-line opts
	opts, err := bsondump.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "%v", err)
		log.Logv(log.Always, util.ShortUsage("bsondump"))
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

	finishedChan := signals.Handle()
	defer close(finishedChan)

	dumper, err := bsondump.New(opts)
	if err != nil {
		log.Logv(log.Always, err.Error())
		return util.ExitFailure
	}
	defer func() {
		err := dumper.Close()
		if err != nil {
			log.Logvf(log.Always, "error cleaning up: %v", err)
			exitCode = util.ExitFailure
		}
	}()

	log.Logvf(log.DebugLow, "running bsondump with --objcheck: %v", opts.ObjCheck)

	var numFound int
	if opts.Type == bsondump.DebugOutputType {
		numFound, err = dumper.Debug()
	} else {
		numFound, err = dumper.JSON()
	}

	log.Logvf(log.Always, "%v objects found", numFound)
	if err != nil {
		log.Logv(log.Always, err.Error())
		return util.ExitFailure
	}
	return util.ExitSuccess
}
