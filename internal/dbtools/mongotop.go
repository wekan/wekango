// Copyright (C) MongoDB, Inc. 2014-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Adapted from mongo-tools mongotop/main/mongotop.go at upstream
// 5df87866650a3ed661b5da6869f430a971ba25e5. CLI exits return to the
// single-executable dispatcher so deferred resource cleanup can finish.
package dbtools

import (
	"time"

	"github.com/mongodb/mongo-tools/common/db"
	"github.com/mongodb/mongo-tools/common/log"
	"github.com/mongodb/mongo-tools/common/signals"
	"github.com/mongodb/mongo-tools/common/util"
	"github.com/mongodb/mongo-tools/mongotop"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func runmongotop(args []string) (exitCode int) {
	// initialize command-line opts
	opts, err := mongotop.ParseOptions(args, VersionStr, GitCommit)
	if err != nil {
		log.Logvf(log.Always, "error parsing command line options: %s", err.Error())
		log.Logv(log.Always, util.ShortUsage("mongotop"))
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

	log.SetVerbosity(opts.Verbosity)
	finishedChan := signals.Handle()
	defer close(finishedChan)

	// verify uri options and log them
	opts.LogUnsupportedOptions()

	if opts.RowCount < 0 {
		log.Logvf(log.Always, "invalid value for --rowcount: %v", opts.RowCount)
		return util.ExitFailure
	}

	if opts.Username != "" && opts.Source == "" && !opts.RequiresExternalDB() {
		if opts.URI != nil && opts.ConnectionString != "" {
			log.Logvf(
				log.Always,
				"authSource is required when authenticating against a non $external database",
			)
			return util.ExitFailure
		}
		log.Logvf(
			log.Always,
			"--authenticationDatabase is required when authenticating against a non $external database",
		)
		return util.ExitFailure
	}

	if opts.ReplicaSetName == "" {
		opts.ReadPreference = readpref.PrimaryPreferred()
	}

	// create a session provider to connect to the db
	sessionProvider, err := db.NewSessionProvider(*opts.ToolOptions)
	if err != nil {
		log.Logvf(log.Always, "error connecting to host: %v", err)
		return util.ExitFailure
	}

	defer sessionProvider.Close()

	// fail fast if connecting to a mongos
	isMongos, err := sessionProvider.IsMongos()
	if err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}
	if isMongos {
		log.Logvf(log.Always, "cannot run mongotop against a mongos")
		return util.ExitFailure
	}

	// instantiate a mongotop instance
	top := &mongotop.MongoTop{
		Options:         opts.ToolOptions,
		OutputOptions:   opts.Output,
		SessionProvider: sessionProvider,
		Sleeptime:       time.Duration(opts.SleepTime) * time.Second,
	}

	// kick it off
	if err := top.Run(); err != nil {
		log.Logvf(log.Always, "Failed: %v", err)
		return util.ExitFailure
	}
	return util.ExitSuccess
}
