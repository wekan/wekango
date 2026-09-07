// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongoexport/main/mongoexport.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"os"

	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongoexport"
)

func runmongoexport(args []string) (exitCode int) {
	opts, err := mongoexport.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %v", err)
		log.Logv(log.Always, util.ShortUsage("mongoexport"))
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

	exporter, err := mongoexport.New(opts)
	if err != nil {
		log.Logvf(log.Always, "%v", err)

		if se, ok := err.(util.SetupError); ok && se.Message != "" {
			log.Logv(log.Always, se.Message)
		}

		return util.ExitFailure
	}
	defer exporter.Close()

	writer, err := exporter.GetOutputWriter()
	if err != nil {
		log.Logvf(log.Always, "error opening output stream: %v", err)
		return util.ExitFailure
	}
	if writer == nil {
		writer = os.Stdout
	} else {
		defer writer.Close()
	}

	numDocs, err := exporter.Export(writer)
	if err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}

	if numDocs == 1 {
		log.Logvf(log.Always, "exported %v record", numDocs)
	} else {
		log.Logvf(log.Always, "exported %v records", numDocs)
	}

	return util.ExitSuccess
}
