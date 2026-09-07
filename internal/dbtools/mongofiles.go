// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongofiles/main/mongofiles.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"fmt"

	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongofiles"
)

func runmongofiles(args []string) (exitCode int) {
	opts, err := mongofiles.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %s", err.Error())
		log.Logv(log.Always, util.ShortUsage("mongofiles"))
		return util.ExitFailure
	}

	finishedChan := signals.Handle()
	defer close(finishedChan)

	// print help, if specified
	if opts.PrintHelp(false) {
		return util.ExitSuccess
	}

	// print version, if specified
	if opts.PrintVersion() {
		return util.ExitSuccess
	}

	mf, err := mongofiles.New(opts)
	if err != nil {
		log.Logv(log.Always, err.Error())
		if setupErr, ok := err.(util.SetupError); ok && setupErr.Message != "" {
			log.Logv(log.Always, setupErr.Message)
		}
		return util.ExitFailure
	}
	defer mf.Close()

	output, err := mf.Run(true)
	if err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}
	fmt.Printf("%s", output)
	return util.ExitSuccess
}
