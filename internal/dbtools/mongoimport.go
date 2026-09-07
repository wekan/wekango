// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongoimport/main/mongoimport.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongoimport"
)

func runmongoimport(args []string) (exitCode int) {
	opts, err := mongoimport.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %v", err)
		log.Logv(log.Always, util.ShortUsage("mongoimport"))
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

	m, err := mongoimport.New(opts)
	if err != nil {
		log.Logv(log.Always, err.Error())
		return util.ExitFailure
	}
	defer m.Close()

	numDocs, numFailure, err := m.ImportDocuments()
	if !opts.Quiet {
		if err != nil {
			log.Logvf(log.Always, "Failed: %v", err)
		}
		if m.ToolOptions.WriteConcern.Acknowledged() {
			if opts.Mode == "delete" {
				log.Logvf(
					log.Always,
					"%v document(s) deleted successfully. %v document(s) failed to delete.",
					numDocs,
					numFailure,
				)
			} else {
				log.Logvf(
					log.Always,
					"%v document(s) imported successfully. %v document(s) failed to import.",
					numDocs,
					numFailure,
				)
			}
		} else {
			log.Logvf(log.Always, "done")
		}
	}
	if err != nil {
		return util.ExitFailure
	}
	return util.ExitSuccess
}
